package db

import (
	"path/filepath"
	"testing"
	"time"
)

func TestADeletedMessageKeepsItsTextAndEdits(t *testing.T) {
	database, err := Initialize(filepath.Join(t.TempDir(), "multichat.db"))
	if err != nil {
		t.Fatal(err)
	}
	sent := time.Date(2026, 9, 28, 7, 0, 0, 0, time.UTC)
	message := LoggedMessage{EventID: "$original", RoomID: "!general", Platform: "discord", SenderUserID: "@discord_1:x",
		Body: "what I first said", ContentJSON: `{"body":"what I first said"}`, SentAt: sent}
	edit := message
	edit.EventID, edit.Body, edit.ReplacesEventID, edit.SentAt = "$edit", "what I said after", "$original", sent.Add(time.Minute)
	other := message
	other.EventID, other.Body, other.SentAt = "$kept", "still here", sent.Add(2*time.Minute)
	for _, m := range []LoggedMessage{message, edit, other, message} { // the repeat is a re-read batch
		if err := LogMessage(database, m); err != nil {
			t.Fatal(err)
		}
	}

	marked, err := MarkMessageRedacted(database, "$original", "$redaction", "@discordbot:x", sent.Add(time.Hour))
	if err != nil || !marked {
		t.Fatalf("marked=%v err=%v", marked, err)
	}
	if marked, err := MarkMessageRedacted(database, "$never-logged", "$r2", "@discordbot:x", sent); err != nil || marked {
		t.Fatalf("an unlogged event was marked: %v %v", marked, err)
	}

	deleted, err := ListLoggedMessages(database, MessageLogFilter{DeletedOnly: true, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0].EventID != "$original" || deleted[0].Body != "what I first said" ||
		deleted[0].RedactedAt == nil || deleted[0].RedactionEventID != "$redaction" {
		t.Fatalf("deleted = %+v", deleted)
	}
	edits, err := EditsOf(database, []string{"$original"})
	if err != nil {
		t.Fatal(err)
	}
	if got := edits["$original"]; len(got) != 1 || got[0].Body != "what I said after" {
		t.Fatalf("edits = %+v", edits)
	}

	all, err := ListLoggedMessages(database, MessageLogFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 || all[0].EventID != "$kept" {
		t.Fatalf("newest first, three rows: %+v", all)
	}
	older, err := ListLoggedMessages(database, MessageLogFilter{Before: all[0].SentAt, Query: "SAID", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(older) != 2 {
		t.Fatalf("before and q should leave the two older rows: %+v", older)
	}
}

func TestTheMessageLogStartIsRecordedOnce(t *testing.T) {
	database, err := Initialize(filepath.Join(t.TempDir(), "multichat.db"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := MessageLogStartedAt(database)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	second, err := MessageLogStartedAt(database)
	if err != nil || !second.Equal(first) {
		t.Fatalf("first %v, second %v, err %v", first, second, err)
	}
}

func TestMessageLogCountsByChannelCountsMessagesNotEdits(t *testing.T) {
	database, err := Initialize(filepath.Join(t.TempDir(), "multichat.db"))
	if err != nil {
		t.Fatal(err)
	}
	sent := time.Date(2026, 9, 28, 7, 0, 0, 0, time.UTC)
	base := LoggedMessage{RoomID: "!general", Platform: "discord", ChannelID: "1001", SenderUserID: "@discord_1:x",
		Body: "hello", ContentJSON: `{}`, SentAt: sent}
	rows := []LoggedMessage{base, base, base, base, base}
	rows[0].EventID = "$first"
	rows[1].EventID, rows[1].SentAt = "$second", sent.Add(time.Hour)
	rows[2].EventID, rows[2].ReplacesEventID, rows[2].SentAt = "$edit", "$first", sent.Add(2*time.Hour) // an edit, not counted
	rows[3].EventID, rows[3].ChannelID = "$elsewhere", "1002"
	rows[4].EventID, rows[4].Platform, rows[4].ChannelID = "$slack", "slack", "1001" // another platform
	for _, m := range rows {
		if err := LogMessage(database, m); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := MarkMessageRedacted(database, "$first", "$r", "@discordbot:x", sent.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}

	counts, err := MessageLogCountsByChannel(database, "discord")
	if err != nil {
		t.Fatal(err)
	}
	general := counts["1001"]
	if general.LoggedMessageCount != 2 || general.DeletedMessageCount != 1 {
		t.Errorf("1001 = %+v, want 2 logged and 1 deleted", general)
	}
	if general.LastLoggedMessageAt == nil || !general.LastLoggedMessageAt.Equal(sent.Add(time.Hour)) {
		t.Errorf("1001 newest = %v, want %v (the edit is later but not a message)", general.LastLoggedMessageAt, sent.Add(time.Hour))
	}
	if counts["1002"].LoggedMessageCount != 1 || len(counts) != 2 {
		t.Errorf("counts = %+v", counts)
	}
}
