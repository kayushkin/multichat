package db

import (
	"database/sql"
	"errors"
	"fmt"
)

// syncStateSchema holds the /sync resume token.
const syncStateSchema = `
CREATE TABLE IF NOT EXISTS matrix_sync_state (
	key        TEXT PRIMARY KEY,
	value      TEXT NOT NULL,
	updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

`

func createSyncStateSchema(database *sql.DB) error {
	if _, err := database.Exec(syncStateSchema); err != nil {
		return fmt.Errorf("sync state schema: %w", err)
	}
	return nil
}

// dropInboundRuleTables removes the tables of the inbound-rule dispatcher,
// which turned a message from a person into an agent session. Scheduler jobs
// with an event_trigger on messaging.inbound replaced it on 2026-10-28; when
// it went, inbound_rules was empty and inbound_dispatches held one stale test
// row. inbound_dispatches goes first because it references inbound_rules.
func dropInboundRuleTables(database *sql.DB) error {
	for _, table := range []string{"inbound_dispatches", "inbound_rules"} {
		if _, err := database.Exec(`DROP TABLE IF EXISTS ` + table); err != nil {
			return fmt.Errorf("drop %s: %w", table, err)
		}
	}
	return nil
}

// --- sync state ---------------------------------------------------------------

const syncStateNextBatchKey = "next_batch"

// SyncNextBatch returns the stored /sync resume token, "" when there is none.
func SyncNextBatch(database *sql.DB) (string, error) {
	var value string
	err := database.QueryRow(`SELECT value FROM matrix_sync_state WHERE key = ?`, syncStateNextBatchKey).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read sync next_batch: %w", err)
	}
	return value, nil
}

// SaveSyncNextBatch stores the /sync resume token.
func SaveSyncNextBatch(database *sql.DB, nextBatch string) error {
	_, err := database.Exec(`INSERT INTO matrix_sync_state (key, value, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP`, syncStateNextBatchKey, nextBatch)
	if err != nil {
		return fmt.Errorf("save sync next_batch: %w", err)
	}
	return nil
}
