package api

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kayushkin/multichat/internal/db"
)

func TestAutoReactionRoutesCheckEveryField(t *testing.T) {
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
		{"POST", "/auto-reactions", `{"sender_user_id":"@discord_1:chat.example","room_id":"","reaction_key":"🥛"}`, 201, `"enabled":true`},
		{"POST", "/auto-reactions", `{"sender_user_id":"@discord_1:chat.example","room_id":"","reaction_key":"🥛"}`, 409, "already gets"},
		{"POST", "/auto-reactions", `{"sender_user_id":"@gmessages_1.719:chat.example","room_id":"!group:chat.example","reaction_key":"🥛","enabled":false}`, 201, `"enabled":false`},
		{"POST", "/auto-reactions", `{"sender_user_id":"Lukas","room_id":"","reaction_key":"🥛"}`, 400, "Matrix user id"},
		{"POST", "/auto-reactions", `{"sender_user_id":"@discord_1:chat.example","room_id":"general","reaction_key":"🥛"}`, 400, "Matrix room id"},
		{"POST", "/auto-reactions", `{"sender_user_id":"@discord_1:chat.example","room_id":"","reaction_key":""}`, 400, "required"},
		{"POST", "/auto-reactions", `{"sender_user_id":"@discord_1:chat.example","room_id":"","reaction_key":"discord-emoji:1"}`, 400, "not supported"},
		{"POST", "/auto-reactions", `{"sender_user_id":"@discord_1:chat.example","reaction_key":"🥛","colour":"blue"}`, 400, "unknown field"},
		{"PATCH", "/auto-reactions/1", `{"enabled":false}`, 200, `"enabled":false`},
		{"PATCH", "/auto-reactions/1", `{"reaction_key":"two words"}`, 400, "not an emoji"},
		{"PATCH", "/auto-reactions/9", `{"enabled":true}`, 404, "no such auto-reaction"},
		{"GET", "/auto-reactions", ``, 200, `"sender_user_id":"@gmessages_1.719:chat.example"`},
		{"DELETE", "/auto-reactions/1", ``, 204, ``},
		{"DELETE", "/auto-reactions/1", ``, 404, "no such auto-reaction"},
	}
	for _, c := range cases {
		rec := call(c.method, c.path, c.body)
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.answer) {
			t.Errorf("%s %s %s: %d %s, want %d containing %q", c.method, c.path, c.body, rec.Code, rec.Body, c.code, c.answer)
		}
	}
}
