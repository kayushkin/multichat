package matrix

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSynapse serves two Signal rooms. Its first incremental /sync reports a
// new message in roomA and that we left roomB; later ones report nothing.
type fakeSynapse struct {
	mutex       sync.Mutex
	lastMessage map[string]string
	syncs       int
}

const (
	cacheTestRoomA = "!a:test"
	cacheTestRoomB = "!b:test"
)

func (f *fakeSynapse) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	path := r.URL.Path
	switch {
	case path == "/_matrix/client/v3/sync" && r.URL.Query().Get("since") == "":
		fmt.Fprint(w, `{"next_batch":"s1"}`)
	case path == "/_matrix/client/v3/sync":
		f.syncs++
		if f.syncs > 1 {
			time.Sleep(50 * time.Millisecond)
			fmt.Fprint(w, `{"next_batch":"s3"}`)
			return
		}
		f.lastMessage[cacheTestRoomA] = "second"
		fmt.Fprintf(w, `{"next_batch":"s2","rooms":{"join":{%q:{"timeline":{"events":[{"type":"m.room.message"}]}}},"leave":{%q:{}}}}`, cacheTestRoomA, cacheTestRoomB)
	case path == "/_matrix/client/v3/joined_rooms":
		fmt.Fprintf(w, `{"joined_rooms":[%q,%q]}`, cacheTestRoomA, cacheTestRoomB)
	case strings.HasSuffix(path, "/state/m.room.name"):
		http.Error(w, `{"errcode":"M_NOT_FOUND"}`, 404)
	case strings.HasSuffix(path, "/members"):
		fmt.Fprint(w, `{"chunk":[{"state_key":"@signal_1:test","content":{"displayname":"Ada"}},{"state_key":"@signal_2:test","content":{"displayname":"Bo"}}]}`)
	case strings.HasSuffix(path, "/messages"):
		roomID := strings.TrimSuffix(strings.TrimPrefix(path, "/_matrix/client/v3/rooms/"), "/messages")
		fmt.Fprintf(w, `{"chunk":[{"origin_server_ts":%d,"content":{"msgtype":"m.text","body":%q}}]}`, f.syncs+1, f.lastMessage[roomID])
	default:
		http.Error(w, "unexpected "+path, 500)
	}
}

func TestListConversationsKeepsTheListCurrentFromSync(t *testing.T) {
	synapse := &fakeSynapse{lastMessage: map[string]string{cacheTestRoomA: "first", cacheTestRoomB: "hello"}}
	server := httptest.NewServer(synapse)
	defer server.Close()
	client := NewClient(server.URL, "token")

	first, err := client.ListConversations(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 {
		t.Fatalf("first list has %d rooms, want 2: %+v", len(first), first)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		convos, err := client.ListConversations(nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(convos) == 1 && convos[0].RoomID == cacheTestRoomA && convos[0].LastMessage == "second" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("after the sync the list is %+v, want only %s with its new message", convos, cacheTestRoomA)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
