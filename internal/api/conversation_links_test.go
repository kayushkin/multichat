package api

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kayushkin/multichat/internal/db"
)

func TestConversationLinksLinkJoinRefuseAndUnlink(t *testing.T) {
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
		{"GET", "/conversations/links", ``, 200, `[]`},
		{"POST", "/conversations/links", `{"room_ids":["!texts:chat.example","!discord:chat.example"]}`, 200, `{"id":1,"room_ids":["!discord:chat.example","!texts:chat.example"]}`},
		// A third room joins the link one of its rooms is already in.
		{"POST", "/conversations/links", `{"room_ids":["!texts:chat.example","!whatsapp:chat.example"]}`, 200, `{"id":1,"room_ids":["!discord:chat.example","!texts:chat.example","!whatsapp:chat.example"]}`},
		{"POST", "/conversations/links", `{"room_ids":["!a:chat.example","!b:chat.example"]}`, 200, `{"id":2,`},
		{"POST", "/conversations/links", `{"room_ids":["!a:chat.example","!texts:chat.example"]}`, 409, "different links"},
		{"POST", "/conversations/links", `{"room_ids":["!a:chat.example","!a:chat.example"]}`, 400, "at least two"},
		{"POST", "/conversations/links", `{"room_ids":["!a:chat.example","b"]}`, 400, "Matrix room id"},
		{"POST", "/conversations/links", `{"room_ids":["!a:chat.example","!c:chat.example"],"x":1}`, 400, "unknown field"},
		{"POST", "/conversations/links/remove", `{"room_id":"!whatsapp:chat.example"}`, 200, `{"id":1,"room_ids":["!discord:chat.example","!texts:chat.example"]}`},
		// A link left with one room goes.
		{"POST", "/conversations/links/remove", `{"room_id":"!a:chat.example"}`, 200, `[{"id":1,`},
		{"GET", "/conversations/links", ``, 200, `[{"id":1,"room_ids":["!discord:chat.example","!texts:chat.example"]}]`},
		{"POST", "/conversations/links/remove", `{"room_id":"!nowhere:chat.example"}`, 200, `[{"id":1,`},
		{"POST", "/conversations/links/remove", `{"room_id":"nowhere"}`, 400, "Matrix room id"},
		// A new link gets a new id, never one a deleted link had.
		{"POST", "/conversations/links", `{"room_ids":["!a:chat.example","!b:chat.example"]}`, 200, `{"id":3,`},
	}
	for _, c := range cases {
		rec := call(c.method, c.path, c.body)
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.answer) {
			t.Errorf("%s %s %s: %d %s", c.method, c.path, c.body, rec.Code, rec.Body)
		}
	}
}
