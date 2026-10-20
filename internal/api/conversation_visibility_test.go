package api

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/matrix"
)

func TestConversationVisibilityChoicesAndAutoHideRule(t *testing.T) {
	router := routerForSettingsTests(t)
	database, err := db.Initialize(filepath.Join(t.TempDir(), "multichat.db"))
	if err != nil {
		t.Fatal(err)
	}
	router.db = database
	call := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+secretValues["MULTICHAT_TOKEN"])
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	cases := []struct {
		method, path, body string
		code               int
		answer             string
	}{
		{"GET", "/conversations/visibility-settings", ``, 200, `{"auto_hide_enabled":true,"auto_hide_member_count_threshold":30}`},
		{"PUT", "/conversations/visibility-settings", `{"auto_hide_enabled":true}`, 400, "both required"},
		{"PUT", "/conversations/visibility-settings", `{"auto_hide_enabled":true,"auto_hide_member_count_threshold":0}`, 400, "at least 1"},
		{"PUT", "/conversations/visibility-settings", `{"auto_hide_enabled":true,"auto_hide_member_count_threshold":5,"x":1}`, 400, "unknown field"},
		{"PUT", "/conversations/visibility-settings", `{"auto_hide_enabled":true,"auto_hide_member_count_threshold":10}`, 200, `"auto_hide_member_count_threshold":10`},
		{"PUT", "/conversations/!big:chat.example/visibility", `{"visibility":"shown"}`, 200, `"visibility":"shown"`},
		{"PUT", "/conversations/!small:chat.example/visibility", `{"visibility":"hidden"}`, 200, `"visibility":"hidden"`},
		{"PUT", "/conversations/!small:chat.example/visibility", `{"visibility":"gone"}`, 400, "visibility must be"},
		{"PUT", "/conversations/small/visibility", `{"visibility":"hidden"}`, 400, "Matrix room id"},
		{"GET", "/conversations/!small:chat.example/visibility", ``, 405, "Method not allowed"},
	}
	for _, c := range cases {
		rec := call(c.method, c.path, c.body)
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.answer) {
			t.Errorf("%s %s %s: %d %s", c.method, c.path, c.body, rec.Code, rec.Body)
		}
	}

	conversations := []matrix.Conversation{
		{RoomID: "!big:chat.example", MemberCount: 50},
		{RoomID: "!small:chat.example", MemberCount: 3},
		{RoomID: "!large:chat.example", MemberCount: 11},
		{RoomID: "!edge:chat.example", MemberCount: 10},
	}
	if err := router.markHiddenConversations(conversations); err != nil {
		t.Fatal(err)
	}
	want := []struct {
		visibility string
		hidden     bool
	}{{"shown", false}, {"hidden", true}, {"auto", true}, {"auto", false}}
	for index, conversation := range conversations {
		if conversation.Visibility != want[index].visibility || conversation.Hidden != want[index].hidden {
			t.Errorf("%s: visibility %q hidden %v, want %+v", conversation.RoomID, conversation.Visibility, conversation.Hidden, want[index])
		}
	}

	if rec := call("PUT", "/conversations/visibility-settings", `{"auto_hide_enabled":false,"auto_hide_member_count_threshold":10}`); rec.Code != 200 {
		t.Fatalf("turn auto-hide off: %d %s", rec.Code, rec.Body)
	}
	if rec := call("PUT", "/conversations/!small:chat.example/visibility", `{"visibility":"auto"}`); rec.Code != 200 {
		t.Fatalf("back to auto: %d %s", rec.Code, rec.Body)
	}
	if err := router.markHiddenConversations(conversations); err != nil {
		t.Fatal(err)
	}
	if conversations[1].Visibility != "auto" || conversations[1].Hidden || conversations[2].Hidden {
		t.Errorf("auto-hide off: %+v", conversations)
	}
}
