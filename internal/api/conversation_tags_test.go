package api

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kayushkin/multichat/internal/db"
)

func TestConversationTagsAssignListRemoveAndDeleteWithTheTag(t *testing.T) {
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
	if rec := call("POST", "/tags", `{"name":"social"}`); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"id":1`) {
		t.Fatalf("POST /tags: %d %s", rec.Code, rec.Body)
	}
	cases := []struct {
		method, path, body string
		code               int
		answer             string
	}{
		{"POST", "/conversations/tags", `{"room_id":"!general:chat.example","tag_id":1}`, 200, `"tags":[{"id":1,"name":"social"`},
		{"POST", "/conversations/tags", `{"room_id":"!general:chat.example","tag_id":1}`, 200, `"tags":[{"id":1,`},
		{"POST", "/conversations/tags", `{"room_id":"!general:chat.example","tag_id":9}`, 404, "no tag has that id"},
		{"POST", "/conversations/tags", `{"room_id":"general","tag_id":1}`, 400, "Matrix room id"},
		{"POST", "/conversations/tags", `{"room_id":"!general:chat.example"}`, 400, "required"},
		{"POST", "/conversations/tags", `{"room_id":"!general:chat.example","tag_id":1,"x":1}`, 400, "unknown field"},
		{"GET", "/conversations/tags/bulk", ``, 200, `{"!general:chat.example":[{"id":1,"name":"social"`},
		{"DELETE", "/conversations/tags?room=!general:chat.example&tag=x", ``, 400, "numeric tag"},
		{"DELETE", "/conversations/tags?room=!general:chat.example&tag=1", ``, 200, `"tags":[]`},
		{"GET", "/conversations/tags/bulk", ``, 200, `{}`},
		{"POST", "/conversations/tags", `{"room_id":"!general:chat.example","tag_id":1}`, 200, `"id":1`},
		{"DELETE", "/tags?id=1", ``, 200, "deleted"},
		{"GET", "/conversations/tags/bulk", ``, 200, `{}`},
	}
	for _, c := range cases {
		rec := call(c.method, c.path, c.body)
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.answer) {
			t.Errorf("%s %s %s: %d %s", c.method, c.path, c.body, rec.Code, rec.Body)
		}
	}
}
