package discordemoji

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kayushkin/multichat/internal/db"
)

func TestServerSweep(t *testing.T) {
	var mu sync.Mutex
	var asked []string
	discord := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "the-secret-login" {
			w.WriteHeader(401)
			return
		}
		mu.Lock()
		asked = append(asked, r.URL.Path)
		mu.Unlock()
		switch r.URL.Path {
		case "/users/@me/guilds":
			w.Write([]byte(`[{"id":"1","name":"Bridged"},{"id":"2","name":"Fresh"},{"id":"3","name":"Stale"},{"id":"4","name":"Never"},{"id":"5","name":"Locked"}]`))
		case "/guilds/3/emojis":
			w.Write([]byte(`[{"id":"30","name":"stale_new","available":true}]`))
		case "/guilds/4/emojis":
			w.Write([]byte(`[{"id":"40","name":"partyParrot","animated":true,"available":true},{"id":"41","name":"gone","available":false}]`))
		case "/guilds/5/emojis":
			w.WriteHeader(403)
			w.Write([]byte(`{"message":"Missing Access"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer discord.Close()

	bridgePath := filepath.Join(t.TempDir(), "mautrix-discord.db")
	bridge := openBridgeAt(t, bridgePath)
	bridge.Exec(bridgeUserSchema)
	bridge.Exec(`INSERT INTO "user" (mxid, dcid, discord_token) VALUES ('@admin:x', '42', 'the-secret-login')`)
	bridge.Exec(`INSERT INTO guild (dcid, name, bridging_mode) VALUES ('1', 'Bridged', 3)`)

	database := openMultichat(t)
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	db.ReplaceDiscordServerEmoji(database, "1", "Bridged", []db.DiscordCustomEmoji{{DiscordEmojiID: "10", Name: "bot_listed"}}, now.Add(-30*24*time.Hour))
	db.ReplaceDiscordServerEmoji(database, "2", "Fresh", []db.DiscordCustomEmoji{{DiscordEmojiID: "20", Name: "fresh"}}, now.Add(-24*time.Hour))
	db.ReplaceDiscordServerEmoji(database, "3", "Stale", []db.DiscordCustomEmoji{{DiscordEmojiID: "31", Name: "stale_old"}}, now.Add(-8*24*time.Hour))
	db.ReplaceDiscordServerEmoji(database, "9", "Left", []db.DiscordCustomEmoji{{DiscordEmojiID: "90", Name: "left"}}, now.Add(-24*time.Hour))

	var slept []time.Duration
	sweeper := &ServerSweeper{HTTPClient: discord.Client(), DiscordAPIBaseURL: discord.URL, Interval: 30 * time.Second,
		StaleAfter: 7 * 24 * time.Hour, Now: func() time.Time { return now }, Sleep: func(d time.Duration) { slept = append(slept, d) }}
	if !sweeper.Start(database, bridgePath) {
		t.Fatal("did not start")
	}
	deadline := time.Now().Add(5 * time.Second)
	for sweeper.Status().Running && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	status := sweeper.Status()
	if status.Running || status.ServersInAccount != 5 || status.ServersToList != 3 || status.ServersListed != 2 {
		t.Fatalf("status = %+v", status)
	}
	if !strings.Contains(status.Error, "Locked (5)") || strings.Contains(status.Error, "the-secret-login") {
		t.Fatalf("error = %q", status.Error)
	}
	// Never listed first, then the oldest; bridged and fresh servers are not asked.
	if strings.Join(asked, " ") != "/users/@me/guilds /guilds/4/emojis /guilds/5/emojis /guilds/3/emojis" {
		t.Fatalf("asked %v", asked)
	}
	if len(slept) != 3 || slept[0] != 30*time.Second {
		t.Fatalf("slept %v", slept)
	}
	list, _ := db.DiscordCustomEmojiList(database)
	var names []string
	for _, e := range list {
		names = append(names, e.DiscordServerName+"/"+e.Name)
	}
	if strings.Join(names, " ") != "Bridged/bot_listed Fresh/fresh Never/partyParrot Stale/stale_new" {
		t.Fatalf("catalog = %v", names)
	}
	if sweeper.Start(database, bridgePath) == false {
		t.Fatal("a finished sweep must let the next one start")
	}
}
