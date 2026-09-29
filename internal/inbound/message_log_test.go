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
		if r.URL.Path == "/_matrix/client/v3/account/whoami" {
			w.Write([]byte(`{"user_id":"@me:x"}`)) // our own Matrix account
			return
		}
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
		event("$admin", "m.reaction", "@admin:x", `{"m.relates_to":{"event_id":"$message","key":"👍","rel_type":"m.annotation"}}`), // neither a bridge puppet nor us
		event("$mine", "m.reaction", "@me:x", `{"m.relates_to":{"event_id":"$message","key":"🔥","rel_type":"m.annotation"}}`),     // our own account: logged
		removedBeforeRead,
		event("$unreact", "m.room.redaction", "@discord_2:x", `{"redacts":"$laugh"}`),
		// Stickers as mautrix-discord sends them: an image one, and a Lottie one it could not convert.
		event("$wave", "m.sticker", "@discord_4:x", `{"body":"Wave","info":{"h":160,"mimetype":"image/apng","size":471047,"w":160},"url":"mxc://x/wave"}`),
		event("$sup", "m.sticker", "@discord_5:x", `{"body":"Sup","info":{"h":160,"mimetype":"application/json","w":160}}`),
		event("$adminsticker", "m.sticker", "@admin:x", `{"body":"Mine","url":"mxc://x/mine"}`),
	}
	for _, e := range events {
		if err := worker.logEvent("!general", e); err != nil {
			t.Fatalf("%s: %v", e.EventID, err)
		}
	}

	reactions, err := db.ReactionsTo(database, []string{"$message"}, db.OwnSenders{})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]db.MessageReactionGroup{"$message": {
		{Key: "🔥", Count: 1, SenderDisplayNames: []string{"Jade"}},
		{Key: "mxc://x/this", Shortcode: ":this:", Count: 1, SenderDisplayNames: []string{"Jade"}},
	}}
	if !reflect.DeepEqual(reactions, want) {
		t.Errorf("reactions = %+v, want %+v", reactions, want)
	}
	messages, err := db.ListLoggedMessages(database, db.MessageLogFilter{Limit: 10})
	if err != nil || len(messages) != 3 {
		t.Fatalf("want the message and two stickers: %+v %v", messages, err)
	}
	byEvent := map[string]db.LoggedMessage{}
	for _, m := range messages {
		byEvent[m.EventID] = m
	}
	if m := byEvent["$message"]; m.RedactedAt != nil || m.Format != "org.matrix.custom.html" || m.FormattedBody == "" {
		t.Fatalf("the message must be logged and not marked deleted by the reaction's removal: %+v", m)
	}
	if m := byEvent["$wave"]; m.MessageType != "m.sticker" || m.Body != "Wave" || m.MediaURL != "mxc://x/wave" || m.MediaMimetype != "image/apng" {
		t.Errorf("wave sticker = %+v", m)
	}
	if m := byEvent["$sup"]; m.MessageType != "m.sticker" || m.MediaURL != "" || m.MediaMimetype != "application/json" {
		t.Errorf("lottie sticker = %+v", m)
	}

	if err := worker.logEvent("!general", event("$delete", "m.room.redaction", "@discordbot:x", `{"redacts":"$message"}`)); err != nil {
		t.Fatal(err)
	}
	deleted, err := db.ListLoggedMessages(database, db.MessageLogFilter{DeletedOnly: true, Limit: 10})
	if err != nil || len(deleted) != 1 || deleted[0].Body != "hi" {
		t.Errorf("deleted = %+v %v", deleted, err)
	}
}
