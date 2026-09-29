package api

import (
	"database/sql"
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
		{"/emoji/settings", `{"skin_tone":"🏾","quick_reaction_count":3}`, 200, `"quick_reaction_count":3`},
		{"/emoji/settings", `{"skin_tone":""}`, 400, "both required"},
		{"/emoji/settings", `{"skin_tone":"","quick_reaction_count":99}`, 400, "between 0 and 12"},
		{"/emoji/settings", `{"skin_tone":"","quick_reaction_count":3,"offer_other_servers_emoji":true}`, 400, "unknown field"},
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

// The Nitro check asks Discord once a day however often the Messages pages
// open, and again when forced.
func TestNitroCheckIsOncePerDay(t *testing.T) {
	calls := 0
	discord := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{"id":"42","premium_type":3}`))
	}))
	defer discord.Close()
	router := routerForSettingsTests(t)
	database, err := db.Initialize(filepath.Join(t.TempDir(), "multichat.db"))
	if err != nil {
		t.Fatal(err)
	}
	router.db = database
	router.discordNitroChecker.DiscordAPIBaseURL = discord.URL
	router.discordDBPath = filepath.Join(t.TempDir(), "mautrix-discord.db")
	bridge, err := sql.Open("sqlite3", router.discordDBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.Close()
	if _, err := bridge.Exec(`CREATE TABLE "user" (mxid TEXT PRIMARY KEY, dcid TEXT, discord_token TEXT);
		INSERT INTO "user" VALUES ('@admin:x', '42', 'login');`); err != nil {
		t.Fatal(err)
	}
	post := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.Header.Set("Authorization", "Bearer "+secretValues["MULTICHAT_TOKEN"])
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	first := post("/emoji/discord/nitro-check")
	if first.Code != 200 || !strings.Contains(first.Body.String(), `"has_nitro":true`) || !strings.Contains(first.Body.String(), `"checked_now":true`) {
		t.Fatalf("first: %d %s", first.Code, first.Body)
	}
	if again := post("/emoji/discord/nitro-check"); !strings.Contains(again.Body.String(), `"checked_now":false`) || calls != 1 {
		t.Fatalf("second the same day: %s, %d calls", again.Body, calls)
	}
	if forced := post("/emoji/discord/nitro-check?force=true"); !strings.Contains(forced.Body.String(), `"checked_now":true`) || calls != 2 {
		t.Fatalf("forced: %s, %d calls", forced.Body, calls)
	}
}
