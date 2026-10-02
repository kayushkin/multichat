package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Auto-reactions: our own account reacts with an emoji to every message one
// person sends, in one room or in every room we share with them. The reaction
// goes out as our Matrix account, which the bridge relays as the operator's
// own account on the platform. internal/inbound/auto_reactor.go applies them.

const autoReactionSchema = `
CREATE TABLE IF NOT EXISTS auto_reactions (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	sender_user_id  TEXT    NOT NULL,
	room_id         TEXT    NOT NULL DEFAULT '',  -- '' is every room
	reaction_key    TEXT    NOT NULL,
	enabled         INTEGER NOT NULL DEFAULT 1,
	reaction_count  INTEGER NOT NULL DEFAULT 0,
	last_reacted_at INTEGER NOT NULL DEFAULT 0,
	last_error      TEXT    NOT NULL DEFAULT '',
	last_error_at   INTEGER NOT NULL DEFAULT 0,
	created_at      INTEGER NOT NULL,
	updated_at      INTEGER NOT NULL,
	UNIQUE (sender_user_id, room_id, reaction_key)
);
CREATE TABLE IF NOT EXISTS auto_reaction_claims (
	rule_id    INTEGER NOT NULL REFERENCES auto_reactions(id) ON DELETE CASCADE,
	event_id   TEXT    NOT NULL,
	claimed_at INTEGER NOT NULL,
	PRIMARY KEY (rule_id, event_id)  -- JetStream is at-least-once; one reaction per rule per message
);
`

func createAutoReactionSchema(database *sql.DB) error {
	if _, err := database.Exec(autoReactionSchema); err != nil {
		return fmt.Errorf("create auto-reaction schema: %w", err)
	}
	return nil
}

// AutoReaction is one rule. SenderDisplayName and RoomName are for display
// only: the API fills them from Matrix when it answers, and they are never
// stored or matched on. Times are Unix seconds; 0 means never.
type AutoReaction struct {
	ID                int64  `json:"id"`
	SenderUserID      string `json:"sender_user_id"`
	SenderDisplayName string `json:"sender_display_name"`
	RoomID            string `json:"room_id"`
	RoomName          string `json:"room_name"`
	ReactionKey       string `json:"reaction_key"`
	Enabled           bool   `json:"enabled"`
	ReactionCount     int64  `json:"reaction_count"`
	LastReactedAt     int64  `json:"last_reacted_at"`
	LastError         string `json:"last_error"`
	LastErrorAt       int64  `json:"last_error_at"`
	CreatedAt         int64  `json:"created_at"`
	UpdatedAt         int64  `json:"updated_at"`
}

// AutoReactionChange is a PATCH: a nil field is left alone.
type AutoReactionChange struct {
	ReactionKey *string `json:"reaction_key"`
	Enabled     *bool   `json:"enabled"`
}

// ErrAutoReactionExists is a second rule for the same person, room and emoji.
var ErrAutoReactionExists = errors.New("that person already gets that reaction in that room")

const autoReactionColumns = `id, sender_user_id, room_id, reaction_key, enabled, reaction_count, last_reacted_at, last_error, last_error_at, created_at, updated_at`

func scanAutoReaction(row interface{ Scan(...any) error }) (AutoReaction, error) {
	var a AutoReaction
	err := row.Scan(&a.ID, &a.SenderUserID, &a.RoomID, &a.ReactionKey, &a.Enabled, &a.ReactionCount,
		&a.LastReactedAt, &a.LastError, &a.LastErrorAt, &a.CreatedAt, &a.UpdatedAt)
	return a, err
}

func queryAutoReactions(database *sql.DB, where string, args ...any) ([]AutoReaction, error) {
	rows, err := database.Query(`SELECT `+autoReactionColumns+` FROM auto_reactions `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("list auto-reactions: %w", err)
	}
	defer rows.Close()
	out := []AutoReaction{}
	for rows.Next() {
		a, err := scanAutoReaction(rows)
		if err != nil {
			return nil, fmt.Errorf("scan auto-reaction: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListAutoReactions lists every rule, on or off.
func ListAutoReactions(database *sql.DB) ([]AutoReaction, error) {
	return queryAutoReactions(database, "")
}

// EnabledAutoReactionsFor lists the rules that fire on a message from this
// sender in this room: the room's own rules and the every-room ones.
func EnabledAutoReactionsFor(database *sql.DB, senderUserID, roomID string) ([]AutoReaction, error) {
	return queryAutoReactions(database, `WHERE enabled = 1 AND sender_user_id = ? AND (room_id = '' OR room_id = ?)`, senderUserID, roomID)
}

func GetAutoReaction(database *sql.DB, id int64) (AutoReaction, error) {
	a, err := scanAutoReaction(database.QueryRow(`SELECT `+autoReactionColumns+` FROM auto_reactions WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, fmt.Errorf("read auto-reaction %d: %w", id, err)
	}
	return a, nil
}

func CreateAutoReaction(database *sql.DB, senderUserID, roomID, reactionKey string, enabled bool) (AutoReaction, error) {
	at := time.Now().Unix()
	result, err := database.Exec(`INSERT INTO auto_reactions (sender_user_id, room_id, reaction_key, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`, senderUserID, roomID, reactionKey, enabled, at, at)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return AutoReaction{}, ErrAutoReactionExists
		}
		return AutoReaction{}, fmt.Errorf("create auto-reaction: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return AutoReaction{}, fmt.Errorf("create auto-reaction: %w", err)
	}
	return GetAutoReaction(database, id)
}

// UpdateAutoReaction applies a change. A new emoji clears the last error,
// which belonged to the old one.
func UpdateAutoReaction(database *sql.DB, id int64, change AutoReactionChange) (AutoReaction, error) {
	current, err := GetAutoReaction(database, id)
	if err != nil {
		return current, err
	}
	next := current
	if change.ReactionKey != nil && *change.ReactionKey != current.ReactionKey {
		next.ReactionKey, next.LastError, next.LastErrorAt = *change.ReactionKey, "", 0
	}
	if change.Enabled != nil {
		next.Enabled = *change.Enabled
	}
	_, err = database.Exec(`UPDATE auto_reactions SET reaction_key = ?, enabled = ?, last_error = ?, last_error_at = ?, updated_at = ? WHERE id = ?`,
		next.ReactionKey, next.Enabled, next.LastError, next.LastErrorAt, time.Now().Unix(), id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return current, ErrAutoReactionExists
		}
		return current, fmt.Errorf("update auto-reaction %d: %w", id, err)
	}
	return GetAutoReaction(database, id)
}

func DeleteAutoReaction(database *sql.DB, id int64) error {
	if _, err := database.Exec(`DELETE FROM auto_reaction_claims WHERE rule_id = ?`, id); err != nil {
		return fmt.Errorf("delete auto-reaction %d claims: %w", id, err)
	}
	result, err := database.Exec(`DELETE FROM auto_reactions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete auto-reaction %d: %w", id, err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ErrAlreadyReacted is a redelivered message this rule already acted on.
var ErrAlreadyReacted = errors.New("rule already reacted to this message")

// ClaimAutoReaction records that a rule is about to react to a message, so a
// redelivery of the same message does not react twice.
func ClaimAutoReaction(database *sql.DB, ruleID int64, eventID string) error {
	result, err := database.Exec(`INSERT OR IGNORE INTO auto_reaction_claims (rule_id, event_id, claimed_at) VALUES (?, ?, ?)`,
		ruleID, eventID, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("claim auto-reaction %d on %s: %w", ruleID, eventID, err)
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return ErrAlreadyReacted
	}
	return nil
}

// RecordAutoReactionOutcome counts a reaction made, or keeps the error that
// stopped it, so the page can say whether a rule works.
func RecordAutoReactionOutcome(database *sql.DB, id int64, reactionErr error) error {
	var err error
	if reactionErr == nil {
		_, err = database.Exec(`UPDATE auto_reactions SET reaction_count = reaction_count + 1, last_reacted_at = ? WHERE id = ?`, time.Now().Unix(), id)
	} else {
		_, err = database.Exec(`UPDATE auto_reactions SET last_error = ?, last_error_at = ? WHERE id = ?`, reactionErr.Error(), time.Now().Unix(), id)
	}
	if err != nil {
		return fmt.Errorf("record the outcome of auto-reaction %d: %w", id, err)
	}
	return nil
}
