package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kayushkin/multichat/internal/db"
)

// The guild and portal columns the import reads, as in mautrix-discord's
// schema (sqlite3 .schema on the live bridge database, 2026-09-28).
const bridgeSchema = `
CREATE TABLE guild (dcid TEXT PRIMARY KEY, mxid TEXT UNIQUE, plain_name TEXT NOT NULL, name TEXT NOT NULL,
	name_set BOOLEAN NOT NULL, avatar TEXT NOT NULL, avatar_url TEXT NOT NULL, avatar_set BOOLEAN NOT NULL,
	bridging_mode INTEGER NOT NULL);
CREATE TABLE portal (dcid TEXT, receiver TEXT, other_user_id TEXT, type INTEGER NOT NULL, dc_guild_id TEXT,
	dc_parent_id TEXT, dc_parent_receiver TEXT NOT NULL DEFAULT '', mxid TEXT UNIQUE, plain_name TEXT NOT NULL,
	name TEXT NOT NULL, PRIMARY KEY (dcid, receiver));
INSERT INTO guild VALUES ('500', '!space:chat.example', 'Riverside', 'Riverside social', 1, '', '', 1, 3);
INSERT INTO portal VALUES ('600', '', NULL, 0, '500', NULL, '', '!general:chat.example', 'general', '#general');
INSERT INTO portal VALUES ('600', '999', NULL, 1, NULL, NULL, '', '!dm:chat.example', 'dm', 'a DM with the same id');
`

func TestImportAddsArchiveRowsBeforeTheLiveLogOnce(t *testing.T) {
	directory := t.TempDir()
	bridge, err := sql.Open("sqlite3", filepath.Join(directory, "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bridge.Exec(bridgeSchema); err != nil {
		t.Fatal(err)
	}
	database, err := db.Initialize(filepath.Join(directory, "multichat.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO matrix_sync_state (key, value) VALUES ('message_log_started_at', '2026-09-28T17:15:17.945511378Z')`); err != nil {
		t.Fatal(err)
	}
	// The live sync has logged the room from 17:15:17; the archive keeps only
	// what came before.
	if err := db.LogMessage(database, db.LoggedMessage{EventID: "$live", RoomID: "!general:chat.example", Platform: "discord",
		SenderUserID: "@discord_11:chat.example", Body: "the first live message",
		SentAt: time.Date(2026, 9, 28, 17, 15, 17, 0, time.UTC)}); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(directory, "archive.jsonl")
	lines := []string{ // newest first, as a history fetch lists them
		`{"id":"3","channel_id":"600","content":"after the live log began","timestamp":"2026-09-28T17:20:00+00:00","author":{"id":"11","username":"late","global_name":null}}`,
		`{"id":"2","channel_id":"600","content":"hi <:this:899>","timestamp":"2026-09-28T16:00:00.5+00:00","author":{"id":"12","username":"arrowdarts","global_name":"Arrow"},"reactions":[{"emoji":{"id":null,"name":"😂"},"count":2}]}`,
		``,
		`{"id":"1","channel_id":"600","content":"","timestamp":"2026-09-21T16:03:27.379+00:00","author":{"id":"13","username":"joiner","global_name":""},"type":7}`,
	}
	if err := os.WriteFile(archive, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := importArchive(database, bridge, archive)
	if err != nil {
		t.Fatal(err)
	}
	if result != (importResult{Read: 3, Inserted: 2, AfterLiveLog: 1}) {
		t.Fatalf("first run = %+v", result)
	}
	again, err := importArchive(database, bridge, archive)
	if err != nil || again != (importResult{Read: 3, AlreadyLogged: 2, AfterLiveLog: 1}) {
		t.Fatalf("second run = %+v, %v", again, err)
	}

	messages, err := db.ListLoggedMessages(database, db.MessageLogFilter{RoomID: "!general:chat.example", Limit: 10})
	if err == nil {
		kept := messages[:0]
		for _, m := range messages {
			if m.Source == db.MessageLogSourceDiscordArchive {
				kept = append(kept, m)
			}
		}
		messages = kept
	}
	if err != nil || len(messages) != 2 {
		t.Fatalf("messages = %+v, %v", messages, err)
	}
	newest, oldest := messages[0], messages[1]
	if oldest.ID >= newest.ID {
		t.Errorf("ids should follow send order: %d then %d", oldest.ID, newest.ID)
	}
	want := db.LoggedMessage{ID: newest.ID, Ref: db.MessageRef(newest.ID), DiscordMessageID: "2", Source: db.MessageLogSourceDiscordArchive,
		RoomID: "!general:chat.example", RoomName: "#general", Platform: "discord", NetworkID: "500",
		NetworkName: "Riverside social", ChannelID: "600", ChannelName: "#general",
		SenderUserID: "@discord_12:chat.example", SenderDisplayName: "Arrow", Body: "hi <:this:899>",
		Format: db.MessageFormatDiscordMarkdown, ContentJSON: lines[1],
		SentAt: time.Date(2026, 9, 28, 16, 0, 0, 500_000_000, time.UTC), LoggedAt: newest.LoggedAt}
	if newest != want {
		t.Errorf("got  %+v\nwant %+v", newest, want)
	}
	if oldest.SenderDisplayName != "joiner" {
		t.Errorf("an empty global_name falls to the username: %+v", oldest)
	}
}

// A server bridged after the log began has no live rows in its rooms, so the
// whole fetch is kept, the weeks after message_log_started_at included.
func TestARoomTheLiveLogNeverSawKeepsItsWholeHistory(t *testing.T) {
	directory := t.TempDir()
	bridge, err := sql.Open("sqlite3", filepath.Join(directory, "bridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bridge.Exec(bridgeSchema); err != nil {
		t.Fatal(err)
	}
	database, err := db.Initialize(filepath.Join(directory, "multichat.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO matrix_sync_state (key, value) VALUES ('message_log_started_at', '2026-09-28T17:15:17.945511378Z')`); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(directory, "archive.jsonl")
	lines := `{"id":"9","channel_id":"600","content":"long after the log began","timestamp":"2026-10-20T10:00:00+00:00","author":{"id":"11","username":"a","global_name":null}}
{"id":"8","channel_id":"600","content":"years before","timestamp":"2019-01-01T10:00:00+00:00","author":{"id":"12","username":"b","global_name":null}}
`
	if err := os.WriteFile(archive, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := importArchive(database, bridge, archive)
	if err != nil || result != (importResult{Read: 2, Inserted: 2}) {
		t.Fatalf("import = %+v, %v; want both kept", result, err)
	}
}
