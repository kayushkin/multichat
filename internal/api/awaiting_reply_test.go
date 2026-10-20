package api

import (
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/kayushkin/multichat/internal/db"
)

func TestAwaitingReplySettingsAndClearsRoutes(t *testing.T) {
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
	if rec := call("POST", "/tags", `{"name":"0"}`); rec.Code != 200 {
		t.Fatalf("POST /tags: %d %s", rec.Code, rec.Body)
	}
	whole := func(tags string, members, days int) string {
		return `{"tag_ids":` + tags + `,"max_group_member_count":` + strconv.Itoa(members) + `,"max_message_age_days":` + strconv.Itoa(days) + `,"clear_when_opened":true,"clear_when_replied":false}`
	}
	cases := []struct {
		method, path, body string
		code               int
		answer             string
	}{
		{"GET", "/awaiting-reply/settings", ``, 200, `"tag_ids":[],"max_group_member_count":5,"max_message_age_days":30,"clear_when_opened":true,"clear_when_replied":true`},
		{"PUT", "/awaiting-reply/settings", whole(`[1,1]`, 5, 30), 200, `"tag_ids":[1],"max_group_member_count":5,"max_message_age_days":30,"clear_when_opened":true,"clear_when_replied":false`},
		{"GET", "/awaiting-reply/settings", ``, 200, `"tag_ids":[1]`},
		{"PUT", "/awaiting-reply/settings", whole(`[9]`, 5, 30), 400, "no tag has id 9"},
		{"PUT", "/awaiting-reply/settings", whole(`[1]`, 1, 30), 400, "at least 2"},
		{"PUT", "/awaiting-reply/settings", whole(`[1]`, 5, 0), 400, "at least 1"},
		{"PUT", "/awaiting-reply/settings", `{"tag_ids":[1]}`, 400, "every field is required"},
		{"PUT", "/awaiting-reply/settings", strings.TrimSuffix(whole(`[1]`, 5, 30), "}") + `,"snooze":1}`, 400, "unknown field"},
		{"GET", "/awaiting-reply/settings", ``, 200, `"tag_ids":[1],"max_group_member_count":5`},
		{"POST", "/awaiting-reply/clears", `{"room_id":"!d:chat.example","reason":"opened","cleared_through":"2026-10-20T15:00:00Z"}`, 200, `"cleared_through":"2026-10-20T15:00:00Z"`},
		{"POST", "/awaiting-reply/clears", `{"room_id":"!d:chat.example","reason":"dismissed","cleared_through":"2026-10-20T14:00:00Z"}`, 200, `"cleared_through":"2026-10-20T15:00:00Z","cleared_at"`},
		{"POST", "/awaiting-reply/clears", `{"room_id":"!d:chat.example","reason":"dismissed","cleared_through":"2026-10-20T16:00:00Z"}`, 200, `"reason":"dismissed"`},
		{"POST", "/awaiting-reply/clears", `{"room_id":"!d:chat.example","reason":"snoozed","cleared_through":"2026-10-20T16:00:00Z"}`, 400, "opened, dismissed"},
		{"POST", "/awaiting-reply/clears", `{"room_id":"d","reason":"opened","cleared_through":"2026-10-20T16:00:00Z"}`, 400, "Matrix room id"},
		{"POST", "/awaiting-reply/clears", `{"room_id":"!d:chat.example","reason":"opened"}`, 400, "cleared_through is required"},
		{"GET", "/awaiting-reply/clears", ``, 405, "Method not allowed"},
		{"GET", "/awaiting-reply", ``, 503, "Matrix client not configured"},
	}
	for _, c := range cases {
		rec := call(c.method, c.path, c.body)
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.answer) {
			t.Errorf("%s %s %s: %d %s", c.method, c.path, c.body, rec.Code, rec.Body)
		}
	}
}
