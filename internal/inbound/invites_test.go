package inbound

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/kayushkin/multichat/internal/matrix"
)

func TestTheWorkerAcceptsBridgeInvitesWaitsOutTheRateLimitAndLeavesOthersAlone(t *testing.T) {
	var joins []string
	rateLimitedOnce := false
	synapse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/_matrix/client/v3/account/whoami":
			w.Write([]byte(`{"user_id":"@admin:x"}`))
		case r.URL.Path == "/_matrix/client/v3/sync":
			member := func(sender string) string {
				return `{"invite_state":{"events":[{"type":"m.room.name","state_key":"","sender":"` + sender + `","content":{"name":"n"}},` +
					`{"type":"m.room.member","state_key":"@admin:x","sender":"` + sender + `","content":{"membership":"invite"}}]}}`
			}
			w.Write([]byte(`{"next_batch":"s1","rooms":{"invite":{` +
				`"!group:x":` + member("@whatsappbot:x") + `,` +
				`"!dm:x":` + member("@discord_1:x") + `,` +
				`"!stranger:x":` + member("@someone:x") + `}}}`))
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/join"):
			if !rateLimitedOnce {
				rateLimitedOnce = true
				w.WriteHeader(429)
				w.Write([]byte(`{"errcode":"M_LIMIT_EXCEEDED","retry_after_ms":1}`))
				return
			}
			joins = append(joins, strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/_matrix/client/v3/rooms/"), "/join"))
			w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer synapse.Close()
	worker := &SyncWorker{Matrix: matrix.NewClient(synapse.URL, "token"), joinedAt: map[string]time.Time{}}

	if err := worker.acceptPendingInvites(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(joins)
	if want := []string{"!dm:x", "!group:x"}; !reflect.DeepEqual(joins, want) {
		t.Fatalf("joined %v, want %v (bridge invites only, the rate-limited one retried)", joins, want)
	}

	joinedAt := worker.joinedAt["!group:x"]
	before := matrix.TimelineEvent{OriginServerTS: joinedAt.Add(-time.Minute).UnixMilli()}
	after := matrix.TimelineEvent{OriginServerTS: joinedAt.Add(time.Minute).UnixMilli()}
	if !worker.sentBeforeWeJoined("!group:x", before) || worker.sentBeforeWeJoined("!group:x", after) {
		t.Fatal("history from before the join must be held back from the bus, and only that")
	}
	if worker.sentBeforeWeJoined("!elsewhere:x", before) {
		t.Fatal("a room joined before this process started publishes normally")
	}
}
