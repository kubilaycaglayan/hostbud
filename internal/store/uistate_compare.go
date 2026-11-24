package store

import (
	"context"
	"encoding/json"
	"errors"
)

// CompareUIStateForUser atomically saves only if the document still equals base.
// A null base means that no document existed when the client loaded it.
func (s *Store) CompareUIStateForUser(ctx context.Context, user, key string, base, value json.RawMessage) (bool, error) {
	if !json.Valid(base) || !json.Valid(value) {
		return false, errors.New("invalid UI state JSON")
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO ui_state (key, value_json, updated_at)
 SELECT $1, $2, $3 WHERE $4::jsonb = 'null'::jsonb OR EXISTS (SELECT 1 FROM ui_state WHERE key = $1)
 ON CONFLICT (key) DO UPDATE SET value_json = excluded.value_json, updated_at = excluded.updated_at
 WHERE ui_state.value_json::jsonb = $4::jsonb`, userUIKey(user, key), string(value), formatTime(s.now()), string(base))
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}
