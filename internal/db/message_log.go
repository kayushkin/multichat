package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/mattn/go-sqlite3"
)

// The message log keeps every message a bridge brings in, as it arrives, so
// that its text survives a later delete. On Matrix a delete is a redaction and
// Synapse strips the event's content, so once a message is deleted on the
// platform nothing else on this host still has what it said.
//
// Rows are keyed by the Matrix event id, which Synapse owns. A redaction does
// not remove the row: it sets redacted_at and names the redaction event.
const messageLogSchema = `
CREATE TABLE IF NOT EXISTS message_log (
	event_id             TEXT PRIMARY KEY,               -- Matrix event id
	room_id              TEXT NOT NULL,
	room_name            TEXT NOT NULL,
	platform             TEXT NOT NULL,                  -- one of matrix.KnownPlatforms
	network_id           TEXT NOT NULL,                  -- from the room's m.bridge state; on Discord the server id
	network_name         TEXT NOT NULL,
	channel_id           TEXT NOT NULL,                  -- from m.bridge; on Discord the channel id
	channel_name         TEXT NOT NULL,
	channel_external_url TEXT NOT NULL,
	sender_user_id       TEXT NOT NULL,                  -- the bridge puppet
	sender_display_name  TEXT NOT NULL,
	message_type         TEXT NOT NULL,                  -- msgtype: m.text, m.image, ...
	body                 TEXT NOT NULL,
	content_json         TEXT NOT NULL,                  -- the event content exactly as Synapse sent it
	replaces_event_id    TEXT NOT NULL DEFAULT '',       -- set on an edit: the message it edits
	sent_at              DATETIME NOT NULL,              -- origin_server_ts
	logged_at            DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	redacted_at          DATETIME,                       -- when the redaction was sent; NULL while not deleted
	redaction_event_id   TEXT NOT NULL DEFAULT '',
	redacted_by_user_id  TEXT NOT NULL DEFAULT ''        -- who sent the redaction; on Discord the bridge, not the person who deleted
);
CREATE INDEX IF NOT EXISTS idx_message_log_room_sent ON message_log(room_id, sent_at);
CREATE INDEX IF NOT EXISTS idx_message_log_redacted ON message_log(redacted_at) WHERE redacted_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_message_log_replaces ON message_log(replaces_event_id) WHERE replaces_event_id != '';
`

func createMessageLogSchema(database *sql.DB) error {
	if _, err := database.Exec(messageLogSchema); err != nil {
		return fmt.Errorf("message log schema: %w", err)
	}
	return nil
}

// LoggedMessage is one row of the message log.
type LoggedMessage struct {
	EventID            string     `json:"event_id"`
	RoomID             string     `json:"room_id"`
	RoomName           string     `json:"room_name"`
	Platform           string     `json:"platform"`
	NetworkID          string     `json:"network_id"`
	NetworkName        string     `json:"network_name"`
	ChannelID          string     `json:"channel_id"`
	ChannelName        string     `json:"channel_name"`
	ChannelExternalURL string     `json:"channel_external_url"`
	SenderUserID       string     `json:"sender_user_id"`
	SenderDisplayName  string     `json:"sender_display_name"`
	MessageType        string     `json:"message_type"`
	Body               string     `json:"body"`
	ContentJSON        string     `json:"content_json"`
	ReplacesEventID    string     `json:"replaces_event_id"`
	SentAt             time.Time  `json:"sent_at"`
	LoggedAt           time.Time  `json:"logged_at"`
	RedactedAt         *time.Time `json:"redacted_at"`
	RedactionEventID   string     `json:"redaction_event_id"`
	RedactedByUserID   string     `json:"redacted_by_user_id"`
}

// LogMessage stores a message. Logging the same event twice (a re-read sync
// batch) keeps the first row.
func LogMessage(database *sql.DB, m LoggedMessage) error {
	_, err := database.Exec(`INSERT INTO message_log (event_id, room_id, room_name, platform, network_id, network_name,
		channel_id, channel_name, channel_external_url, sender_user_id, sender_display_name, message_type, body,
		content_json, replaces_event_id, sent_at, redacted_at, redaction_event_id, redacted_by_user_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(event_id) DO NOTHING`,
		m.EventID, m.RoomID, m.RoomName, m.Platform, m.NetworkID, m.NetworkName,
		m.ChannelID, m.ChannelName, m.ChannelExternalURL, m.SenderUserID, m.SenderDisplayName, m.MessageType, m.Body,
		m.ContentJSON, m.ReplacesEventID, m.SentAt.UTC(), nullableTime(m.RedactedAt), m.RedactionEventID, m.RedactedByUserID)
	if err != nil {
		return fmt.Errorf("log message %s: %w", m.EventID, err)
	}
	return nil
}

// MarkMessageRedacted records that a logged message was deleted. It reports
// whether the message was in the log; a redaction of a message logged before
// the log existed has nothing to mark.
func MarkMessageRedacted(database *sql.DB, eventID, redactionEventID, redactedByUserID string, redactedAt time.Time) (bool, error) {
	result, err := database.Exec(`UPDATE message_log SET redacted_at = ?, redaction_event_id = ?, redacted_by_user_id = ?
		WHERE event_id = ? AND redacted_at IS NULL`, redactedAt.UTC(), redactionEventID, redactedByUserID, eventID)
	if err != nil {
		return false, fmt.Errorf("mark %s redacted: %w", eventID, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}

// MessageLogHasEvent reports whether an event is already in the log.
func MessageLogHasEvent(database *sql.DB, eventID string) (bool, error) {
	var one int
	err := database.QueryRow(`SELECT 1 FROM message_log WHERE event_id = ?`, eventID).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("look up %s in message log: %w", eventID, err)
	}
	return true, nil
}

// MessageLogFilter narrows a message log listing. Empty fields match anything.
type MessageLogFilter struct {
	Platform     string
	NetworkID    string
	RoomID       string
	SenderUserID string
	Query        string // substring of the body
	DeletedOnly  bool
	Before       time.Time // sent strictly before; zero means now
	Limit        int
}

// ListLoggedMessages returns messages newest first.
func ListLoggedMessages(database *sql.DB, filter MessageLogFilter) ([]LoggedMessage, error) {
	var where []string
	var args []any
	add := func(clause string, arg any) {
		where = append(where, clause)
		args = append(args, arg)
	}
	if filter.Platform != "" {
		add("platform = ?", filter.Platform)
	}
	if filter.NetworkID != "" {
		add("network_id = ?", filter.NetworkID)
	}
	if filter.RoomID != "" {
		add("room_id = ?", filter.RoomID)
	}
	if filter.SenderUserID != "" {
		add("sender_user_id = ?", filter.SenderUserID)
	}
	if filter.Query != "" {
		add("instr(lower(body), lower(?)) > 0", filter.Query)
	}
	if filter.DeletedOnly {
		where = append(where, "redacted_at IS NOT NULL")
	}
	if !filter.Before.IsZero() {
		add("sent_at < ?", filter.Before.UTC())
	}
	query := `SELECT event_id, room_id, room_name, platform, network_id, network_name, channel_id, channel_name,
		channel_external_url, sender_user_id, sender_display_name, message_type, body, content_json, replaces_event_id,
		sent_at, logged_at, redacted_at, redaction_event_id, redacted_by_user_id FROM message_log`
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY sent_at DESC, event_id DESC LIMIT ?"
	args = append(args, filter.Limit)
	rows, err := database.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list message log: %w", err)
	}
	defer rows.Close()
	out := []LoggedMessage{}
	for rows.Next() {
		var m LoggedMessage
		var redactedAt sql.NullTime
		if err := rows.Scan(&m.EventID, &m.RoomID, &m.RoomName, &m.Platform, &m.NetworkID, &m.NetworkName, &m.ChannelID,
			&m.ChannelName, &m.ChannelExternalURL, &m.SenderUserID, &m.SenderDisplayName, &m.MessageType, &m.Body,
			&m.ContentJSON, &m.ReplacesEventID, &m.SentAt, &m.LoggedAt, &redactedAt, &m.RedactionEventID,
			&m.RedactedByUserID); err != nil {
			return nil, fmt.Errorf("scan message log: %w", err)
		}
		if redactedAt.Valid {
			t := redactedAt.Time
			m.RedactedAt = &t
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// EditsOf returns the edits made to the given messages, oldest first, keyed
// by the message they edit.
func EditsOf(database *sql.DB, eventIDs []string) (map[string][]LoggedMessage, error) {
	out := map[string][]LoggedMessage{}
	if len(eventIDs) == 0 {
		return out, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(eventIDs)), ",")
	args := make([]any, len(eventIDs))
	for i, id := range eventIDs {
		args[i] = id
	}
	rows, err := database.Query(`SELECT event_id, replaces_event_id, body, sent_at FROM message_log
		WHERE replaces_event_id IN (`+placeholders+`) ORDER BY sent_at`, args...)
	if err != nil {
		return nil, fmt.Errorf("edits: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var m LoggedMessage
		if err := rows.Scan(&m.EventID, &m.ReplacesEventID, &m.Body, &m.SentAt); err != nil {
			return nil, fmt.Errorf("scan edit: %w", err)
		}
		out[m.ReplacesEventID] = append(out[m.ReplacesEventID], m)
	}
	return out, rows.Err()
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}

const messageLogStartedAtKey = "message_log_started_at"

// MessageLogStartedAt returns when the log first ran, recording now on the
// first call. Filling a gap never reaches further back than this, so the
// first gap after the log starts does not page through each room's whole
// history.
func MessageLogStartedAt(database *sql.DB) (time.Time, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := database.Exec(`INSERT INTO matrix_sync_state (key, value) VALUES (?, ?) ON CONFLICT(key) DO NOTHING`,
		messageLogStartedAtKey, now); err != nil {
		return time.Time{}, fmt.Errorf("record message log start: %w", err)
	}
	var value string
	if err := database.QueryRow(`SELECT value FROM matrix_sync_state WHERE key = ?`, messageLogStartedAtKey).Scan(&value); err != nil {
		return time.Time{}, fmt.Errorf("read message log start: %w", err)
	}
	return time.Parse(time.RFC3339Nano, value)
}

// ChannelLogCounts is what the message log holds for one channel.
type ChannelLogCounts struct {
	LoggedMessageCount  int
	DeletedMessageCount int
	LastLoggedMessageAt *time.Time
}

// MessageLogCountsByChannel counts logged messages on one platform, keyed by
// the platform's channel id (from the room's m.bridge state). Edits are not
// counted: they are rows of their own, but not messages anyone sent.
func MessageLogCountsByChannel(database *sql.DB, platform string) (map[string]ChannelLogCounts, error) {
	rows, err := database.Query(`SELECT channel_id, count(*), count(redacted_at), max(sent_at) FROM message_log
		WHERE platform = ? AND replaces_event_id = '' AND channel_id != '' GROUP BY channel_id`, platform)
	if err != nil {
		return nil, fmt.Errorf("count message log by channel: %w", err)
	}
	defer rows.Close()
	out := map[string]ChannelLogCounts{}
	for rows.Next() {
		var channelID, lastSentAt string
		var counts ChannelLogCounts
		if err := rows.Scan(&channelID, &counts.LoggedMessageCount, &counts.DeletedMessageCount, &lastSentAt); err != nil {
			return nil, fmt.Errorf("scan message log counts: %w", err)
		}
		// max() drops the column's DATETIME type, so the driver hands back
		// the stored text; parse it with the formats the driver writes.
		last, err := parseStoredTime(lastSentAt)
		if err != nil {
			return nil, fmt.Errorf("newest sent_at of channel %s: %w", channelID, err)
		}
		counts.LastLoggedMessageAt = &last
		out[channelID] = counts
	}
	return out, rows.Err()
}

func parseStoredTime(value string) (time.Time, error) {
	for _, format := range sqlite3.SQLiteTimestampFormats {
		if t, err := time.ParseInLocation(format, value, time.UTC); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognised stored time %q", value)
}
