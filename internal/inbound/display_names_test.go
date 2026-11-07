package inbound

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/matrix"
)

func TestTheLogWriterAsksForEachSendersNameOnceAndNothingForAMessageAlreadyLogged(t *testing.T) {
	profileLookups := 0
	synapse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/profile/") {
			profileLookups++
		}
		w.Write([]byte(`{"displayname":"Kat"}`))
	}))
	defer synapse.Close()
	database, err := db.Initialize(filepath.Join(t.TempDir(), "multichat.db"))
	if err != nil {
		t.Fatal(err)
	}
	worker := &SyncWorker{
		Matrix:   matrix.NewClient(synapse.URL, "token"),
		Database: database,
		rooms:    map[string]roomFacts{"!kat": {Name: "Kat", IsDirectMessage: true}},
		bridges:  map[string]matrix.BridgeInfo{"!kat": {}},
	}
	message := func(id string) matrix.TimelineEvent {
		return matrix.TimelineEvent{EventID: id, Type: "m.room.message", Sender: "@gmessagesweb_1:x", OriginServerTS: 1790616706478,
			Content: json.RawMessage(`{"body":"hi","msgtype":"m.text"}`)}
	}
	for _, id := range []string{"$one", "$two", "$three"} {
		if err := worker.logEvent("!kat", message(id)); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	if profileLookups != 1 {
		t.Fatalf("three messages from one sender asked for the name %d times, want once", profileLookups)
	}

	// A batch read again after a failure, by a worker that remembers nothing.
	worker.displayNames = nil
	if err := worker.logEvent("!kat", message("$two")); err != nil {
		t.Fatal(err)
	}
	if profileLookups != 1 {
		t.Fatalf("a message already logged asked for the name again (%d lookups)", profileLookups)
	}
	messages, err := db.ListLoggedMessages(database, db.MessageLogFilter{Limit: 10})
	if err != nil || len(messages) != 3 || messages[0].SenderDisplayName != "Kat" {
		t.Fatalf("want three messages from Kat: %+v %v", messages, err)
	}
}
