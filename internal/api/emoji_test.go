package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kayushkin/multichat/internal/db"
)

func TestEmojiFavoritesAndSettingsRoutes(t *testing.T) {
	router := routerForSettingsTests(t)
	database, err := db.Initialize(filepath.Join(t.TempDir(), "multichat.db"))
	if err != nil {
		t.Fatal(err)
	}
	router.db = database
	put := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+secretValues["MULTICHAT_TOKEN"])
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	cases := []struct {
		path, body string
		code       int
		answer     string
	}{
		{"/emoji/favorites", `{"favorite_keys":["🔥","👍"]}`, 200, `{"favorite_keys":["🔥","👍"]}`},
		{"/emoji/favorites", `{}`, 400, "favorite_keys is required"},
		{"/emoji/favorites", `{"favorite_keys":["discord-emoji:9"]}`, 400, "not in the custom emoji catalog"},
		{"/emoji/favorites", `{"favorite_keys":[],"extra":1}`, 400, "unknown field"},
		{"/emoji/settings", `{"skin_tone":"🏾","quick_reaction_count":3,"offer_other_servers_emoji":true}`, 200, `"quick_reaction_count":3`},
		{"/emoji/settings", `{"skin_tone":""}`, 400, "all required"},
		{"/emoji/settings", `{"skin_tone":"","quick_reaction_count":99,"offer_other_servers_emoji":false}`, 400, "between 0 and 12"},
	}
	for _, c := range cases {
		rec := put(c.path, c.body)
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.answer) {
			t.Errorf("PUT %s %s: %d %s", c.path, c.body, rec.Code, rec.Body)
		}
	}
	// No DISCORD_DB: GET /emoji says so rather than answering an empty catalog.
	req := httptest.NewRequest(http.MethodGet, "/emoji", nil)
	req.Header.Set("Authorization", "Bearer "+secretValues["MULTICHAT_TOKEN"])
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), "DISCORD_DB") {
		t.Fatalf("GET /emoji without DISCORD_DB: %d %s", rec.Code, rec.Body)
	}
}
