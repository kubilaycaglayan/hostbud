package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// v2 queue schema (migrations/0005_queues.sql and 0006_parallel_queues.sql,
// docs/roadmap-v2/ARCHITECTURE.md §6).

// Queue statuses.
const (
	QueueIdle     = "idle"
	QueueRunning  = "running"
	QueuePaused   = "paused"
	QueueFinished = "finished"
)

// Queue item statuses.
const (
	ItemQueued         = "queued"
	ItemRunning        = "running"
	ItemDone           = "done"
	ItemNeedsAttention = "needs_attention"
	ItemSkipped        = "skipped"
)

// Run statuses. Starting, running and stale runs are active: their session
// is (or may be) alive and they occupy the queue.
const (
	RunStarting  = "starting"
	RunRunning   = "running"
	RunAchieved  = "achieved"
	RunFailed    = "failed"
	RunExited    = "exited"
	RunStale     = "stale"
	RunCancelled = "cancelled"
)

// Run event sources.
const (
	SourceHook   = "hook"
	SourcePoller = "poller"
	SourceTimer  = "timer"
	SourceUser   = "user"
	SourceLLM    = "llm"
)

// MaxRunEventPayload caps a run event's JSON payload (the hook body cap).
const MaxRunEventPayload = 64 << 10

const (
	maxQueueNameBytes   = 255
	maxItemFlagsBytes   = 4096
	maxInstructionBytes = 16 << 10
)

var (
	queueStatuses = []string{QueueIdle, QueueRunning, QueuePaused, QueueFinished}
	itemStatuses  = []string{ItemQueued, ItemRunning, ItemDone, ItemNeedsAttention, ItemSkipped}
	runStatuses   = []string{RunStarting, RunRunning, RunAchieved, RunFailed, RunExited, RunStale, RunCancelled}
	eventSources  = []string{SourceHook, SourcePoller, SourceTimer, SourceUser, SourceLLM}
	// ActiveRunStatuses are the run states that still hold the queue.
	ActiveRunStatuses = []string{RunStarting, RunRunning, RunStale}
	agentKinds        = []string{"claude", "codex"}
)

var (
	// ErrConflict means a guarded update found the row in another state
	// (another signal or request got there first).
	ErrConflict = errors.New("state changed; reload and try again")
	// ErrPayloadTooLarge means a run event payload is over MaxRunEventPayload.
	ErrPayloadTooLarge = errors.New("run event payload is larger than 64 KiB")
	// ErrInvalidOrder means a reorder didn't list exactly the queued items.
	ErrInvalidOrder = errors.New("the new order must list exactly the queued items")
	// ErrProjectHasQueue means a project can't be deleted while a queue uses it.
	ErrProjectHasQueue = errors.New("the project has a queue")
)

// Queue is one project's ordered list of agent items.
type Queue struct {
	ID        string    `json:"id"`
	MachineID string    `json:"machineId"`
	ProjectID string    `json:"projectId"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	// WaitingSince is when the queue last started waiting for a run slot
	// (start, resume, or its previous run ending; V2-M2 FIFO order).
	WaitingSince *time.Time `json:"waitingSince,omitempty"`
}

// QueueItem is one agent run request: agent, flags and instruction.
type QueueItem struct {
	ID          string    `json:"id"`
	QueueID     string    `json:"queueId"`
	MachineID   string    `json:"machineId"`
	Position    int       `json:"position"`
	Agent       string    `json:"agent"`
	Flags       string    `json:"flags"`
	Instruction string    `json:"instruction"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Run is one attempt at an item: its session, binding and state. TokenHash
// is the SHA-256 of the bearer token; the token itself is never stored.
type Run struct {
	ID               string     `json:"id"`
	ItemID           string     `json:"itemId"`
	MachineID        string     `json:"machineId"`
	SessionName      string     `json:"sessionName"`
	AgentSessionID   string     `json:"agentSessionId,omitempty"`
	TranscriptPath   string     `json:"-"`
	TranscriptOffset int64      `json:"-"`
	ClientVersion    string     `json:"clientVersion,omitempty"`
	TokenHash        []byte     `json:"-"`
	Status           string     `json:"status"`
	StartedAt        time.Time  `json:"startedAt"`
	EndedAt          *time.Time `json:"endedAt,omitempty"`
	LastSignalAt     *time.Time `json:"lastSignalAt,omitempty"`
	Detail           string     `json:"detail,omitempty"`
}

// Active reports whether the run still holds its queue.
func (r Run) Active() bool { return slices.Contains(ActiveRunStatuses, r.Status) }

// RunEvent is one audit row: a forwarded hook body, a poller or timer
// observation, or an owner action.
type RunEvent struct {
	ID        int64           `json:"id"`
	RunID     string          `json:"runId"`
	MachineID string          `json:"machineId"`
	Source    string          `json:"source"`
	Kind      string          `json:"kind"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"createdAt"`
}

// QueueItemUpdate lists the item fields to change; nil fields stay.
type QueueItemUpdate struct {
	Agent       *string
	Flags       *string
	Instruction *string
}

// RunUpdate lists the run fields to set; nil fields stay.
type RunUpdate struct {
	SessionName      *string
	AgentSessionID   *string
	TranscriptPath   *string
	TranscriptOffset *int64
	ClientVersion    *string
	Detail           *string
	LastSignalAt     *time.Time
	EndedAt          *time.Time
}

const (
	queueCols = `id, machine_id, project_id, name, status, created_at, updated_at, waiting_since`
	itemCols  = `id, queue_id, machine_id, position, agent, flags, instruction, status, created_at, updated_at`
	runCols   = `id, item_id, machine_id, session_name, agent_session_id, transcript_path, transcript_offset,
		client_version, token_hash, status, started_at, ended_at, last_signal_at, detail`
	eventCols = `id, run_id, machine_id, source, kind, payload_json, created_at`
)

type scanner interface{ Scan(...any) error }

func scanQueue(row scanner) (Queue, error) {
	var q Queue
	var waiting sql.NullTime
	err := row.Scan(&q.ID, &q.MachineID, &q.ProjectID, &q.Name, &q.Status, &q.CreatedAt, &q.UpdatedAt, &waiting)
	if waiting.Valid {
		t := waiting.Time.UTC()
		q.WaitingSince = &t
	}
	return q, err
}

func scanItem(row scanner) (QueueItem, error) {
	var it QueueItem
	err := row.Scan(&it.ID, &it.QueueID, &it.MachineID, &it.Position, &it.Agent, &it.Flags, &it.Instruction, &it.Status, &it.CreatedAt, &it.UpdatedAt)
	return it, err
}

func scanRun(row scanner) (Run, error) {
	var r Run
	var agentSession, transcript, version, detail sql.NullString
	var offset sql.NullInt64
	var ended, signal sql.NullTime
	err := row.Scan(&r.ID, &r.ItemID, &r.MachineID, &r.SessionName, &agentSession, &transcript, &offset,
		&version, &r.TokenHash, &r.Status, &r.StartedAt, &ended, &signal, &detail)
	if err != nil {
		return r, err
	}
	r.AgentSessionID, r.TranscriptPath, r.ClientVersion, r.Detail = agentSession.String, transcript.String, version.String, detail.String
	r.TranscriptOffset = offset.Int64
	if ended.Valid {
		t := ended.Time.UTC()
		r.EndedAt = &t
	}
	if signal.Valid {
		t := signal.Time.UTC()
		r.LastSignalAt = &t
	}
	r.StartedAt = r.StartedAt.UTC()
	return r, nil
}

func scanEvent(row scanner) (RunEvent, error) {
	var e RunEvent
	var payload string
	err := row.Scan(&e.ID, &e.RunID, &e.MachineID, &e.Source, &e.Kind, &payload, &e.CreatedAt)
	e.Payload = json.RawMessage(payload)
	return e, err
}

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func newQueueRowID(prefix string) (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", fmt.Errorf("generate %s id: %w", prefix, err)
	}
	return prefix + "_" + hex.EncodeToString(id[:]), nil
}

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// NewULID returns a ULID: a 48-bit millisecond timestamp and 80 random bits,
// as 26 Crockford base32 characters (lexically sortable by time).
func NewULID(t time.Time) (string, error) {
	var b [16]byte
	ms := uint64(t.UnixMilli())
	for i := 5; i >= 0; i-- {
		b[i] = byte(ms)
		ms >>= 8
	}
	if _, err := rand.Read(b[6:]); err != nil {
		return "", fmt.Errorf("generate run id: %w", err)
	}
	// 128 bits → 26 characters of 5 bits, the first one holding 3 bits.
	out := make([]byte, 26)
	var acc uint32
	bits, bi := 2, 0 // two leading zero bits pad 128 to 130
	for i := range out {
		for bits < 5 {
			acc = acc<<8 | uint32(b[bi])
			bi++
			bits += 8
		}
		bits -= 5
		out[i] = crockford[(acc>>uint(bits))&31]
	}
	return string(out), nil
}

func validQueueName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > maxQueueNameBytes || strings.IndexByte(name, 0) >= 0 {
		return "", errors.New("queue name must be 1–255 bytes")
	}
	return name, nil
}

func validItem(agent, flags, instruction string) error {
	switch {
	case !slices.Contains(agentKinds, agent):
		return fmt.Errorf("unknown agent %q", agent)
	case len(flags) > maxItemFlagsBytes || strings.ContainsAny(flags, "\x00\n\r"):
		return errors.New("flags must be one line of at most 4096 bytes")
	case strings.TrimSpace(instruction) == "" || len(instruction) > maxInstructionBytes || strings.IndexByte(instruction, 0) >= 0:
		return errors.New("instruction must be 1–16384 bytes")
	}
	return nil
}

// CreateQueue creates an idle queue for a saved project.
func (s *Store) CreateQueue(ctx context.Context, projectID, name string) (Queue, error) {
	name, err := validQueueName(name)
	if err != nil {
		return Queue{}, err
	}
	p, err := s.Project(ctx, projectID)
	if err != nil {
		return Queue{}, err
	}
	id, err := newQueueRowID("queue")
	if err != nil {
		return Queue{}, err
	}
	now := s.now()
	q, err := scanQueue(s.db.QueryRowContext(ctx, `
		INSERT INTO queues (id, machine_id, project_id, name, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'idle', $5, $5) RETURNING `+queueCols,
		id, p.MachineID, p.ID, name, now))
	return q, duplicateName(err)
}

// duplicateName maps the per-project name index to ErrDuplicate.
func duplicateName(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "queues_project_name" {
		return ErrDuplicate
	}
	return err
}

// Queues lists a machine's queues in creation order.
func (s *Store) Queues(ctx context.Context, machineID string) ([]Queue, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+queueCols+` FROM queues WHERE machine_id = $1 ORDER BY created_at, id`, machineID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Queue
	for rows.Next() {
		q, err := scanQueue(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// Queue returns one queue or ErrNotFound.
func (s *Store) Queue(ctx context.Context, id string) (Queue, error) {
	q, err := scanQueue(s.db.QueryRowContext(ctx, `SELECT `+queueCols+` FROM queues WHERE id = $1`, id))
	return q, notFound(err)
}

// RenameQueue changes a queue's name.
func (s *Store) RenameQueue(ctx context.Context, id, name string) (Queue, error) {
	name, err := validQueueName(name)
	if err != nil {
		return Queue{}, err
	}
	q, err := scanQueue(s.db.QueryRowContext(ctx, `UPDATE queues SET name = $2, updated_at = $3 WHERE id = $1 RETURNING `+queueCols, id, name, s.now()))
	return q, duplicateName(notFound(err))
}

// TransitionQueue sets a queue's status if it is currently one of from.
// It returns ErrConflict if the queue is in another state.
func (s *Store) TransitionQueue(ctx context.Context, id string, from []string, to string) (Queue, error) {
	if !slices.Contains(queueStatuses, to) {
		return Queue{}, fmt.Errorf("invalid queue status %q", to)
	}
	q, err := scanQueue(s.db.QueryRowContext(ctx, `
		UPDATE queues SET status = $2, updated_at = $3 WHERE id = $1 AND status = ANY($4) RETURNING `+queueCols,
		id, to, s.now(), from))
	if errors.Is(err, sql.ErrNoRows) {
		if _, e := s.Queue(ctx, id); e != nil {
			return Queue{}, e
		}
		return Queue{}, ErrConflict
	}
	return q, err
}

// DeleteQueue deletes a queue with its items, runs and run events in one
// explicit transaction. It refuses (ErrConflict) while a run is active.
func (s *Store) DeleteQueue(ctx context.Context, id string) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if err := lockQueue(ctx, tx, id); err != nil {
			return err
		}
		var active int
		if err := tx.QueryRowContext(ctx, `
			SELECT count(*) FROM runs r JOIN queue_items i ON i.id = r.item_id
			WHERE i.queue_id = $1 AND r.status = ANY($2)`, id, ActiveRunStatuses).Scan(&active); err != nil {
			return err
		}
		if active > 0 {
			return ErrConflict
		}
		for _, stmt := range []string{
			`DELETE FROM run_events WHERE run_id IN (SELECT r.id FROM runs r JOIN queue_items i ON i.id = r.item_id WHERE i.queue_id = $1)`,
			`DELETE FROM runs WHERE item_id IN (SELECT id FROM queue_items WHERE queue_id = $1)`,
			`DELETE FROM queue_items WHERE queue_id = $1`,
			`DELETE FROM queues WHERE id = $1`,
		} {
			if _, err := tx.ExecContext(ctx, stmt, id); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// lockQueue takes the queue row lock that serializes position changes.
func lockQueue(ctx context.Context, tx *sql.Tx, id string) error {
	var got string
	err := tx.QueryRowContext(ctx, `SELECT id FROM queues WHERE id = $1 FOR UPDATE`, id).Scan(&got)
	return notFound(err)
}

// AddQueueItem appends a queued item at the end of the queue.
func (s *Store) AddQueueItem(ctx context.Context, queueID, agent, flags, instruction string) (QueueItem, error) {
	if err := validItem(agent, flags, instruction); err != nil {
		return QueueItem{}, err
	}
	id, err := newQueueRowID("item")
	if err != nil {
		return QueueItem{}, err
	}
	var it QueueItem
	err = s.inTx(ctx, func(tx *sql.Tx) error {
		if err := lockQueue(ctx, tx, queueID); err != nil {
			return err
		}
		now := s.now()
		it, err = scanItem(tx.QueryRowContext(ctx, `
			INSERT INTO queue_items (id, queue_id, machine_id, position, agent, flags, instruction, status, created_at, updated_at)
			SELECT $1, q.id, q.machine_id, COALESCE((SELECT max(position) FROM queue_items WHERE queue_id = q.id), 0) + 1,
				$3, $4, $5, 'queued', $6, $6
			FROM queues q WHERE q.id = $2
			RETURNING `+itemCols, id, queueID, agent, flags, instruction, now))
		return err
	})
	return it, err
}

// QueueItems lists a queue's items by position.
func (s *Store) QueueItems(ctx context.Context, queueID string) ([]QueueItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemCols+` FROM queue_items WHERE queue_id = $1 ORDER BY position`, queueID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []QueueItem
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// QueueItem returns one item or ErrNotFound.
func (s *Store) QueueItem(ctx context.Context, id string) (QueueItem, error) {
	it, err := scanItem(s.db.QueryRowContext(ctx, `SELECT `+itemCols+` FROM queue_items WHERE id = $1`, id))
	return it, notFound(err)
}

// FirstQueuedItem returns the queued item with the lowest position, or
// ErrNotFound when nothing is queued.
func (s *Store) FirstQueuedItem(ctx context.Context, queueID string) (QueueItem, error) {
	it, err := scanItem(s.db.QueryRowContext(ctx, `SELECT `+itemCols+` FROM queue_items WHERE queue_id = $1 AND status = 'queued' ORDER BY position LIMIT 1`, queueID))
	return it, notFound(err)
}

// UpdateQueueItem edits a queued item. It returns ErrConflict unless the
// item is queued.
func (s *Store) UpdateQueueItem(ctx context.Context, id string, u QueueItemUpdate) (QueueItem, error) {
	cur, err := s.QueueItem(ctx, id)
	if err != nil {
		return QueueItem{}, err
	}
	agent, flags, instruction := cur.Agent, cur.Flags, cur.Instruction
	if u.Agent != nil {
		agent = *u.Agent
	}
	if u.Flags != nil {
		flags = *u.Flags
	}
	if u.Instruction != nil {
		instruction = *u.Instruction
	}
	if err := validItem(agent, flags, instruction); err != nil {
		return QueueItem{}, err
	}
	it, err := scanItem(s.db.QueryRowContext(ctx, `
		UPDATE queue_items SET agent = $2, flags = $3, instruction = $4, updated_at = $5
		WHERE id = $1 AND status = 'queued' RETURNING `+itemCols, id, agent, flags, instruction, s.now()))
	if errors.Is(err, sql.ErrNoRows) {
		return QueueItem{}, ErrConflict
	}
	return it, err
}

// TransitionQueueItem sets an item's status if it is currently one of from.
func (s *Store) TransitionQueueItem(ctx context.Context, id string, from []string, to string) (QueueItem, error) {
	if !slices.Contains(itemStatuses, to) {
		return QueueItem{}, fmt.Errorf("invalid item status %q", to)
	}
	it, err := scanItem(s.db.QueryRowContext(ctx, `
		UPDATE queue_items SET status = $2, updated_at = $3 WHERE id = $1 AND status = ANY($4) RETURNING `+itemCols,
		id, to, s.now(), from))
	if errors.Is(err, sql.ErrNoRows) {
		if _, e := s.QueueItem(ctx, id); e != nil {
			return QueueItem{}, e
		}
		return QueueItem{}, ErrConflict
	}
	return it, err
}

// DeleteQueueItem deletes a queued item (ErrConflict otherwise) and closes
// the gap in the positions. A retried item is queued again and may have old
// runs; their rows and events go with it (their sessions stay open).
func (s *Store) DeleteQueueItem(ctx context.Context, id string) error {
	it, err := s.QueueItem(ctx, id)
	if err != nil {
		return err
	}
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if err := lockQueue(ctx, tx, it.QueueID); err != nil {
			return err
		}
		var pos int
		err := tx.QueryRowContext(ctx, `SELECT position FROM queue_items WHERE id = $1 AND status = 'queued'`, id).Scan(&pos)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConflict
		} else if err != nil {
			return err
		}
		for _, stmt := range []string{
			`DELETE FROM run_events WHERE run_id IN (SELECT id FROM runs WHERE item_id = $1)`,
			`DELETE FROM runs WHERE item_id = $1`,
			`DELETE FROM queue_items WHERE id = $1`,
		} {
			if _, err := tx.ExecContext(ctx, stmt, id); err != nil {
				return err
			}
		}
		return renumber(ctx, tx, it.QueueID, s.now())
	})
}

// renumber makes a queue's positions 1..n in their current order, through
// negative temporaries so UNIQUE(queue_id, position) holds at every step.
func renumber(ctx context.Context, tx *sql.Tx, queueID string, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `UPDATE queue_items SET position = -position WHERE queue_id = $1`, queueID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE queue_items q SET position = n.rn, updated_at = $2
		FROM (SELECT id, row_number() OVER (ORDER BY position DESC) AS rn FROM queue_items WHERE queue_id = $1) n
		WHERE q.id = n.id`, queueID, now)
	return err
}

// ReorderQueueItems puts the queued items in the given order. itemIDs must
// list exactly the queue's queued items (ErrInvalidOrder); they take the
// positions queued items hold now, so other items keep theirs.
func (s *Store) ReorderQueueItems(ctx context.Context, queueID string, itemIDs []string) ([]QueueItem, error) {
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		if err := lockQueue(ctx, tx, queueID); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT id, position FROM queue_items WHERE queue_id = $1 AND status = 'queued' ORDER BY position`, queueID)
		if err != nil {
			return err
		}
		var ids []string
		var positions []int
		for rows.Next() {
			var id string
			var pos int
			if err := rows.Scan(&id, &pos); err != nil {
				_ = rows.Close()
				return err
			}
			ids, positions = append(ids, id), append(positions, pos)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		want := slices.Clone(itemIDs)
		slices.Sort(want)
		have := slices.Clone(ids)
		slices.Sort(have)
		if !slices.Equal(want, have) || len(slices.Compact(slices.Clone(want))) != len(want) {
			return ErrInvalidOrder
		}
		now := s.now()
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx, `UPDATE queue_items SET position = -position WHERE id = $1`, id); err != nil {
				return err
			}
		}
		for i, id := range itemIDs {
			if _, err := tx.ExecContext(ctx, `UPDATE queue_items SET position = $2, updated_at = $3 WHERE id = $1`, id, positions[i], now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.QueueItems(ctx, queueID)
}

// CreateRun records a new starting run for an item.
func (s *Store) CreateRun(ctx context.Context, itemID string, tokenHash []byte, startedAt time.Time) (Run, error) {
	if len(tokenHash) != 32 {
		return Run{}, errors.New("run token hash must be a SHA-256")
	}
	id, err := NewULID(startedAt)
	if err != nil {
		return Run{}, err
	}
	r, err := scanRun(s.db.QueryRowContext(ctx, `
		INSERT INTO runs (id, item_id, machine_id, token_hash, status, started_at)
		SELECT $1, i.id, i.machine_id, $3, 'starting', $4 FROM queue_items i WHERE i.id = $2
		RETURNING `+runCols, id, itemID, tokenHash, startedAt.UTC()))
	return r, notFound(err)
}

// Run returns one run or ErrNotFound.
func (s *Store) Run(ctx context.Context, id string) (Run, error) {
	r, err := scanRun(s.db.QueryRowContext(ctx, `SELECT `+runCols+` FROM runs WHERE id = $1`, id))
	return r, notFound(err)
}

// RunByTokenHash returns the run with this token hash or ErrNotFound.
func (s *Store) RunByTokenHash(ctx context.Context, hash []byte) (Run, error) {
	r, err := scanRun(s.db.QueryRowContext(ctx, `SELECT `+runCols+` FROM runs WHERE token_hash = $1`, hash))
	return r, notFound(err)
}

// ActiveRunForItem returns the item's active run or ErrNotFound.
func (s *Store) ActiveRunForItem(ctx context.Context, itemID string) (Run, error) {
	r, err := scanRun(s.db.QueryRowContext(ctx, `SELECT `+runCols+` FROM runs WHERE item_id = $1 AND status = ANY($2) ORDER BY id DESC LIMIT 1`, itemID, ActiveRunStatuses))
	return r, notFound(err)
}

// ActiveRuns lists every active run (restart recovery), oldest first.
func (s *Store) ActiveRuns(ctx context.Context) ([]Run, error) {
	return s.queryRuns(ctx, `SELECT `+runCols+` FROM runs WHERE status = ANY($1) ORDER BY id`, ActiveRunStatuses)
}

// LatestRuns returns each item's newest run, keyed by item id.
func (s *Store) LatestRuns(ctx context.Context, queueID string) (map[string]Run, error) {
	runs, err := s.queryRuns(ctx, `
		SELECT DISTINCT ON (r.item_id) `+prefixCols("r.", runCols)+`
		FROM runs r JOIN queue_items i ON i.id = r.item_id
		WHERE i.queue_id = $1 ORDER BY r.item_id, r.id DESC`, queueID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]Run, len(runs))
	for _, r := range runs {
		out[r.ItemID] = r
	}
	return out, nil
}

func prefixCols(prefix, cols string) string {
	parts := strings.Split(cols, ",")
	for i, p := range parts {
		parts[i] = prefix + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}

func (s *Store) queryRuns(ctx context.Context, query string, args ...any) ([]Run, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// TransitionRun sets a run's status if it is currently one of from; detail
// and endedAt are set with it when given. It returns ErrConflict if the run
// is in another state, so two signals can never both move it.
func (s *Store) TransitionRun(ctx context.Context, id string, from []string, to, detail string, endedAt *time.Time) (Run, error) {
	if !slices.Contains(runStatuses, to) {
		return Run{}, fmt.Errorf("invalid run status %q", to)
	}
	var ended any
	if endedAt != nil {
		ended = endedAt.UTC()
	}
	r, err := scanRun(s.db.QueryRowContext(ctx, `
		UPDATE runs SET status = $2,
			detail = CASE WHEN $3 = '' THEN detail ELSE $3 END,
			ended_at = COALESCE($4, ended_at)
		WHERE id = $1 AND status = ANY($5) RETURNING `+runCols, id, to, detail, ended, from))
	if errors.Is(err, sql.ErrNoRows) {
		if _, e := s.Run(ctx, id); e != nil {
			return Run{}, e
		}
		return Run{}, ErrConflict
	}
	return r, err
}

// UpdateRun sets the given run fields.
func (s *Store) UpdateRun(ctx context.Context, id string, u RunUpdate) (Run, error) {
	var sets []string
	args := []any{id}
	add := func(col string, v any) {
		args = append(args, v)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	if u.SessionName != nil {
		add("session_name", *u.SessionName)
	}
	if u.AgentSessionID != nil {
		add("agent_session_id", *u.AgentSessionID)
	}
	if u.TranscriptPath != nil {
		add("transcript_path", *u.TranscriptPath)
	}
	if u.TranscriptOffset != nil {
		add("transcript_offset", *u.TranscriptOffset)
	}
	if u.ClientVersion != nil {
		add("client_version", *u.ClientVersion)
	}
	if u.Detail != nil {
		add("detail", *u.Detail)
	}
	if u.LastSignalAt != nil {
		add("last_signal_at", u.LastSignalAt.UTC())
	}
	if u.EndedAt != nil {
		add("ended_at", u.EndedAt.UTC())
	}
	if len(sets) == 0 {
		return s.Run(ctx, id)
	}
	r, err := scanRun(s.db.QueryRowContext(ctx, `UPDATE runs SET `+strings.Join(sets, ", ")+` WHERE id = $1 RETURNING `+runCols, args...))
	return r, notFound(err)
}

// AppendRunEvent records one audit row. The payload must be JSON of at most
// MaxRunEventPayload bytes (ErrPayloadTooLarge); nil is stored as {}.
func (s *Store) AppendRunEvent(ctx context.Context, runID, source, kind string, payload []byte) (RunEvent, error) {
	if !slices.Contains(eventSources, source) {
		return RunEvent{}, fmt.Errorf("invalid run event source %q", source)
	}
	if kind == "" || len(kind) > 64 {
		return RunEvent{}, errors.New("run event kind must be 1–64 bytes")
	}
	if payload == nil {
		payload = []byte("{}")
	}
	if len(payload) > MaxRunEventPayload {
		return RunEvent{}, ErrPayloadTooLarge
	}
	if !json.Valid(payload) {
		return RunEvent{}, errors.New("run event payload must be JSON")
	}
	e, err := scanEvent(s.db.QueryRowContext(ctx, `
		INSERT INTO run_events (run_id, machine_id, source, kind, payload_json, created_at)
		SELECT r.id, r.machine_id, $2, $3, $4, $5 FROM runs r WHERE r.id = $1
		RETURNING `+eventCols, runID, source, kind, string(payload), s.now()))
	return e, notFound(err)
}

// RunEvents lists a run's newest events first, at most limit (1–500).
func (s *Store) RunEvents(ctx context.Context, runID string, limit int) ([]RunEvent, error) {
	limit = min(max(limit, 1), 500)
	rows, err := s.db.QueryContext(ctx, `SELECT `+eventCols+` FROM run_events WHERE run_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2`, runID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []RunEvent
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
