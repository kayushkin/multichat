package inbound

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/matrix"
)

func TestBackfillReactionsLogsWhatCameSinceTheLogBeganOnce(t *testing.T) {
	since := time.UnixMilli(1_000_000)
	// Newest first, as /messages?dir=b answers; split over two pages.
	pages := map[string]string{
		"": `{"end":"page2","chunk":[
			{"event_id":"$unlaugh","type":"m.room.redaction","sender":"@discord_2:x","origin_server_ts":1000400,"content":{"redacts":"$laugh"}},
			{"event_id":"$message","type":"m.room.message","sender":"@discord_1:x","origin_server_ts":1000300,"content":{"body":"left to the live log"}},
			{"event_id":"$this","type":"m.reaction","sender":"@discord_3:x","origin_server_ts":1000200,"content":{"com.beeper.reaction.shortcode":":this:","m.relates_to":{"event_id":"$target","key":"mxc://x/this","rel_type":"m.annotation"}}}]}`,
		"page2": `{"end":"page3","chunk":[
			{"event_id":"$laugh","type":"m.reaction","sender":"@discord_2:x","origin_server_ts":1000100,"content":{"m.relates_to":{"event_id":"$target","key":"🤣","rel_type":"m.annotation"}}},
			{"event_id":"$old","type":"m.reaction","sender":"@discord_2:x","origin_server_ts":999000,"content":{"m.relates_to":{"event_id":"$target","key":"👍","rel_type":"m.annotation"}}}]}`,
		"page3": `{"chunk":[]}`,
	}
	synapse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/_matrix/client/v3/joined_rooms":
			w.Write([]byte(`{"joined_rooms":["!general"]}`))
		case strings.HasSuffix(r.URL.Path, "/messages"):
			page, ok := pages[r.URL.Query().Get("from")]
			if !ok {
				t.Errorf("paged to %q", r.URL.Query().Get("from"))
			}
			if r.URL.Query().Get("from") == "page3" {
				t.Error("paged past an event older than since")
			}
			w.Write([]byte(page))
		default:
			w.Write([]byte(`{"displayname":"Jade"}`))
		}
	}))
	defer synapse.Close()
	database, err := db.Initialize(filepath.Join(t.TempDir(), "multichat.db"))
	if err != nil {
		t.Fatal(err)
	}
	worker := &SyncWorker{Matrix: matrix.NewClient(synapse.URL, "token"), Database: database}

	first, err := worker.BackfillReactions(since)
	if err != nil {
		t.Fatal(err)
	}
	if first != (ReactionBackfillResult{RoomsRead: 1, ReactionsSeen: 2, RedactionsSeen: 1, ReactionsLogged: 2, ReactionsMarkedOff: 1}) {
		t.Errorf("first run = %+v", first)
	}
	second, err := worker.BackfillReactions(since)
	if err != nil {
		t.Fatal(err)
	}
	if second.ReactionsLogged != 0 || second.ReactionsMarkedOff != 0 || second.ReactionsSeen != 2 {
		t.Errorf("second run = %+v, want nothing changed", second)
	}
	reactions, err := db.ReactionsTo(database, []string{"$target"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]db.MessageReactionGroup{"$target": {
		{Key: "mxc://x/this", Shortcode: ":this:", Count: 1, SenderDisplayNames: []string{"Jade"}},
	}}
	if !reflect.DeepEqual(reactions, want) {
		t.Errorf("reactions = %+v", reactions)
	}
	if messages, _ := db.ListLoggedMessages(database, db.MessageLogFilter{Limit: 10}); len(messages) != 0 {
		t.Errorf("the backfill logged messages: %+v", messages)
	}
}
