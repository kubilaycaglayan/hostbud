package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// v2 queue schema (migrations/0005_queues.sql–0011_queue_lifecycle_times.sql,
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
	// V2-M4 completion gates: the run achieved its goal and the item waits
	// for its verify command, then for the owner's approval.
	ItemVerifying        = "verifying"
	ItemAwaitingApproval = "awaiting_approval"
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
	SourceHook       = "hook"
	SourcePoller     = "poller"
	SourceTimer      = "timer"
	SourceUser       = "user"
	SourceLLM        = "llm"
	SourceVerify     = "verify"
	KindLLMStarted   = "llm_started"
	KindLLMResult    = "llm_result"
	KindLLMSkipped   = "llm_skipped"
	KindLLMDiscarded = "llm_discarded"
	// SourceVerify: the V2-M4 verify runner (attempts and their results).
)

// MaxRunEventPayload caps a run event's JSON payload (the hook body cap).
const MaxRunEventPayload = 64 << 10

const (
	maxQueueNameBytes   = 255
	maxItemFlagsBytes   = 4096
	maxInstructionBytes = 16 << 10
	// MaxVerifyCommandBytes caps an item's verify command (V2-M4).
	MaxVerifyCommandBytes = 4096
)

var (
	queueSessionNameRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	queueStatuses      = []string{QueueIdle, QueueRunning, QueuePaused, QueueFinished}
	itemStatuses       = []string{ItemQueued, ItemRunning, ItemVerifying, ItemAwaitingApproval, ItemDone, ItemNeedsAttention, ItemSkipped}
	runStatuses        = []string{RunStarting, RunRunning, RunAchieved, RunFailed, RunExited, RunStale, RunCancelled}
	eventSources       = []string{SourceHook, SourcePoller, SourceTimer, SourceUser, SourceLLM, SourceVerify}
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
	StartedAt    *time.Time `json:"startedAt,omitempty"`
	EndedAt      *time.Time `json:"endedAt,omitempty"`
	ScheduledAt  *time.Time `json:"scheduledAt,omitempty"`
	// AfterRunID gates this queue until an already active tracked run achieves its goal.
	AfterRunID *string `json:"afterRunId,omitempty"`
}

// QueueItem is one agent run request: agent, flags and instruction.
type QueueItem struct {
	ID          string     `json:"id"`
	QueueID     string     `json:"queueId"`
	MachineID   string     `json:"machineId"`
	Position    int        `json:"position"`
	Agent       string     `json:"agent"`
	Flags       string     `json:"flags"`
	Instruction string     `json:"instruction"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	EndedAt     *time.Time `json:"endedAt,omitempty"`
	// V2-M4 gates: the verify command ("" = none; NULL in the store) and
	// whether the owner must approve before the item is done.
	VerifyCommand    string `json:"verifyCommand"`
	RequiresApproval bool   `json:"requiresApproval"`
	ExecutionMode    string `json:"executionMode"`
	TargetSession    string `json:"targetSession,omitempty"`
	Command          string `json:"command,omitempty"`
}

// ItemExecution configures either a tracked agent run or a one-shot command
// dispatched into an existing tmux session.
type ItemExecution struct {
	Mode          string
	TargetSession string
	Command       string
}

// ItemGates are an item's V2-M4 completion gates; the zero value is none.
type ItemGates struct {
	VerifyCommand    string
	RequiresApproval bool
	ExecutionMode    string
	TargetSession    string
	Command          string
}

// Gated reports whether the item has any completion gate.
func (it QueueItem) Gated() bool { return it.VerifyCommand != "" || it.RequiresApproval }

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

// QueueItemHistory is a metadata-only snapshot of an item lifecycle event.
// It is intentionally independent of live queues and runs.
type QueueItemHistory struct {
	ID               int64     `json:"id"`
	MachineID        string    `json:"machineId"`
	QueueID          string    `json:"queueId"`
	QueueName        string    `json:"queueName"`
	ProjectName      string    `json:"projectName"`
	ItemID           string    `json:"itemId"`
	Position         int       `json:"position"`
	ExecutionMode    string    `json:"executionMode"`
	TargetSession    string    `json:"targetSession,omitempty"`
	Agent            string    `json:"agent"`
	Flags            string    `json:"flags"`
	Instruction      string    `json:"instruction"`
	Command          string    `json:"command"`
	VerifyCommand    string    `json:"verifyCommand,omitempty"`
	RequiresApproval bool      `json:"requiresApproval,omitempty"`
	Status           string    `json:"status"`
	Action           string    `json:"action"`
	Detail           string    `json:"detail,omitempty"`
	OccurredAt       time.Time `json:"occurredAt"`
}

// QueueItemUpdate lists the item fields to change; nil fields stay.
type QueueItemUpdate struct {
	Agent       *string
	Flags       *string
	Instruction *string
	// V2-M4 gates: the only fields a needs-attention item may change.
	VerifyCommand    *string
	RequiresApproval *bool
	ExecutionMode    *string
	TargetSession    *string
	Command          *string
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
	queueCols = `id, machine_id, project_id, name, status, created_at, updated_at, waiting_since, after_run_id, started_at, ended_at, scheduled_at`
	itemCols  = `id, queue_id, machine_id, position, agent, flags, instruction, status, created_at, updated_at,
		verify_command, requires_approval, started_at, ended_at, execution_mode, target_session, command`
	runCols = `id, item_id, machine_id, session_name, agent_session_id, transcript_path, transcript_offset,
		client_version, token_hash, status, started_at, ended_at, last_signal_at, detail`
	eventCols = `id, run_id, machine_id, source, kind, payload_json, created_at`
)

type scanner interface{ Scan(...any) error }

func scanQueue(row scanner) (Queue, error) {
	var q Queue
	var waiting, started, ended, scheduled sql.NullTime
	var afterRun sql.NullString
	err := row.Scan(&q.ID, &q.MachineID, &q.ProjectID, &q.Name, &q.Status, &q.CreatedAt, &q.UpdatedAt, &waiting, &afterRun, &started, &ended, &scheduled)
	if waiting.Valid {
		t := waiting.Time.UTC()
		q.WaitingSince = &t
	}
	if afterRun.Valid {
		q.AfterRunID = &afterRun.String
	}
	if started.Valid {
		t := started.Time.UTC()
		q.StartedAt = &t
	}
	if ended.Valid {
		t := ended.Time.UTC()
		q.EndedAt = &t
	}
	if scheduled.Valid {
		t := scheduled.Time.UTC()
		q.ScheduledAt = &t
	}
	return q, err
}

func scanItem(row scanner) (QueueItem, error) {
	var it QueueItem
	var verify sql.NullString
	var started, ended sql.NullTime
	err := row.Scan(&it.ID, &it.QueueID, &it.MachineID, &it.Position, &it.Agent, &it.Flags, &it.Instruction, &it.Status, &it.CreatedAt, &it.UpdatedAt,
		&verify, &it.RequiresApproval, &started, &ended, &it.ExecutionMode, &it.TargetSession, &it.Command)
	it.VerifyCommand = verify.String
	if started.Valid {
		t := started.Time.UTC()
		it.StartedAt = &t
	}
	if ended.Valid {
		t := ended.Time.UTC()
		it.EndedAt = &t
	}
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

func scanRunAndQueue(row scanner) (Run, string, error) {
	var r Run
	var q string
	var agentSession, transcript, version, detail sql.NullString
	var offset sql.NullInt64
	var ended, signal sql.NullTime
	err := row.Scan(&r.ID, &r.ItemID, &r.MachineID, &r.SessionName, &agentSession, &transcript, &offset,
		&version, &r.TokenHash, &r.Status, &r.StartedAt, &ended, &signal, &detail, &q)
	if err != nil {
		return r, q, err
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
	return r, q, nil
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

// NormalizeVerifyCommand trims a verify command and checks the store's
// limits: at most MaxVerifyCommandBytes, one line, no NUL. "" means no
// verify gate. (That it splits into argv is the queue service's check.)
func NormalizeVerifyCommand(cmd string) (string, error) {
	cmd = strings.TrimSpace(cmd)
	switch {
	case len(cmd) > MaxVerifyCommandBytes:
		return "", errors.New("the verify command must be at most 4096 bytes")
	case strings.ContainsAny(cmd, "\x00\n\r"):
		return "", errors.New("the verify command must be one line")
	}
	return cmd, nil
}

// nullIfEmpty stores "" as NULL.
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
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

// ValidateItemExecution enforces the selected execution contract.
func ValidateItemExecution(x ItemExecution) error {
	switch x.Mode {
	case "agent":
		if x.TargetSession != "" || x.Command != "" {
			return errors.New("agent items cannot set a target session or command")
		}
	case "session":
		if !queueSessionNameRE.MatchString(x.TargetSession) {
			return errors.New("choose a valid existing session")
		}
		if strings.TrimSpace(x.Command) == "" || len(x.Command) > maxInstructionBytes || strings.ContainsAny(x.Command, "\x00\n\r") {
			return errors.New("command must be one line of 1–16384 bytes")
		}
	default:
		return errors.New("execution mode must be agent or session")
	}
	return nil
}

// CreateQueue creates an idle queue for a saved project.
func (s *Store) CreateQueue(ctx context.Context, projectID, name string, afterRunIDs ...string) (Queue, error) {
	name, err := validQueueName(name)
	if err != nil {
		return Queue{}, err
	}
	p, err := s.Project(ctx, projectID)
	if err != nil {
		return Queue{}, err
	}
	var afterRunID any
	if len(afterRunIDs) > 0 && afterRunIDs[0] != "" {
		run, runErr := s.Run(ctx, afterRunIDs[0])
		if runErr != nil {
			return Queue{}, runErr
		}
		if run.MachineID != p.MachineID || (run.Status != RunStarting && run.Status != RunRunning && run.Status != RunStale) {
			return Queue{}, ErrConflict
		}
		afterRunID = run.ID
	}
	id, err := newQueueRowID("queue")
	if err != nil {
		return Queue{}, err
	}
	now := s.now()
	q, err := scanQueue(s.db.QueryRowContext(ctx, `
		INSERT INTO queues (id, machine_id, project_id, name, status, created_at, updated_at, after_run_id)
		VALUES ($1, $2, $3, $4, 'idle', $5, $5, $6) RETURNING `+queueCols,
		id, p.MachineID, p.ID, name, now, afterRunID))
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

// QueueItemHistory returns bounded newest-first metadata history for a machine.
func (s *Store) QueueItemHistory(ctx context.Context, machineID string, limit, offset int) ([]QueueItemHistory, error) {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, machine_id, queue_id, queue_name, project_name, item_id,
		position, execution_mode, target_session, agent, flags, instruction, command, verify_command, requires_approval, item_status, action, detail, occurred_at
		FROM queue_item_history WHERE machine_id = $1 ORDER BY occurred_at DESC, id DESC LIMIT $2 OFFSET $3`, machineID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]QueueItemHistory, 0)
	for rows.Next() {
		var h QueueItemHistory
		if err := rows.Scan(&h.ID, &h.MachineID, &h.QueueID, &h.QueueName, &h.ProjectName, &h.ItemID,
			&h.Position, &h.ExecutionMode, &h.TargetSession, &h.Agent, &h.Flags, &h.Instruction, &h.Command, &h.VerifyCommand, &h.RequiresApproval, &h.Status, &h.Action, &h.Detail, &h.OccurredAt); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// Queue returns one queue or ErrNotFound.
func (s *Store) Queue(ctx context.Context, id string) (Queue, error) {
	q, err := scanQueue(s.db.QueryRowContext(ctx, `SELECT `+queueCols+` FROM queues WHERE id = $1`, id))
	return q, notFound(err)
}

// SetQueueSchedule persists or clears the due time for a not-yet-running
// queue. The queue status is guarded so pause and timer races remain safe.
func (s *Store) SetQueueSchedule(ctx context.Context, id string, due *time.Time, allowed ...string) (Queue, error) {
	if len(allowed) == 0 {
		allowed = []string{QueueIdle, QueueRunning, QueuePaused, QueueFinished}
	}
	var value any
	if due != nil {
		value = due.UTC()
	}
	q, err := scanQueue(s.db.QueryRowContext(ctx, `UPDATE queues SET scheduled_at = $2, updated_at = $3 WHERE id = $1 AND status = ANY($4) RETURNING `+queueCols,
		id, value, s.now(), allowed))
	if errors.Is(err, sql.ErrNoRows) {
		if _, e := s.Queue(ctx, id); e != nil {
			return Queue{}, e
		}
		return Queue{}, ErrConflict
	}
	return q, err
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
// explicit transaction. It refuses (ErrConflict) while a run is active or
// an item is verifying.
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
		// V2-M4: a verify command running on the host holds the queue too.
		var verifying int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM queue_items WHERE queue_id = $1 AND status = $2`, id, ItemVerifying).Scan(&verifying); err != nil {
			return err
		}
		if active > 0 || verifying > 0 {
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

// AddQueueItem appends a queued item at the end of the queue, with its
// completion gates if given (V2-M4; none by default).
func (s *Store) AddQueueItem(ctx context.Context, queueID, agent, flags, instruction string, gates ...ItemGates) (QueueItem, error) {
	var g ItemGates
	if len(gates) > 0 {
		g = gates[0]
	}
	x := ItemExecution{Mode: g.ExecutionMode, TargetSession: g.TargetSession, Command: g.Command}
	if x.Mode == "" {
		x.Mode = "agent"
	}
	if x.Mode == "agent" {
		if err := validItem(agent, flags, instruction); err != nil {
			return QueueItem{}, err
		}
	} else if !slices.Contains(agentKinds, agent) {
		agent = "claude"
	}
	if err := ValidateItemExecution(x); err != nil {
		return QueueItem{}, err
	}
	if x.Mode == "session" && (g.VerifyCommand != "" || g.RequiresApproval) {
		return QueueItem{}, errors.New("verify and approval gates require a tracked agent session")
	}
	verify, err := NormalizeVerifyCommand(g.VerifyCommand)
	if err != nil {
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
			INSERT INTO queue_items (id, queue_id, machine_id, position, agent, flags, instruction, status, created_at, updated_at,
				verify_command, requires_approval, execution_mode, target_session, command)
			SELECT $1, q.id, q.machine_id, COALESCE((SELECT max(position) FROM queue_items WHERE queue_id = q.id), 0) + 1,
				$3, $4, $5, 'queued', $6, $6, $7, $8, $9, $10, $11
			FROM queues q WHERE q.id = $2
			RETURNING `+itemCols, id, queueID, agent, flags, instruction, now, nullIfEmpty(verify), g.RequiresApproval, x.Mode, x.TargetSession, x.Command))
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

// UpdateQueueItem edits a queued item. A needs-attention item may change
// only its gates (V2-M4: fix a verify command before Re-run verify). It
// returns ErrConflict when the item is in another state.
func (s *Store) UpdateQueueItem(ctx context.Context, id string, u QueueItemUpdate) (QueueItem, error) {
	cur, err := s.QueueItem(ctx, id)
	if err != nil {
		return QueueItem{}, err
	}
	next, err := ApplyItemUpdate(cur, u)
	if err != nil {
		return QueueItem{}, err
	}
	editable := EditableStatuses(u)
	it, err := scanItem(s.db.QueryRowContext(ctx, `
		UPDATE queue_items SET agent = $2, flags = $3, instruction = $4, verify_command = $5, requires_approval = $6,
			execution_mode = $7, target_session = $8, command = $9, updated_at = $10
		WHERE id = $1 AND status = ANY($11) RETURNING `+itemCols,
		id, next.Agent, next.Flags, next.Instruction, nullIfEmpty(next.VerifyCommand), next.RequiresApproval, next.ExecutionMode, next.TargetSession, next.Command, s.now(), editable))
	if errors.Is(err, sql.ErrNoRows) {
		return QueueItem{}, ErrConflict
	}
	return it, err
}

// ApplyItemUpdate returns cur with u applied and checked (the store's
// limits; the verify command normalized).
func ApplyItemUpdate(cur QueueItem, u QueueItemUpdate) (QueueItem, error) {
	next := cur
	if next.ExecutionMode == "" {
		next.ExecutionMode = "agent"
	}
	if u.Agent != nil {
		next.Agent = *u.Agent
	}
	if u.Flags != nil {
		next.Flags = *u.Flags
	}
	if u.Instruction != nil {
		next.Instruction = *u.Instruction
	}
	if u.RequiresApproval != nil {
		next.RequiresApproval = *u.RequiresApproval
	}
	if u.VerifyCommand != nil {
		verify, err := NormalizeVerifyCommand(*u.VerifyCommand)
		if err != nil {
			return cur, err
		}
		next.VerifyCommand = verify
	}
	if u.ExecutionMode != nil {
		next.ExecutionMode = *u.ExecutionMode
	}
	if u.TargetSession != nil {
		next.TargetSession = *u.TargetSession
	}
	if u.Command != nil {
		next.Command = *u.Command
	}
	if next.ExecutionMode == "agent" {
		if err := validItem(next.Agent, next.Flags, next.Instruction); err != nil {
			return cur, err
		}
	} else if !slices.Contains(agentKinds, next.Agent) {
		return cur, errors.New("unknown agent")
	}
	if err := ValidateItemExecution(ItemExecution{Mode: next.ExecutionMode, TargetSession: next.TargetSession, Command: next.Command}); err != nil {
		return cur, err
	}
	if next.ExecutionMode == "session" && (next.VerifyCommand != "" || next.RequiresApproval) {
		return cur, errors.New("verify and approval gates require a tracked agent session")
	}
	return next, nil
}

// EditableStatuses are the item states u may be applied in: any field
// while queued; the gates alone also while the item needs attention.
func EditableStatuses(u QueueItemUpdate) []string {
	if u.Agent == nil && u.Flags == nil && u.Instruction == nil && u.ExecutionMode == nil && u.TargetSession == nil && u.Command == nil {
		return []string{ItemQueued, ItemNeedsAttention}
	}
	return []string{ItemQueued}
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

// LLMEligibleRuns returns running and stale runs whose item is in the
// matching state and for which this run remains the item's latest attempt.
func (s *Store) LLMEligibleRuns(ctx context.Context) ([]Run, error) {
	return s.queryRuns(ctx, `SELECT `+prefixCols("r.", runCols)+` FROM runs r JOIN queue_items i ON i.id=r.item_id
		WHERE ((r.status='running' AND i.status='running') OR (r.status='stale' AND i.status='needs_attention'))
		AND r.id=(SELECT max(r2.id) FROM runs r2 WHERE r2.item_id=r.item_id) ORDER BY r.started_at`)
}

// RecoverLLMClaims closes abandoned claims after the provider's maximum
// classification duration; the recovered marker prevents another call.
func (s *Store) RecoverLLMClaims(ctx context.Context) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO run_events (run_id,machine_id,source,kind,payload_json,created_at)
		SELECT e.run_id,e.machine_id,'llm','llm_result','{"label":"unknown","reason":"hostbud restarted during classification","recovered":true}',$1
		FROM run_events e WHERE e.source='llm' AND e.kind='llm_started' AND e.created_at < $1::timestamptz-interval '70 seconds'
		AND NOT EXISTS(SELECT 1 FROM run_events z WHERE z.run_id=e.run_id AND z.source='llm' AND z.kind IN ('llm_result','llm_skipped','llm_discarded') AND z.created_at>=e.created_at)`, s.now())
		return err
	})
}

// SkipLLM closes a claim when capture proves its exact tmux target is gone.
// Skips are not charged against the provider-call budget.
func (s *Store) SkipLLM(ctx context.Context, runID string) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		var locked string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM runs WHERE id=$1 FOR UPDATE`, runID).Scan(&locked); err != nil {
			return notFound(err)
		}
		var machine string
		if err := tx.QueryRowContext(ctx, `SELECT machine_id FROM runs WHERE id=$1`, runID).Scan(&machine); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO run_events(run_id,machine_id,source,kind,payload_json,created_at)
		SELECT $1,$2,'llm','llm_skipped','{}',$3 WHERE EXISTS(SELECT 1 FROM run_events WHERE run_id=$1 AND source='llm' AND kind='llm_started')
		AND NOT EXISTS(SELECT 1 FROM run_events WHERE run_id=$1 AND source='llm' AND kind IN ('llm_result','llm_skipped'))`, runID, machine, s.now())
		return err
	})
}

// ClaimLLM commits the claim before capture/provider work and enforces the
// per-run hourly budget. An interrupted claim recovered after restart is terminal.
func (s *Store) ClaimLLM(ctx context.Context, runID string, signal *time.Time, limit int, quiet time.Duration) (bool, error) {
	claimed := false
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var locked string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM runs WHERE id=$1 FOR UPDATE`, runID).Scan(&locked); err != nil {
			return err
		}
		var exists bool
		err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM runs r JOIN queue_items i ON i.id=r.item_id
			WHERE r.id=$1 AND ((r.status='running' AND i.status='running') OR (r.status='stale' AND i.status='needs_attention'))
			AND r.id=(SELECT max(r2.id) FROM runs r2 WHERE r2.item_id=r.item_id) AND r.last_signal_at IS NOT DISTINCT FROM $2
			AND (SELECT count(*) FROM run_events e WHERE e.run_id=r.id AND e.source='llm' AND e.kind='llm_started' AND e.created_at > now()-interval '1 hour' AND NOT EXISTS(SELECT 1 FROM run_events z WHERE z.run_id=e.run_id AND z.source='llm' AND z.kind='llm_skipped' AND z.created_at>=e.created_at)) < $3
			AND NOT EXISTS(SELECT 1 FROM run_events e WHERE e.run_id=r.id AND e.source='llm' AND e.kind='llm_started' AND e.created_at > now()-($4 * interval '1 second'))
			AND NOT EXISTS(SELECT 1 FROM run_events e WHERE e.run_id=r.id AND e.source='llm' AND e.kind='llm_result' AND e.payload_json::jsonb->>'recovered'='true')
			AND NOT EXISTS(SELECT 1 FROM run_events e WHERE e.run_id=r.id AND e.source='llm' AND e.kind='llm_started' AND NOT EXISTS(SELECT 1 FROM run_events z WHERE z.run_id=e.run_id AND z.source='llm' AND z.kind IN ('llm_result','llm_skipped','llm_discarded') AND z.created_at>=e.created_at)))`, runID, signal, limit, quiet.Seconds()).Scan(&exists)
		if err != nil || !exists {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"signalAt": signal})
		if err := insertRunEvent(ctx, tx, runID, SourceLLM, KindLLMStarted, payload, s.now()); err != nil {
			return err
		}
		claimed = true
		return nil
	})
	return claimed, err
}

// FinishLLM stores a classification only while the run and signal are still
// current. It cannot mutate queue, item, run or slot state.
func (s *Store) FinishLLM(ctx context.Context, runID string, signal *time.Time, result []byte) (bool, Run, string, error) {
	return s.FinishLLMNotify(ctx, runID, signal, result, nil)
}

// LLMNoticeContext contains only the allowlisted notification fields.
func (s *Store) LLMNoticeContext(ctx context.Context, runID string) (string, string, int, error) {
	var queueID, project string
	var position int
	err := s.db.QueryRowContext(ctx, `SELECT q.id,p.name,i.position FROM runs r JOIN queue_items i ON i.id=r.item_id JOIN queues q ON q.id=i.queue_id JOIN projects p ON p.id=q.project_id WHERE r.id=$1`, runID).Scan(&queueID, &project, &position)
	return queueID, project, position, notFound(err)
}

// FinishLLMNotify inserts the flag and eligible push outbox rows in the same
// guarded transaction; the notice key is unique per account and run/label.
func (s *Store) FinishLLMNotify(ctx context.Context, runID string, signal *time.Time, result []byte, notice *Notice) (bool, Run, string, error) {
	if len(result) > MaxRunEventPayload || !json.Valid(result) {
		return false, Run{}, "", ErrPayloadTooLarge
	}
	var run Run
	var queueID string
	inserted := false
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		run, queueID, err = scanRunAndQueue(tx.QueryRowContext(ctx, `SELECT `+prefixCols("r.", runCols)+`,q.id FROM runs r JOIN queue_items i ON i.id=r.item_id JOIN queues q ON q.id=i.queue_id
		WHERE r.id=$1 AND ((r.status='running' AND i.status='running') OR (r.status='stale' AND i.status='needs_attention'))
		AND r.id=(SELECT max(r2.id) FROM runs r2 WHERE r2.item_id=r.item_id) AND r.last_signal_at IS NOT DISTINCT FROM $2
		FOR UPDATE OF r,i`, runID, signal))
		if errors.Is(err, sql.ErrNoRows) {
			var claimExists bool
			if e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM run_events WHERE run_id=$1 AND source='llm' AND kind=$2)`, runID, KindLLMStarted).Scan(&claimExists); e != nil {
				return e
			}
			if claimExists {
				discarded, _ := json.Marshal(map[string]string{"label": "unknown", "reason": "classification discarded because the run changed"})
				return insertRunEvent(ctx, tx, runID, SourceLLM, KindLLMDiscarded, discarded, s.now())
			}
			return nil
		}
		if err != nil {
			return err
		}
		if err := insertRunEvent(ctx, tx, runID, SourceLLM, KindLLMResult, result, s.now()); err != nil {
			return err
		}
		if err := enqueueNotice(ctx, tx, notice, s.now()); err != nil {
			return err
		}
		inserted = true
		return nil
	})
	return inserted, run, queueID, err
}
