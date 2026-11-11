package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeMessageStore holds one bot account, one conversation it is in and one
// it is not, and keeps what an import files.
type fakeMessageStore struct {
	mu      sync.Mutex
	filed   []map[string]any
	queries []string
}

func (f *fakeMessageStore) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == "GET" && r.URL.Path == "/accounts/message_account_000002":
			io.WriteString(w, `{"id":"message_account_000002","platform":"discord","handle":"bot-handle","display_name":"Helper Bot","kind":"bot"}`)
		case r.Method == "GET" && r.URL.Path == "/conversations/conversation_000001":
			io.WriteString(w, `{"id":"conversation_000001","platform":"discord","source":"discord","source_ref":"channel-1","is_group":false,"participants":[{"handle":"bot-handle"},{"handle":"person-handle"}]}`)
		case r.Method == "GET" && r.URL.Path == "/conversations/conversation_000002":
			io.WriteString(w, `{"id":"conversation_000002","platform":"discord","source":"discord","source_ref":"channel-2","is_group":true,"participants":[{"handle":"someone-else"}]}`)
		case r.Method == "GET" && r.URL.Path == "/conversations":
			f.queries = append(f.queries, r.URL.RawQuery)
			io.WriteString(w, `[{"id":"conversation_000001"}]`)
		case r.Method == "POST" && r.URL.Path == "/imports":
			io.WriteString(w, `{"id":"import_000001"}`)
		case r.Method == "POST" && r.URL.Path == "/imports/import_000001/batches":
			var batch map[string]any
			if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
				t.Errorf("batch does not parse: %v", err)
			}
			f.filed = append(f.filed, batch)
			io.WriteString(w, `{}`)
		case r.Method == "POST" && r.URL.Path == "/imports/import_000001/finish":
			io.WriteString(w, `{}`)
		case r.Method == "GET" && r.URL.Path == "/messages" && r.URL.Query().Get("source_ref") == "m1":
			io.WriteString(w, `{"messages":[{"id":"chat_message_000009","source_ref":"m1","body":"hello"}]}`)
		default:
			w.WriteHeader(404)
			io.WriteString(w, `{"error":"no such record"}`)
		}
	})
}

func routerWithMessageStore(t *testing.T) (*Router, *fakeMessageStore, *fakeBots) {
	t.Helper()
	store := &fakeMessageStore{}
	server := httptest.NewServer(store.handler(t))
	t.Cleanup(server.Close)
	router := routerWithoutAuth(t)
	router.messageStoreURL = server.URL
	bots := &fakeBots{}
	router.SetBotAccounts(bots)
	return router, store, bots
}

func TestABotSendsIntoItsConversationAndTheMessageIsFiledAtOnce(t *testing.T) {
	router, store, bots := routerWithMessageStore(t)
	got := post(router, "/message-store/accounts/message_account_000002/conversations/conversation_000001/messages", `{"body":"hello"}`)
	if got.Code != 201 || !strings.Contains(got.Body.String(), `"id":"chat_message_000009"`) {
		t.Fatalf("send: %d %s", got.Code, got.Body)
	}
	if len(bots.sends) != 1 || bots.sends[0] != "message_account_000002||channel-1|hello" {
		t.Fatalf("sent %v, want one send to channel-1", bots.sends)
	}
	if len(store.filed) != 1 {
		t.Fatalf("filed %d batches, want 1", len(store.filed))
	}
	message := store.filed[0]["messages"].([]any)[0].(map[string]any)
	if message["source_ref"] != "m1" || message["body"] != "hello" || message["sender_handle"] != "bot-handle" || message["body_source"] != "platform" {
		t.Fatalf("filed message %v", message)
	}
}

func TestABotCannotSendIntoAConversationItIsNotIn(t *testing.T) {
	router, _, bots := routerWithMessageStore(t)
	got := post(router, "/message-store/accounts/message_account_000002/conversations/conversation_000002/messages", `{"body":"hello"}`)
	if got.Code != 403 || len(bots.sends) != 0 {
		t.Fatalf("send outside the account's conversations: %d %s, sends %v", got.Code, got.Body, bots.sends)
	}
}

func TestAnEmptyOrTooLongMessageIsRefusedBeforeAnythingIsSent(t *testing.T) {
	router, _, bots := routerWithMessageStore(t)
	for _, body := range []string{`{"body":"  "}`, `{"body":"` + strings.Repeat("a", 2001) + `"}`, `{"body":"hi","extra":1}`} {
		if got := post(router, "/message-store/accounts/message_account_000002/conversations/conversation_000001/messages", body); got.Code != 400 {
			t.Fatalf("body %.40s: %d %s", body, got.Code, got.Body)
		}
	}
	if len(bots.sends) != 0 {
		t.Fatalf("sent %v", bots.sends)
	}
}

func TestAnUnknownAccountOrConversationIsNotFound(t *testing.T) {
	router, _, bots := routerWithMessageStore(t)
	for _, path := range []string{
		"/message-store/accounts/message_account_000099/conversations/conversation_000001/messages",
		"/message-store/accounts/message_account_000002/conversations/conversation_000099/messages",
	} {
		if got := post(router, path, `{"body":"hello"}`); got.Code != 404 {
			t.Fatalf("%s: %d %s", path, got.Code, got.Body)
		}
	}
	if len(bots.sends) != 0 {
		t.Fatalf("sent %v", bots.sends)
	}
}

func TestReadsPassThroughUnchangedAndOtherRoutesAreRefused(t *testing.T) {
	router, store, _ := routerWithMessageStore(t)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/message-store/conversations?account=message_account_000002&limit=500", nil))
	if recorder.Code != 200 || recorder.Body.String() != `[{"id":"conversation_000001"}]` || store.queries[0] != "account=message_account_000002&limit=500" {
		t.Fatalf("read: %d %s %v", recorder.Code, recorder.Body, store.queries)
	}
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/message-store/conversations/conversation_000099", nil))
	if recorder.Code != 404 || !strings.Contains(recorder.Body.String(), "no such record") {
		t.Fatalf("message-store's 404 should pass through: %d %s", recorder.Code, recorder.Body)
	}
	for _, request := range []*http.Request{
		httptest.NewRequest("POST", "/message-store/imports", strings.NewReader(`{}`)),
		httptest.NewRequest("GET", "/message-store/imports", nil),
		httptest.NewRequest("PATCH", "/message-store/conversations/conversation_000001/participants/x", strings.NewReader(`{}`)),
	} {
		recorder = httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != 404 || strings.Contains(recorder.Body.String(), "no such record") {
			t.Fatalf("%s %s should be refused by multichat: %d %s", request.Method, request.URL.Path, recorder.Code, recorder.Body)
		}
	}
}

func TestWithoutMessageStoreTheRoutesSayWhy(t *testing.T) {
	router := routerWithoutAuth(t)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/message-store/accounts", nil))
	if recorder.Code != 503 || !strings.Contains(recorder.Body.String(), "MESSAGE_STORE_URL") {
		t.Fatalf("%d %s", recorder.Code, recorder.Body)
	}
}

// Discord lists no bot's DMs, so a send to a person by user id files the DM
// channel it opened, with the bot and the person in it, and the message.
func TestASendToAPersonFilesTheDirectMessageAtOnce(t *testing.T) {
	router, store, _ := routerWithMessageStore(t)
	got := post(router, "/messages/send", `{"account_id":"message_account_000002","user_id":"person-handle","body":"hello"}`)
	if got.Code != 200 {
		t.Fatalf("send: %d %s", got.Code, got.Body)
	}
	if len(store.filed) != 1 {
		t.Fatalf("filed %d batches, want 1", len(store.filed))
	}
	conversation := store.filed[0]["conversations"].([]any)[0].(map[string]any)
	participants := conversation["participants"].([]any)
	if conversation["source_ref"] != "dm-1" || conversation["is_group"] != false || len(participants) != 2 ||
		participants[0].(map[string]any)["handle"] != "bot-handle" || participants[1].(map[string]any)["handle"] != "person-handle" {
		t.Fatalf("filed conversation %v", conversation)
	}
	message := store.filed[0]["messages"].([]any)[0].(map[string]any)
	if message["conversation_source_ref"] != "dm-1" || message["source_ref"] != "m1" || message["body"] != "hello" || message["sender_handle"] != "bot-handle" {
		t.Fatalf("filed message %v", message)
	}
	// A send to a channel is the bot importer's to read.
	if got := post(router, "/messages/send", `{"account_id":"message_account_000002","channel_id":"55","body":"hi"}`); got.Code != 200 || len(store.filed) != 1 {
		t.Fatalf("channel send: %d, filed %d batches", got.Code, len(store.filed))
	}
}

// When message-store refuses the filing the message is already on Discord,
// so the answer says what was sent along with the failure.
func TestADirectMessageMessageStoreRefusesSaysWhatWasSent(t *testing.T) {
	router, _, _ := routerWithMessageStore(t)
	got := post(router, "/messages/send", `{"account_id":"message_account_000404","user_id":"person-handle","body":"hello"}`)
	if got.Code != 502 || !strings.Contains(got.Body.String(), `"message_ids":["m1"]`) || !strings.Contains(got.Body.String(), "message-store did not take") {
		t.Fatalf("refused filing: %d %s", got.Code, got.Body)
	}
}
