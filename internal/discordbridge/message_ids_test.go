package discordbridge

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/kayushkin/multichat/internal/db"
)

// A live Discord row gets the Discord message id the bridge mapped its event
// to, keeps it after the bridge forgets the message, and shares it with the
// other parts of the same Discord message; an archive row keeps its own.
func TestLiveDiscordRowsCarryTheirDiscordMessageID(t *testing.T) {
	multichat, err := db.Initialize(filepath.Join(t.TempDir(), "multichat.db"))
	if err != nil {
		t.Fatal(err)
	}
	bridgePath := filepath.Join(t.TempDir(), "mautrix-discord.db")
	writer, err := sql.Open("sqlite3", bridgePath)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := writer.Exec(bridgeSchema); err != nil {
		t.Fatal(err)
	}
	insertPart := func(dcid, attachment, mxid string) {
		t.Helper()
		if _, err := writer.Exec(`INSERT INTO message (dcid, dc_attachment_id, dc_chan_id, dc_chan_receiver, dc_sender, timestamp,
			dc_edit_timestamp, dc_thread_id, mxid) VALUES (?, ?, '1001', '', '7', 0, 0, '', ?)`, dcid, attachment, mxid); err != nil {
			t.Fatal(err)
		}
	}
	insertPart("5000", "", "$text")
	insertPart("5000", "6000", "$picture")
	bridge, err := Open(bridgePath)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()

	sent := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	live := func(eventID string, minutes int) db.LoggedMessage {
		return db.LoggedMessage{EventID: eventID, RoomID: "!general:x", Platform: "discord", ChannelID: "1001",
			SenderUserID: "@discord_7:x", Body: "hello", ContentJSON: `{}`, SentAt: sent.Add(time.Duration(minutes) * time.Minute)}
	}
	edit := live("$edit", 3)
	edit.ReplacesEventID = "$text"
	for _, m := range []db.LoggedMessage{live("$text", 0), live("$picture", 0), live("$not-yet-in-bridge", 1), edit} {
		if err := db.LogMessage(multichat, m); err != nil {
			t.Fatal(err)
		}
	}
	archive := live("", -60)
	archive.DiscordMessageID = "4000"
	if _, err := db.LogArchivedDiscordMessage(multichat, archive); err != nil {
		t.Fatal(err)
	}

	recorded, unknown, err := RecordDiscordMessageIDs(multichat, bridge, time.Time{})
	if err != nil || recorded != 2 || unknown != 1 {
		t.Fatalf("recorded=%d unknown=%d err=%v; want 2 and 1", recorded, unknown, err)
	}
	// The bridge stores the late message, then forgets the first one, as it
	// does when a message is deleted on Discord.
	insertPart("5001", "", "$not-yet-in-bridge")
	if _, err := writer.Exec(`DELETE FROM message WHERE dcid = '5000'`); err != nil {
		t.Fatal(err)
	}
	recorded, unknown, err = RecordDiscordMessageIDs(multichat, bridge, sent.Add(-24*time.Hour))
	if err != nil || recorded != 1 || unknown != 0 {
		t.Fatalf("second pass recorded=%d unknown=%d err=%v; want 1 and 0", recorded, unknown, err)
	}

	rows, err := db.ListLoggedMessages(multichat, db.MessageLogFilter{Limit: 10, IncludeEdits: true})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, row := range rows {
		key := row.EventID
		if key == "" {
			key = "archive"
		}
		got[key] = row.DiscordMessageID
	}
	want := map[string]string{"$text": "5000", "$picture": "5000", "$not-yet-in-bridge": "5001", "$edit": "", "archive": "4000"}
	for key, id := range want {
		if got[key] != id {
			t.Errorf("%s: discord_message_id %q, want %q (all: %v)", key, got[key], id, got)
		}
	}
}
