package discordemoji

import (
	"bytes"
	"database/sql"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kayushkin/multichat/internal/db"
)

// The two bridge tables this package touches, as mautrix-discord creates them.
const bridgeSchema = `
CREATE TABLE guild (dcid TEXT PRIMARY KEY, mxid TEXT, plain_name TEXT NOT NULL DEFAULT '', name TEXT NOT NULL DEFAULT '', name_set BOOLEAN NOT NULL DEFAULT false, avatar TEXT NOT NULL DEFAULT '', avatar_url TEXT NOT NULL DEFAULT '', avatar_set BOOLEAN NOT NULL DEFAULT false, bridging_mode INTEGER NOT NULL DEFAULT 0);
CREATE TABLE discord_file (
    url       TEXT,
    encrypted BOOLEAN,
    mxc       TEXT NOT NULL,
    id         TEXT,
    emoji_name TEXT,
    size            BIGINT NOT NULL,
    width           INTEGER,
    height          INTEGER,
    mime_type       TEXT NOT NULL,
    decryption_info jsonb,
    timestamp       BIGINT NOT NULL,
    PRIMARY KEY (url, encrypted)
);
`

func openBridge(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mautrix-discord.db")
	bridge, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { bridge.Close() })
	if _, err := bridge.Exec(bridgeSchema); err != nil {
		t.Fatal(err)
	}
	return bridge
}

func openMultichat(t *testing.T) *sql.DB {
	t.Helper()
	database, err := db.Initialize(filepath.Join(t.TempDir(), "multichat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

// A refresh resolves the bot token by credential id, lists every bridged
// server's emoji with it, and keeps the available ones; a server no longer
// bridged is not asked for.
func TestRefreshListsEveryBridgedServersEmoji(t *testing.T) {
	bridge := openBridge(t)
	bridge.Exec(`INSERT INTO guild (dcid, name, bridging_mode) VALUES ('111', 'Riverside', 3), ('222', 'Pretend', 1), ('333', 'Unbridged', 0)`)
	var asked []string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/credentials/cred_bot/resolve":
			if r.Header.Get("Authorization") != "Bearer auth-token" {
				w.WriteHeader(401)
				return
			}
			w.Write([]byte(`{"auth_type":"api_key","api_key":"bot-token"}`))
		case strings.HasPrefix(r.URL.Path, "/guilds/"):
			if r.Header.Get("Authorization") != "Bot bot-token" {
				w.WriteHeader(401)
				return
			}
			asked = append(asked, r.URL.Path)
			if r.URL.Path == "/guilds/111/emojis" {
				w.Write([]byte(`[{"id":"1","name":"partyParrot","animated":true,"available":true},{"id":"2","name":"lostBoost","available":false}]`))
			} else {
				w.Write([]byte(`[{"id":"3","name":"parrot","animated":false,"available":true}]`))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()
	database := openMultichat(t)
	refresher := Refresher{HTTPClient: fake.Client(), AuthStoreURL: fake.URL, AuthStoreToken: "auth-token", BotCredentialID: "cred_bot", DiscordAPIBaseURL: fake.URL}
	if err := refresher.Refresh(database, bridge, time.Now()); err != nil {
		t.Fatal(err)
	}
	if strings.Join(asked, " ") != "/guilds/222/emojis /guilds/111/emojis" {
		t.Fatalf("asked %v", asked)
	}
	list, err := db.DiscordCustomEmojiList(database)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Name != "parrot" || list[0].DiscordServerName != "Pretend" || list[1].Name != "partyParrot" || !list[1].Animated || list[1].ReactionKey != "discord-emoji:1" {
		t.Fatalf("catalog = %+v", list)
	}
}

func TestRefreshWithoutACredentialIsNotConfigured(t *testing.T) {
	err := Refresher{AuthStoreURL: "http://x", AuthStoreToken: "t"}.Refresh(nil, nil, time.Now())
	if err == nil || !strings.Contains(err.Error(), "DISCORD_EMOJI_BOT_CREDENTIAL_ID") {
		t.Fatalf("err = %v", err)
	}
}

type recordingUploader struct{ uploads []string }

func (u *recordingUploader) UploadMedia(data []byte, contentType, filename string) (string, error) {
	u.uploads = append(u.uploads, contentType+" "+filename)
	return "mxc://chat.example/uploaded-" + filename, nil
}

// Teaching adds the row the bridge would have added, under the CDN address the
// bridge files the emoji by, and a second teach reuses it.
func TestTeachAddsTheBridgesOwnRow(t *testing.T) {
	var pngImage bytes.Buffer
	png.Encode(&pngImage, image.NewRGBA(image.Rect(0, 0, 48, 32)))
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.RequestURI() {
		case "/emojis/3.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write(pngImage.Bytes())
		case "/emojis/1.webp?animated=true":
			w.Header().Set("Content-Type", "image/webp")
			w.Write([]byte("RIFF-not-decoded"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer cdn.Close()
	bridge := openBridge(t)
	uploader := &recordingUploader{}
	teacher := Teacher{HTTPClient: cdn.Client(), Uploader: uploader, CDNBaseURL: cdn.URL, Now: func() time.Time { return time.UnixMilli(1790000000000) }}

	mxc, err := teacher.Teach(bridge, db.DiscordCustomEmoji{DiscordEmojiID: "3", Name: "parrot"})
	if err != nil || mxc != "mxc://chat.example/uploaded-parrot" {
		t.Fatalf("teach = %q, %v", mxc, err)
	}
	var url, name, mime string
	var width, height sql.NullInt64
	var size, timestamp int64
	if err := bridge.QueryRow(`SELECT url, emoji_name, mime_type, width, height, size, timestamp FROM discord_file WHERE id = '3'`).
		Scan(&url, &name, &mime, &width, &height, &size, &timestamp); err != nil {
		t.Fatal(err)
	}
	if url != "https://cdn.discordapp.com/emojis/3.png" || name != "parrot" || mime != "image/png" || width.Int64 != 48 || height.Int64 != 32 || size != int64(pngImage.Len()) || timestamp != 1790000000000 {
		t.Fatalf("row = %s %s %s %v %v %d %d", url, name, mime, width, height, size, timestamp)
	}

	mxc, err = teacher.Teach(bridge, db.DiscordCustomEmoji{DiscordEmojiID: "1", Name: "partyParrot", Animated: true})
	if err != nil || mxc != "mxc://chat.example/uploaded-partyParrot" {
		t.Fatalf("teach animated = %q, %v", mxc, err)
	}
	if err := bridge.QueryRow(`SELECT url, width FROM discord_file WHERE id = '1'`).Scan(&url, &width); err != nil {
		t.Fatal(err)
	}
	if url != "https://cdn.discordapp.com/emojis/1.webp?animated=true" || width.Valid {
		t.Fatalf("animated row = %s %v", url, width)
	}

	if _, err := teacher.Teach(bridge, db.DiscordCustomEmoji{DiscordEmojiID: "3", Name: "parrot"}); err != nil {
		t.Fatal(err)
	}
	if len(uploader.uploads) != 2 {
		t.Fatalf("uploads = %v; a known emoji must not be uploaded again", uploader.uploads)
	}
}

// An emoji the bridge already relayed from Discord keeps the bridge's mxc.
func TestTeachUsesTheBridgesOwnMXC(t *testing.T) {
	bridge := openBridge(t)
	bridge.Exec(`INSERT INTO discord_file (url, encrypted, mxc, id, emoji_name, size, mime_type, timestamp) VALUES ('https://cdn.discordapp.com/emojis/3.gif', false, 'mxc://chat.example/bridges', '3', 'parrot', 1, 'image/gif', 1)`)
	uploader := &recordingUploader{}
	mxc, err := Teacher{Uploader: uploader}.Teach(bridge, db.DiscordCustomEmoji{DiscordEmojiID: "3", Name: "parrot"})
	if err != nil || mxc != "mxc://chat.example/bridges" || len(uploader.uploads) != 0 {
		t.Fatalf("teach = %q, %v, uploads %v", mxc, err, uploader.uploads)
	}
}

const bridgeUserSchema = `CREATE TABLE "user" (mxid TEXT PRIMARY KEY, dcid TEXT UNIQUE, discord_token TEXT, management_room TEXT,
	space_room TEXT, dm_space_room TEXT, read_state_version INTEGER NOT NULL DEFAULT 0, heartbeat_session jsonb);`

// The check sends the login bare, reads premium_type, and never puts the
// login in an error.
func TestNitroCheck(t *testing.T) {
	var sentAuthorization string
	status := 200
	answer := `{"id":"42","premium_type":2}`
	discord := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/@me" {
			http.NotFound(w, r)
			return
		}
		sentAuthorization = r.Header.Get("Authorization")
		w.WriteHeader(status)
		w.Write([]byte(answer))
	}))
	defer discord.Close()
	bridge := openBridge(t)
	checker := NitroChecker{HTTPClient: discord.Client(), DiscordAPIBaseURL: discord.URL}
	if _, err := bridge.Exec(bridgeUserSchema); err != nil {
		t.Fatal(err)
	}
	if _, _, err := checker.Check(bridge); !errors.Is(err, ErrNoDiscordLogin) {
		t.Fatalf("no login: %v", err)
	}
	bridge.Exec(`INSERT INTO "user" (mxid, dcid, discord_token) VALUES ('@admin:x', '42', 'the-secret-login')`)

	id, premium, err := checker.Check(bridge)
	if err != nil || id != "42" || premium != 2 || sentAuthorization != "the-secret-login" {
		t.Fatalf("check = %s %d %v, sent %q", id, premium, err, sentAuthorization)
	}

	status, answer = 401, `{"message": "401: Unauthorized", "code": 0}`
	_, _, err = checker.Check(bridge)
	if err == nil || strings.Contains(err.Error(), "the-secret-login") || !strings.Contains(err.Error(), "401") {
		t.Fatalf("refused check: %v", err)
	}

	status, answer = 200, `{"id":"99","premium_type":0}`
	if _, _, err := checker.Check(bridge); err == nil || !strings.Contains(err.Error(), "99") {
		t.Fatalf("another account's answer: %v", err)
	}
}

// Each emoji the bridge has seen is listed once, animated when Discord
// serves it animated.
func TestBridgeSeenEmoji(t *testing.T) {
	bridge := openBridge(t)
	bridge.Exec(`INSERT INTO discord_file (url, encrypted, mxc, id, emoji_name, size, mime_type, timestamp) VALUES
		('https://cdn.discordapp.com/emojis/1.gif', false, 'mxc://x/old', '1', 'dance', 1, 'image/gif', 1),
		('https://cdn.discordapp.com/emojis/1.webp?animated=true', false, 'mxc://x/new', '1', 'dance', 1, 'image/webp', 2),
		('https://cdn.discordapp.com/emojis/2.png', false, 'mxc://x/two', '2', ':wave:', 1, 'image/png', 3),
		('https://cdn.discordapp.com/attachments/3/photo.png', false, 'mxc://x/photo', '', '', 1, 'image/png', 4)`)
	seen, err := BridgeSeenEmoji(bridge)
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0].BridgeMXC != "mxc://x/new" || !seen[0].Animated || seen[1].Animated || seen[1].ReactionKey != "discord-emoji:2" || seen[1].Name != "wave" {
		t.Fatalf("seen = %+v", seen)
	}
}
