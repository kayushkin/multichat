package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/matrix"
)

// A reaction posted through multichat is sent as our Matrix account, shows as
// ours at once, and can be taken back; someone else's cannot.
func TestPostAndTakeBackAReaction(t *testing.T) {
	var sentReaction, redacted string
	synapse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/_matrix/client/v3/account/whoami":
			w.Write([]byte(`{"user_id":"@admin:chat.example"}`))
		case strings.HasPrefix(r.URL.Path, "/_matrix/client/v3/profile/"):
			w.Write([]byte(`{"displayname":"Admin"}`))
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/send/m.reaction/"):
			var body map[string]map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			sentReaction = body["m.relates_to"]["event_id"] + " " + body["m.relates_to"]["key"] + " " + body["m.relates_to"]["rel_type"]
			w.Write([]byte(`{"event_id":"$my-reaction"}`))
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/redact/"):
			redacted = r.URL.Path
			w.Write([]byte(`{"event_id":"$redaction"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer synapse.Close()
	router := routerForSettingsTests(t)
	database, err := db.Initialize(filepath.Join(t.TempDir(), "multichat.db"))
	if err != nil {
		t.Fatal(err)
	}
	router.db = database
	router.matrix = matrix.NewClient(synapse.URL, secretValues["MATRIX_TOKEN"])
	room := "!room:chat.example"
	base := "/conversations/" + url.PathEscape(room) + "/reactions"
	call := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+secretValues["MULTICHAT_TOKEN"])
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	if rec := call(http.MethodPost, base, `{"event_id":"$message"}`); rec.Code != 400 || !strings.Contains(rec.Body.String(), "key") {
		t.Fatalf("missing key: %d %s", rec.Code, rec.Body)
	}
	rec := call(http.MethodPost, base, `{"event_id":"$message","key":"🔥"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "$my-reaction") {
		t.Fatalf("post: %d %s", rec.Code, rec.Body)
	}
	if sentReaction != "$message 🔥 m.annotation" {
		t.Fatalf("Synapse got %q", sentReaction)
	}
	own := db.OwnSenders{MatrixUserID: "@admin:chat.example", PuppetUserIDs: map[string]bool{}}
	groups, err := db.ReactionsTo(router.db, []string{"$message"}, own)
	if err != nil {
		t.Fatal(err)
	}
	if got := groups["$message"]; len(got) != 1 || !got[0].ReactedByMe || got[0].MyReactionEventID != "$my-reaction" {
		t.Fatalf("groups = %+v", groups)
	}

	if err := db.LogReaction(router.db, db.LoggedReaction{EventID: "$theirs", TargetEventID: "$message", RoomID: room,
		Platform: "discord", SenderUserID: "@discord_2:chat.example", SenderDisplayName: "Kit", Key: "🔥",
		ContentJSON: `{}`, SentAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if rec := call(http.MethodDelete, base+"/"+url.PathEscape("$theirs"), ""); rec.Code != 403 {
		t.Fatalf("someone else's reaction: %d %s", rec.Code, rec.Body)
	}
	if rec := call(http.MethodDelete, base+"/"+url.PathEscape("$my-reaction"), ""); rec.Code != 200 {
		t.Fatalf("take back: %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(redacted, "/redact/") || !strings.Contains(redacted, "$my-reaction") {
		t.Fatalf("redacted %q", redacted)
	}
	groups, err = db.ReactionsTo(router.db, []string{"$message"}, own)
	if err != nil {
		t.Fatal(err)
	}
	if got := groups["$message"]; len(got) != 1 || got[0].Count != 1 || got[0].ReactedByMe {
		t.Fatalf("after taking back: %+v", groups)
	}
	if rec := call(http.MethodDelete, base+"/"+url.PathEscape("$my-reaction"), ""); rec.Code != 409 {
		t.Fatalf("taking back twice: %d %s", rec.Code, rec.Body)
	}
}
