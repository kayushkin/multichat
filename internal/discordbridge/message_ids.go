package discordbridge

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/kayushkin/multichat/internal/db"
)

// lookupBatchSize bounds the event ids asked of the bridge in one query,
// under SQLite's default limit on bound parameters.
const lookupBatchSize = 500

// MessagePartsOf reads from the bridge's message table which Discord message
// each Matrix event is part of. An event the bridge does not know (an edit, a
// message deleted on Discord, whose row the bridge removes) is absent.
func MessagePartsOf(bridge *sql.DB, eventIDs []string) ([]db.DiscordMessagePart, error) {
	var parts []db.DiscordMessagePart
	for start := 0; start < len(eventIDs); start += lookupBatchSize {
		batch := eventIDs[start:min(start+lookupBatchSize, len(eventIDs))]
		args := make([]any, len(batch))
		for index, eventID := range batch {
			args[index] = eventID
		}
		rows, err := bridge.Query(`SELECT mxid, dcid, COALESCE(dc_attachment_id, '') FROM message
			WHERE mxid IN (`+strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")+`) AND dcid IS NOT NULL AND dcid != ''`, args...)
		if err != nil {
			return nil, fmt.Errorf("read Discord message ids from DISCORD_DB: %w", err)
		}
		for rows.Next() {
			var part db.DiscordMessagePart
			if err := rows.Scan(&part.EventID, &part.DiscordMessageID, &part.DiscordAttachmentID); err != nil {
				rows.Close()
				return nil, fmt.Errorf("read Discord message ids from DISCORD_DB: %w", err)
			}
			parts = append(parts, part)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("read Discord message ids from DISCORD_DB: %w", err)
		}
	}
	return parts, nil
}

// RecordDiscordMessageIDs copies the Discord message id of every live
// Discord row logged at or after loggedSince (zero: every row) that has none
// yet. It answers how many it recorded and how many the bridge did not know.
func RecordDiscordMessageIDs(multichat, bridge *sql.DB, loggedSince time.Time) (recorded, unknown int, err error) {
	eventIDs, err := db.LiveDiscordEventsWithoutDiscordMessageID(multichat, loggedSince)
	if err != nil {
		return 0, 0, err
	}
	if len(eventIDs) == 0 {
		return 0, 0, nil
	}
	parts, err := MessagePartsOf(bridge, eventIDs)
	if err != nil {
		return 0, 0, err
	}
	recorded, err = db.RecordDiscordMessageParts(multichat, parts)
	if err != nil {
		return 0, 0, err
	}
	return recorded, len(eventIDs) - len(parts), nil
}

// DiscordMessageIDRecorder copies Discord message ids into the message log's
// discord_message_parts. The sync worker logs an event the moment Synapse
// hands it over, which can be before the bridge has stored its own row, so
// the ids are copied on a short pause instead, over the rows logged in the
// last RecentWindow. A message deleted on Discord before a pass reaches it
// keeps no id: the bridge has already removed its row.
type DiscordMessageIDRecorder struct {
	Multichat, Bridge *sql.DB
	Every             time.Duration
	RecentWindow      time.Duration
}

// BackfillAll copies the id of every row that has none; the server runs it
// once at start.
func (r DiscordMessageIDRecorder) BackfillAll() error {
	recorded, unknown, err := RecordDiscordMessageIDs(r.Multichat, r.Bridge, time.Time{})
	if err != nil {
		return err
	}
	log.Printf("Discord message ids: recorded %d at start; the bridge knows no id for %d live Discord rows (deleted on Discord before their id was copied)", recorded, unknown)
	return nil
}

// Run blocks forever. A failed pass is logged and the next one tries again.
func (r DiscordMessageIDRecorder) Run() {
	for {
		time.Sleep(r.Every)
		recorded, _, err := RecordDiscordMessageIDs(r.Multichat, r.Bridge, time.Now().Add(-r.RecentWindow))
		if err != nil {
			log.Printf("Discord message ids: pass failed: %v", err)
			continue
		}
		if recorded > 0 {
			log.Printf("Discord message ids: recorded %d", recorded)
		}
	}
}
