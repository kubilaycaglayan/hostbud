package notify

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

// Notification kinds: the three events an account can choose (V2-M3).
const (
	KindDone      = "done"
	KindAttention = "attention"
	KindFinished  = "finished"
	// KindTest is Settings → Send test notification (not an event choice).
	KindTest = "test"
)

// PayloadVersion is the payload's v field.
const PayloadVersion = 1

// MaxPayload caps a payload's JSON (it is also the push message body).
const MaxPayload = 1 << 10

// maxProject caps the project name in a payload (runes).
const maxProject = 80

// Payload is the whole content of a notification, in-app and push alike.
// It carries only the allowlisted fields: never instruction text, flags,
// paths, session names, run details, pane output, tokens or emails. Title
// and Body are made here from Project, Position and Outcome only.
type Payload struct {
	V        int    `json:"v"`
	Kind     string `json:"kind"`
	Key      string `json:"key"`
	Project  string `json:"project"`
	Position int    `json:"position"`
	Outcome  string `json:"outcome"`
	URL      string `json:"url"`
	Title    string `json:"title"`
	Body     string `json:"body"`
}

// Event is what happened, as the dispatcher knows it.
type Event struct {
	Kind     string // KindDone, KindAttention or KindFinished
	RunID    string // the run that finished (for a queue: its last run)
	QueueID  string
	ItemID   string // the item to open (for a queue: its last run's item)
	Project  string // the project's display name
	Position int    // the item's position in its queue
	// Outcome for KindAttention: the run's final status (failed, exited,
	// stale, …), or a completion gate (OutcomeAwaitingApproval,
	// OutcomeVerifyFailed). Ignored for the other kinds.
	Outcome string
	// Attempt is the verify attempt of an OutcomeVerifyFailed notice.
	Attempt int
}

// V2-M4 completion-gate outcomes of KindAttention notices.
const (
	OutcomeAwaitingApproval = "awaiting_approval"
	OutcomeVerifyFailed     = "verify_failed"
)

// DedupeKey is the event's key: one notification per event and device.
func DedupeKey(e Event) string {
	switch e.Kind {
	case KindFinished:
		return "queue:" + e.QueueID + ":finished:" + e.RunID
	case KindTest:
		return "test:" + e.RunID
	case KindAttention:
		switch e.Outcome {
		case OutcomeAwaitingApproval:
			return "run:" + e.RunID + ":approval"
		case OutcomeVerifyFailed:
			return fmt.Sprintf("run:%s:verify:%d", e.RunID, e.Attempt)
		}
		return "run:" + e.RunID + ":" + e.Kind
	default:
		return "run:" + e.RunID + ":" + e.Kind
	}
}

// attentionWords turns a run's final status into a short phrase.
var attentionWords = map[string]string{
	"failed":    "failed",
	"exited":    "ended without reaching its goal",
	"stale":     "has gone quiet",
	"cancelled": "was cancelled",
	// V2-M4 gates.
	OutcomeVerifyFailed:     "failed its verify command",
	OutcomeAwaitingApproval: "is waiting for your approval",
}

// Build makes the payload for an event. It fails only for an unknown kind.
func Build(e Event) (Payload, error) {
	project := strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, e.Project))
	if utf8.RuneCountInString(project) > maxProject {
		project = string([]rune(project)[:maxProject-1]) + "…"
	}
	if project == "" {
		project = "hostbud"
	}
	p := Payload{
		V: PayloadVersion, Kind: e.Kind, Key: DedupeKey(e), Project: project, Position: e.Position,
		URL: "/queues/" + url.PathEscape(e.QueueID) + "?item=" + url.QueryEscape(e.ItemID),
	}
	if e.Kind == KindTest {
		p.URL = "/"
	}
	switch e.Kind {
	case KindDone:
		p.Outcome = "done"
		p.Title = fmt.Sprintf("%s: item %d done", project, e.Position)
		p.Body = fmt.Sprintf("Item %d reached its goal.", e.Position)
	case KindAttention:
		words, ok := attentionWords[e.Outcome]
		if !ok {
			e.Outcome, words = "needs_attention", "needs your attention"
		}
		p.Outcome = e.Outcome
		p.Title = fmt.Sprintf("%s: item %d needs attention", project, e.Position)
		p.Body = fmt.Sprintf("Item %d %s. The queue is paused.", e.Position, words)
		if e.Outcome == OutcomeAwaitingApproval {
			p.Body = fmt.Sprintf("Item %d %s. The queue waits.", e.Position, words)
		}
	case KindFinished:
		p.Outcome = "finished"
		p.Title = project + ": queue finished"
		p.Body = fmt.Sprintf("Every item has run (last: item %d).", e.Position)
	case KindTest:
		p.Outcome = "test"
		p.Title = "hostbud: test notification"
		p.Body = "Notifications reach this device."
	default:
		return Payload{}, fmt.Errorf("unknown notification kind %q", e.Kind)
	}
	return p, nil
}

// JSON encodes the payload, within MaxPayload (a long project name is cut
// further if needed).
func (p Payload) JSON() ([]byte, error) {
	b, err := json.Marshal(p)
	for err == nil && len(b) > MaxPayload && p.Project != "" {
		r := []rune(p.Project)
		cut := string(r[:len(r)/2]) + "…"
		p.Title = strings.Replace(p.Title, p.Project, cut, 1)
		p.Project = cut
		if len(r) <= 2 {
			p.Project = ""
		}
		b, err = json.Marshal(p)
	}
	if err == nil && len(b) > MaxPayload {
		return nil, fmt.Errorf("notification payload is %d bytes, over %d", len(b), MaxPayload)
	}
	return b, err
}
