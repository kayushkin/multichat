package db

import (
	"database/sql"
	"fmt"
	"time"
)

// A live Discord row in the message log is one Matrix event, and the event
// carries no Discord id. mautrix-discord keeps the mapping in its own
// database, but it deletes a message's row there when the message is deleted
// on Discord, which is exactly when the log is the only copy left. So the log
// copies the mapping here as soon as it can and keeps it.
//
// It is a table of its own rather than message_log.discord_message_id
// because one Discord message can be several Matrix events (its text and
// each attachment), and that column is UNIQUE: an archive row is the whole
// Discord message.
const discordMessagePartsSchema = `
CREATE TABLE IF NOT EXISTS discord_message_parts (
	event_id              TEXT PRIMARY KEY,           -- message_log.event_id of a live Discord row
	discord_message_id    TEXT NOT NULL,              -- the Discord message the event is part of
	discord_attachment_id TEXT NOT NULL,              -- '' for the message's text, else the attachment's id
	recorded_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_discord_message_parts_message ON discord_message_parts(discord_message_id);
CREATE INDEX IF NOT EXISTS idx_message_log_logged_at ON message_log(logged_at);
`

// DiscordMessagePart says which Discord message a Matrix event is part of.
type DiscordMessagePart struct {
	EventID             string
	DiscordMessageID    string
	DiscordAttachmentID string
}

// LiveDiscordEventsWithoutDiscordMessageID lists the event ids of live
// Discord rows, edits left out, that have no Discord message id recorded
// and were logged at or after loggedSince; a zero loggedSince reads them all.
func LiveDiscordEventsWithoutDiscordMessageID(database *sql.DB, loggedSince time.Time) ([]string, error) {
	query := `SELECT event_id FROM message_log
		WHERE source = ? AND platform = 'discord' AND replaces_event_id = '' AND event_id IS NOT NULL
		AND NOT EXISTS (SELECT 1 FROM discord_message_parts p WHERE p.event_id = message_log.event_id)`
	args := []any{MessageLogSourceMatrix}
	if !loggedSince.IsZero() {
		query += ` AND logged_at >= ?`
		args = append(args, loggedSince.UTC())
	}
	rows, err := database.Query(query+` ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("list live Discord rows without a Discord message id: %w", err)
	}
	defer rows.Close()
	var eventIDs []string
	for rows.Next() {
		var eventID string
		if err := rows.Scan(&eventID); err != nil {
			return nil, fmt.Errorf("list live Discord rows without a Discord message id: %w", err)
		}
		eventIDs = append(eventIDs, eventID)
	}
	return eventIDs, rows.Err()
}

// RecordDiscordMessageParts stores which Discord message each event is part
// of, in one transaction. A part already recorded keeps its first value.
func RecordDiscordMessageParts(database *sql.DB, parts []DiscordMessagePart) (int, error) {
	tx, err := database.Begin()
	if err != nil {
		return 0, fmt.Errorf("record Discord message ids: %w", err)
	}
	defer tx.Rollback()
	recorded := 0
	for _, part := range parts {
		if part.EventID == "" || part.DiscordMessageID == "" {
			return 0, fmt.Errorf("record Discord message ids: a part needs an event id and a Discord message id: %+v", part)
		}
		result, err := tx.Exec(`INSERT INTO discord_message_parts (event_id, discord_message_id, discord_attachment_id)
			VALUES (?, ?, ?) ON CONFLICT(event_id) DO NOTHING`, part.EventID, part.DiscordMessageID, part.DiscordAttachmentID)
		if err != nil {
			return 0, fmt.Errorf("record Discord message id of %s: %w", part.EventID, err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		recorded += int(affected)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("record Discord message ids: %w", err)
	}
	return recorded, nil
}
