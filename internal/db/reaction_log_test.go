package db

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestReactionsAreGroupedByKeyAndARemovedOneIsLeftOut(t *testing.T) {
	database, err := Initialize(filepath.Join(t.TempDir(), "multichat.db"))
	if err != nil {
		t.Fatal(err)
	}
	sent := time.Date(2026, 9, 28, 17, 0, 0, 0, time.UTC)
	reactions := []LoggedReaction{
		{EventID: "$r1", TargetEventID: "$message", SenderDisplayName: "Milo", Key: "🤣", SentAt: sent},
		{EventID: "$r2", TargetEventID: "$message", SenderDisplayName: "Jade", Key: "mxc://x/this", Shortcode: ":this:", SentAt: sent.Add(time.Second)},
		{EventID: "$r3", TargetEventID: "$message", SenderDisplayName: "Neil", Key: "🤣", SentAt: sent.Add(2 * time.Second)},
		{EventID: "$r4", TargetEventID: "$message", SenderDisplayName: "Dean", Key: "🤣", SentAt: sent.Add(3 * time.Second)},
		{EventID: "$r5", TargetEventID: "$other", SenderDisplayName: "Milo", Key: "👍", SentAt: sent},
	}
	for _, r := range append(reactions, reactions[0]) { // the repeat is a re-read batch
		r.RoomID, r.Platform, r.SenderUserID, r.ContentJSON = "!general", "discord", "@discord_1:x", `{}`
		if err := LogReaction(database, r); err != nil {
			t.Fatal(err)
		}
	}
	// A redaction hits a reaction: it is not a message, so the message mark misses.
	if marked, err := MarkMessageRedacted(database, "$r4", "$redaction", "@discord_9:x", sent.Add(time.Minute)); err != nil || marked {
		t.Fatalf("a reaction was marked as a message: %v %v", marked, err)
	}
	if marked, err := MarkReactionRedacted(database, "$r4", "$redaction", "@discord_9:x", sent.Add(time.Minute)); err != nil || !marked {
		t.Fatalf("marked=%v err=%v", marked, err)
	}
	if logged, err := MessageLogHasEvent(database, "$r2"); err != nil || !logged {
		t.Errorf("a logged reaction counts as a logged event for gap filling: %v %v", logged, err)
	}

	got, err := ReactionsTo(database, []string{"$message", "", "$none"}, OwnSenders{})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]MessageReactionGroup{"$message": {
		{Key: "🤣", Count: 2, SenderDisplayNames: []string{"Milo", "Neil"}},
		{Key: "mxc://x/this", Shortcode: ":this:", Count: 1, SenderDisplayNames: []string{"Jade"}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
}

func TestArchiveRowsAreKeyedByDiscordIDAndCarryDiscordReactions(t *testing.T) {
	database, err := Initialize(filepath.Join(t.TempDir(), "multichat.db"))
	if err != nil {
		t.Fatal(err)
	}
	sent := time.Date(2026, 9, 21, 16, 3, 27, 0, time.UTC)
	archived := LoggedMessage{DiscordMessageID: "9400000000000000001", RoomID: "!general", Platform: "discord",
		ChannelID: "10", SenderUserID: "@discord_920000000000000005:x", SenderDisplayName: "arrowdarts",
		Body: "hi <:this:940000000000000002>", Format: MessageFormatDiscordMarkdown, SentAt: sent,
		ContentJSON: `{"id":"9400000000000000001","reactions":[{"emoji":{"id":null,"name":"😂"},"count":3},{"emoji":{"id":"940000000000000002","name":"this"},"count":1}]}`}
	for i, wantInserted := range []bool{true, false} {
		inserted, err := LogArchivedDiscordMessage(database, archived)
		if err != nil || inserted != wantInserted {
			t.Fatalf("import %d: inserted=%v err=%v", i, inserted, err)
		}
	}
	if err := LogMessage(database, LoggedMessage{EventID: "$live", RoomID: "!general", Platform: "discord", ChannelID: "10",
		SenderUserID: "@discord_1:x", ContentJSON: `{}`, SentAt: sent.Add(24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	messages, err := ListLoggedMessages(database, MessageLogFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[1].EventID != "" || messages[1].Source != MessageLogSourceDiscordArchive ||
		messages[1].DiscordMessageID != "9400000000000000001" || messages[0].Source != MessageLogSourceMatrix {
		t.Fatalf("messages = %+v", messages)
	}
	reactions, err := MessageLogReactions(database, messages, OwnSenders{})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]MessageReactionGroup{"discord:9400000000000000001": {
		{Key: "😂", Count: 3, SenderDisplayNames: []string{}},
		{Key: "discord-emoji:940000000000000002", Shortcode: ":this:", Count: 1, SenderDisplayNames: []string{}},
	}}
	if !reflect.DeepEqual(reactions, want) {
		t.Errorf("got %+v\nwant %+v", reactions, want)
	}
	counts, err := MessageLogCountsByChannel(database, "discord")
	if err != nil || counts["10"].LoggedMessageCount != len(messages) {
		t.Errorf("the status count must equal what the log lists: %+v %v", counts, err)
	}
	if _, err := LogArchivedDiscordMessage(database, LoggedMessage{DiscordMessageID: "1", EventID: "$x"}); err == nil {
		t.Error("an archive row with a Matrix event id was accepted")
	}
}
