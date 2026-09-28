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
