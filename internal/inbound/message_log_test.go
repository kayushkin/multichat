package inbound

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/matrix"
)

func TestTheLogWriterKeepsMessagesAndReactionsAndMarksWhicheverARedactionHits(t *testing.T) {
	synapse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"displayname":"Jade"}`)) // every profile lookup
	}))
	defer synapse.Close()
	database, err := db.Initialize(filepath.Join(t.TempDir(), "multichat.db"))
	if err != nil {
		t.Fatal(err)
	}
	worker := &SyncWorker{
		Matrix:   matrix.NewClient(synapse.URL, "token"),
		Database: database,
		rooms:    map[string]roomFacts{"!general": {Name: "#general"}},
		bridges:  map[string]matrix.BridgeInfo{"!general": {NetworkID: "500", ChannelID: "600"}},
	}
	event := func(id, eventType, sender, content string) matrix.TimelineEvent {
		return matrix.TimelineEvent{EventID: id, Type: eventType, Sender: sender, OriginServerTS: 1790616706478,
			Content: json.RawMessage(content)}
	}
	removedBeforeRead := event("$removed", "m.reaction", "@discord_3:x", `{}`)
	removedBeforeRead.Unsigned.RedactedBecause = &matrix.TimelineEvent{EventID: "$r0", Sender: "@discord_3:x"}
	events := []matrix.TimelineEvent{
		event("$message", "m.room.message", "@discord_1:x", `{"body":"hi","msgtype":"m.text","format":"org.matrix.custom.html","formatted_body":"hi <img data-mx-emoticon src=\"mxc://x/Q\" alt=\":bearlaugh:\">"}`),
		event("$laugh", "m.reaction", "@discord_2:x", `{"m.relates_to":{"event_id":"$message","key":"🤣","rel_type":"m.annotation"}}`),
		event("$this", "m.reaction", "@discord_3:x", `{"com.beeper.reaction.shortcode":":this:","m.relates_to":{"event_id":"$message","key":"mxc://x/this","rel_type":"m.annotation"}}`),
		event("$admin", "m.reaction", "@admin:x", `{"m.relates_to":{"event_id":"$message","key":"👍","rel_type":"m.annotation"}}`), // not a bridge puppet
		removedBeforeRead,
		event("$unreact", "m.room.redaction", "@discord_2:x", `{"redacts":"$laugh"}`),
	}
	for _, e := range events {
		if err := worker.logEvent("!general", e); err != nil {
			t.Fatalf("%s: %v", e.EventID, err)
		}
	}

	reactions, err := db.ReactionsTo(database, []string{"$message"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]db.MessageReactionGroup{"$message": {
		{Key: "mxc://x/this", Shortcode: ":this:", Count: 1, SenderDisplayNames: []string{"Jade"}},
	}}
	if !reflect.DeepEqual(reactions, want) {
		t.Errorf("reactions = %+v, want %+v", reactions, want)
	}
	messages, err := db.ListLoggedMessages(database, db.MessageLogFilter{Limit: 10})
	if err != nil || len(messages) != 1 || messages[0].RedactedAt != nil ||
		messages[0].Format != "org.matrix.custom.html" || messages[0].FormattedBody == "" {
		t.Fatalf("the message must be logged and not marked deleted by the reaction's removal: %+v %v", messages, err)
	}

	if err := worker.logEvent("!general", event("$delete", "m.room.redaction", "@discordbot:x", `{"redacts":"$message"}`)); err != nil {
		t.Fatal(err)
	}
	deleted, err := db.ListLoggedMessages(database, db.MessageLogFilter{DeletedOnly: true, Limit: 10})
	if err != nil || len(deleted) != 1 || deleted[0].Body != "hi" {
		t.Errorf("deleted = %+v %v", deleted, err)
	}
}
