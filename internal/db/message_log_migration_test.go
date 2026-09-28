package db

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// The message_log table exactly as it was created before row ids, source and
// the content columns (commit cb370dc).
const messageLogSchemaKeyedByEventID = `
CREATE TABLE message_log (
	event_id             TEXT PRIMARY KEY,
	room_id              TEXT NOT NULL,
	room_name            TEXT NOT NULL,
	platform             TEXT NOT NULL,
	network_id           TEXT NOT NULL,
	network_name         TEXT NOT NULL,
	channel_id           TEXT NOT NULL,
	channel_name         TEXT NOT NULL,
	channel_external_url TEXT NOT NULL,
	sender_user_id       TEXT NOT NULL,
	sender_display_name  TEXT NOT NULL,
	message_type         TEXT NOT NULL,
	body                 TEXT NOT NULL,
	content_json         TEXT NOT NULL,
	replaces_event_id    TEXT NOT NULL DEFAULT '',
	sent_at              DATETIME NOT NULL,
	logged_at            DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	redacted_at          DATETIME,
	redaction_event_id   TEXT NOT NULL DEFAULT '',
	redacted_by_user_id  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_message_log_room_sent ON message_log(room_id, sent_at);
CREATE INDEX idx_message_log_redacted ON message_log(redacted_at) WHERE redacted_at IS NOT NULL;
CREATE INDEX idx_message_log_replaces ON message_log(replaces_event_id) WHERE replaces_event_id != '';
`

func TestAnOldMessageLogIsMigratedWithItsRowsAndBackfilled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "multichat.db")
	old, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(messageLogSchemaKeyedByEventID); err != nil {
		t.Fatal(err)
	}
	sent := time.Date(2026, 9, 28, 17, 0, 0, 0, time.UTC)
	insert := func(eventID, body, contentJSON, replaces string, sentAt time.Time, redacted bool) {
		t.Helper()
		var redactedAt any
		if redacted {
			redactedAt = sentAt.Add(time.Minute)
		}
		if _, err := old.Exec(`INSERT INTO message_log (event_id, room_id, room_name, platform, network_id, network_name,
			channel_id, channel_name, channel_external_url, sender_user_id, sender_display_name, message_type, body,
			content_json, replaces_event_id, sent_at, redacted_at, redaction_event_id)
			VALUES (?, '!general', '#general', 'discord', '1', 'Riverside', '10', '#general', '', '@discord_1:x', 'Jade',
			'm.text', ?, ?, ?, ?, ?, '')`, eventID, body, contentJSON, replaces, sentAt, redactedAt); err != nil {
			t.Fatal(err)
		}
	}
	// Inserted out of order: the migration numbers rows by sent_at.
	insert("$emoji", "I dunno why", `{"body":"I dunno why","format":"org.matrix.custom.html","formatted_body":"I dunno why <img data-mx-emoticon src=\"mxc://x/Q\" alt=\":bearlaugh:\">","msgtype":"m.text","m.relates_to":{"m.in_reply_to":{"event_id":"$earlier"}}}`, "", sent.Add(time.Minute), false)
	insert("$image", "image0.gif", `{"body":"image0.gif","info":{"mimetype":"image/gif"},"msgtype":"m.image","url":"mxc://x/G"}`, "", sent, true)
	insert("$edit", "fixed", `{"body":"* fixed","msgtype":"m.text","m.new_content":{"body":"fixed","format":"org.matrix.custom.html","formatted_body":"<b>fixed</b>","msgtype":"m.text"},"m.relates_to":{"rel_type":"m.replace","event_id":"$emoji"}}`, "$emoji", sent.Add(2*time.Minute), false)
	insert("$gone", "", `{}`, "", sent.Add(3*time.Minute), true)
	old.Close()

	database, err := Initialize(path)
	if err != nil {
		t.Fatal(err)
	}
	messages, err := ListLoggedMessages(database, MessageLogFilter{Limit: 10, IncludeEdits: true})
	if err != nil {
		t.Fatal(err)
	}
	byEvent := map[string]LoggedMessage{}
	for _, m := range messages {
		byEvent[m.EventID] = m
	}
	if len(messages) != 4 {
		t.Fatalf("want every row kept, got %+v", messages)
	}
	if byEvent["$image"].ID != 1 || byEvent["$emoji"].ID != 2 || byEvent["$edit"].ID != 3 || byEvent["$gone"].ID != 4 {
		t.Errorf("ids should follow sent_at: %+v", byEvent)
	}
	emoji := byEvent["$emoji"]
	if emoji.Source != MessageLogSourceMatrix || emoji.Format != "org.matrix.custom.html" ||
		emoji.FormattedBody == "" || emoji.ReplyToEventID != "$earlier" || emoji.SenderDisplayName != "Jade" ||
		emoji.DiscordMessageID != "" {
		t.Errorf("emoji row = %+v", emoji)
	}
	image := byEvent["$image"]
	if image.MediaURL != "mxc://x/G" || image.MediaMimetype != "image/gif" || image.RedactedAt == nil || image.Format != "" {
		t.Errorf("image row = %+v", image)
	}
	edit := byEvent["$edit"]
	if edit.Body != "fixed" || edit.FormattedBody != "<b>fixed</b>" || edit.ReplacesEventID != "$emoji" {
		t.Errorf("an edit takes its formatting from m.new_content: %+v", edit)
	}

	// New rows continue the numbering; a second start migrates nothing.
	if err := LogMessage(database, LoggedMessage{EventID: "$new", RoomID: "!general", Platform: "discord",
		SenderUserID: "@discord_1:x", ContentJSON: `{}`, SentAt: sent.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	database.Close()
	again, err := Initialize(path)
	if err != nil {
		t.Fatal(err)
	}
	var count, maxID int
	if err := again.QueryRow(`SELECT count(*), max(id) FROM message_log`).Scan(&count, &maxID); err != nil {
		t.Fatal(err)
	}
	if count != 5 || maxID != 5 {
		t.Errorf("after a second start: %d rows, max id %d", count, maxID)
	}
	var migrating int
	if err := again.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'message_log_migrating'`).Scan(&migrating); err != nil || migrating != 0 {
		t.Errorf("the migration table was left behind: %d %v", migrating, err)
	}
}
