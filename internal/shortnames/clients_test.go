package shortnames

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/kayushkin/llm-bridge/msg"
	"github.com/kayushkin/multichat/internal/matrix"
)

// A fake of the short-name-store contract: pages of names, a 404 namespace,
// and a 409 on a taken path.
func TestTheStoreClientReadsEveryPageAndReportsAConflict(t *testing.T) {
	total := storePageSize + 3
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/namespaces/messages":
			json.NewEncoder(w).Encode(Namespace{Name: "messages", ReservedSegments: []string{"search"}})
		case r.Method == http.MethodGet && r.URL.Path == "/namespaces/nowhere":
			w.WriteHeader(404)
			json.NewEncoder(w).Encode(map[string]string{"error": "no namespace nowhere"})
		case r.Method == http.MethodGet && r.URL.Path == "/short-names":
			query := r.URL.Query()
			if query.Get("namespace") != "messages" || query.Get("include_superseded") != "true" || query.Get("limit") != strconv.Itoa(storePageSize) {
				t.Errorf("query %s", r.URL.RawQuery)
			}
			offset, _ := strconv.Atoi(query.Get("offset"))
			page := []ShortName{}
			for i := offset; i < min(offset+storePageSize, total); i++ {
				page = append(page, ShortName{ID: fmt.Sprintf("short_name_%06d", i+1), Path: fmt.Sprintf("p%d", i)})
			}
			json.NewEncoder(w).Encode(page)
		case r.Method == http.MethodPost && r.URL.Path == "/short-names":
			var body NewShortName
			decoder := json.NewDecoder(r.Body)
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&body); err != nil {
				w.WriteHeader(400)
				return
			}
			if body.Path == "kim" {
				w.WriteHeader(409)
				json.NewEncoder(w).Encode(map[string]string{"error": "path kim belongs to another record"})
				return
			}
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(ShortName{ID: "short_name_000009", Path: body.Path, EntityID: body.EntityID})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	client := StoreClient{BaseURL: server.URL, HTTPClient: server.Client()}
	ctx := context.Background()

	namespace, err := client.Namespace(ctx, "messages")
	if err != nil || namespace.ReservedSegments[0] != "search" {
		t.Fatalf("%+v %v", namespace, err)
	}
	if _, err := client.Namespace(ctx, "nowhere"); !errors.Is(err, ErrNamespaceMissing) {
		t.Errorf("missing namespace: %v", err)
	}
	names, err := client.AllShortNames(ctx, "messages")
	if err != nil || len(names) != total {
		t.Fatalf("%d names, %v", len(names), err)
	}
	written, err := client.CreateShortName(ctx, NewShortName{Namespace: "messages", Path: "maria", EntityType: EntityTypeMultichatRoom, EntityID: "!m:x", CreatedBy: CreatedBy, Reason: "r"})
	if err != nil || written.ID != "short_name_000009" {
		t.Fatalf("%+v %v", written, err)
	}
	_, err = client.CreateShortName(ctx, NewShortName{Namespace: "messages", Path: "kim", EntityType: EntityTypeMultichatRoom, EntityID: "!k:x", CreatedBy: CreatedBy, Reason: "r"})
	if !errors.Is(err, ErrPathTaken) {
		t.Errorf("409: %v", err)
	}
}

func TestTheOneShotModelSendsTheRoleAndTheTokenAndRefusesATruncatedReply(t *testing.T) {
	stopReason := "tool_use"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oneshot" || r.Header.Get("X-Test-Token") != "secret" {
			t.Errorf("%s with token %q", r.URL.Path, r.Header.Get("X-Test-Token"))
		}
		var request msg.OneShotRequest
		json.NewDecoder(r.Body).Decode(&request)
		if request.ModelRole != "balanced" || request.Caller != CreatedBy || len(request.Schema) == 0 || request.Model != "" {
			t.Errorf("request %+v", request)
		}
		json.NewEncoder(w).Encode(msg.OneShotResponse{Model: "gpt-test", StopReason: stopReason,
			Parsed: json.RawMessage(`{"names":[{"entity_id":"!a:x","path":"kim","reason":"first name"}]}`)})
	}))
	defer server.Close()
	model := OneShotModel{BridgeURL: server.URL, ServiceTokenHeader: "X-Test-Token", ServiceToken: "secret", ModelRole: "balanced", MaxTokens: 100, HTTPClient: server.Client()}
	var reply roomNamesReply
	answeredBy, err := model.Propose(context.Background(), "system", "prompt", roomNamesSchema, &reply)
	if err != nil || answeredBy != "gpt-test" || reply.Names[0].Path != "kim" {
		t.Fatalf("%s %+v %v", answeredBy, reply, err)
	}
	stopReason = "max_tokens"
	if _, err := model.Propose(context.Background(), "system", "prompt", roomNamesSchema, &reply); !errors.Is(err, ErrTruncated) {
		t.Errorf("truncated: %v", err)
	}
}

func TestRoomFactsUsePeopleStoreNamesAndTellTheKindsApart(t *testing.T) {
	conversations := []matrix.Conversation{
		{RoomID: "!dm:x", Name: "kim_l", Platform: "discord", DirectMessagePartnerUserID: "@discord_1:x",
			Members: []matrix.ConversationMember{{UserID: "@discord_1:x", DisplayName: "kim_l"}}},
		{RoomID: "!tg:x", Name: "Bob", Platform: "telegram", Members: []matrix.ConversationMember{{UserID: "@telegram_2:x", DisplayName: "Bob"}}},
		{RoomID: "!group:x", Name: "NYC", Platform: "gmessages", MemberCount: 5,
			Members: []matrix.ConversationMember{{UserID: "@g_1:x", DisplayName: "A"}, {UserID: "@g_2:x", DisplayName: "B"}}},
		{RoomID: "!chan:x", Name: "#general", Platform: "discord", Members: []matrix.ConversationMember{{UserID: "@discord_3:x"}}},
	}
	rooms := RoomsFromConversations(conversations, map[string]string{"@discord_1:x": "Kim Park"},
		map[string]DiscordChannel{"!chan:x": {ServerID: "s", ServerName: "Pretend", ChannelName: "general"}})
	want := []struct{ kind, partner string }{{KindDirectMessage, "Kim Park"}, {KindDirectMessage, "Bob"}, {KindGroup, ""}, {KindDiscordChannel, ""}}
	for i, room := range rooms {
		if room.Kind != want[i].kind || room.DirectMessagePartnerName != want[i].partner {
			t.Errorf("room %s: %s %q, want %s %q", room.RoomID, room.Kind, room.DirectMessagePartnerName, want[i].kind, want[i].partner)
		}
	}
	if rooms[3].Discord == nil || rooms[3].Discord.ServerName != "Pretend" {
		t.Errorf("channel facts %+v", rooms[3])
	}
}
