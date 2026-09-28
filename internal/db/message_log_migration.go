package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"

	"github.com/kayushkin/multichat/internal/matrix"
)

// migrateMessageLogToRowIDs moves a message log made before 2026-09-28's
// emoji and archive work — keyed by event_id, with no row id, no source and
// no formatting or media columns — to the current shape, in one transaction:
// a new table, a copy of every row, then the new columns filled from each
// row's content_json with the same parser the sync worker uses, then the old
// table dropped and the new one renamed. Rows are copied oldest first, so row
// ids follow the order messages were sent. A log already in the current shape,
// or none at all, is left alone.
func migrateMessageLogToRowIDs(database *sql.DB) error {
	columns, err := tableColumns(database, "message_log")
	if err != nil {
		return err
	}
	if len(columns) == 0 || columns["id"] {
		return nil
	}
	tx, err := database.Begin()
	if err != nil {
		return fmt.Errorf("migrate message log: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`CREATE TABLE message_log_migrating ` + messageLogColumnDefinitions); err != nil {
		return fmt.Errorf("migrate message log: create: %w", err)
	}
	const copied = `event_id, room_id, room_name, platform, network_id, network_name, channel_id, channel_name,
		channel_external_url, sender_user_id, sender_display_name, message_type, body, content_json, replaces_event_id,
		sent_at, logged_at, redacted_at, redaction_event_id, redacted_by_user_id`
	result, err := tx.Exec(`INSERT INTO message_log_migrating (source, ` + copied + `)
		SELECT '` + MessageLogSourceMatrix + `', ` + copied + ` FROM message_log ORDER BY sent_at, logged_at, event_id`)
	if err != nil {
		return fmt.Errorf("migrate message log: copy: %w", err)
	}
	copiedCount, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if err := backfillMessageContentColumns(tx); err != nil {
		return err
	}
	if _, err := tx.Exec(`DROP TABLE message_log`); err != nil {
		return fmt.Errorf("migrate message log: drop old table: %w", err)
	}
	if _, err := tx.Exec(`ALTER TABLE message_log_migrating RENAME TO message_log`); err != nil {
		return fmt.Errorf("migrate message log: rename: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("migrate message log: commit: %w", err)
	}
	log.Printf("message log: migrated %d rows to row ids and content columns", copiedCount)
	return nil
}

// backfillMessageContentColumns fills format, formatted_body, media and reply
// columns from content_json. An edit takes them from its m.new_content, as
// its body already was.
func backfillMessageContentColumns(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT id, content_json FROM message_log_migrating`)
	if err != nil {
		return fmt.Errorf("migrate message log: read content: %w", err)
	}
	type update struct {
		id    int64
		facts matrix.MessageContentFacts
	}
	var updates []update
	for rows.Next() {
		var id int64
		var contentJSON string
		if err := rows.Scan(&id, &contentJSON); err != nil {
			rows.Close()
			return fmt.Errorf("migrate message log: scan content: %w", err)
		}
		facts, err := matrix.ParseMessageContent(json.RawMessage(contentJSON))
		if err != nil {
			rows.Close()
			return fmt.Errorf("migrate message log: row %d: %w", id, err)
		}
		updates = append(updates, update{id, LoggedContentOf(facts)})
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, u := range updates {
		if _, err := tx.Exec(`UPDATE message_log_migrating SET format = ?, formatted_body = ?, media_url = ?,
			media_mimetype = ?, reply_to_event_id = ? WHERE id = ?`,
			u.facts.Format, u.facts.FormattedBody, u.facts.MediaURL, u.facts.MediaMimetype, u.facts.ReplyToEventID, u.id); err != nil {
			return fmt.Errorf("migrate message log: fill row %d: %w", u.id, err)
		}
	}
	return nil
}

// LoggedContentOf picks what the log keeps of a message's content: an edit's
// m.new_content when it has one (the top level is a "* new text" fallback),
// else the content itself. The reply is always the top level's, and so is
// ReplacesEventID.
func LoggedContentOf(facts matrix.MessageContentFacts) matrix.MessageContentFacts {
	if facts.NewContent == nil {
		return facts
	}
	logged := *facts.NewContent
	logged.MsgType = facts.MsgType
	logged.ReplyToEventID = facts.ReplyToEventID
	logged.ReplacesEventID = facts.ReplacesEventID
	logged.NewContent = nil
	return logged
}

func tableColumns(database *sql.DB, table string) (map[string]bool, error) {
	rows, err := database.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return nil, fmt.Errorf("columns of %s: %w", table, err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}
