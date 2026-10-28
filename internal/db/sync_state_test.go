package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// A database made while the inbound-rule dispatcher existed loses its two
// tables on start, rows and all, and a second start changes nothing.
func TestStartDropsTheInboundRuleTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "multichat.db")
	old, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`
CREATE TABLE inbound_rules (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL);
CREATE TABLE inbound_dispatches (id INTEGER PRIMARY KEY AUTOINCREMENT, rule_id INTEGER NOT NULL REFERENCES inbound_rules(id));
INSERT INTO inbound_rules (name) VALUES ('a test rule');
INSERT INTO inbound_dispatches (rule_id) VALUES (1);`); err != nil {
		t.Fatal(err)
	}
	old.Close()

	for start := 1; start <= 2; start++ {
		database, err := Initialize(path)
		if err != nil {
			t.Fatalf("start %d: %v", start, err)
		}
		var left int
		if err := database.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name IN ('inbound_rules', 'inbound_dispatches')`).Scan(&left); err != nil {
			t.Fatal(err)
		}
		if left != 0 {
			t.Errorf("start %d left %d inbound-rule tables", start, left)
		}
		if _, err := database.Exec(`SELECT key FROM matrix_sync_state`); err != nil {
			t.Errorf("start %d: matrix_sync_state: %v", start, err)
		}
		database.Close()
	}
}
