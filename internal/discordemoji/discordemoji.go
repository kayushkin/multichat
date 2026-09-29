// Package discordemoji keeps multichat's catalog of the custom emoji of every
// Discord server the mautrix-discord bridge bridges, and teaches the bridge an
// emoji before multichat reacts with it.
//
// Discord owns the emoji and their ids. The catalog is fetched with a Discord
// bot's token, which auth-store holds, from GET /guilds/{id}/emojis. The bot
// must be a member of each bridged server.
//
// The bridge turns a Matrix reaction whose key is an mxc:// URI into a Discord
// emoji only when its own discord_file table holds that URI with the emoji's
// id and name (mautrix-discord portal.go, handleMatrixReaction; direct_media
// is off here). It adds a row the first time it relays an emoji from Discord.
// For an emoji it has not seen, Teach does what the bridge would: fetch the
// image from Discord's CDN, upload it to Synapse, and add the row. That is a
// write into the bridge's own database, so the row must match the bridge's
// format exactly (see emojiCDNURL).
package discordemoji

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kayushkin/multichat/internal/db"
	_ "github.com/mattn/go-sqlite3"
)

// DiscordAPIBaseURL is Discord's REST API.
const DiscordAPIBaseURL = "https://discord.com/api/v10"

// DiscordCDNBaseURL is where Discord serves emoji images.
const DiscordCDNBaseURL = "https://cdn.discordapp.com"

// Refresher fetches the catalog.
type Refresher struct {
	HTTPClient *http.Client
	// AuthStoreURL and AuthStoreToken reach auth-store; BotCredentialID names
	// the credential whose api_key is the bot token.
	AuthStoreURL    string
	AuthStoreToken  string
	BotCredentialID string
	// DiscordAPIBaseURL is DiscordAPIBaseURL outside tests.
	DiscordAPIBaseURL string
}

// ErrNotConfigured names the setting a refresh is missing.
var ErrNotConfigured = errors.New("not configured")

// BridgedServer is one Discord server the bridge bridges.
type BridgedServer struct {
	DiscordServerID string
	Name            string
}

// BridgedServers lists the servers the bridge bridges (guild.bridging_mode ≠ 0).
func BridgedServers(bridgeDatabase *sql.DB) ([]BridgedServer, error) {
	rows, err := bridgeDatabase.Query(`SELECT dcid, name FROM guild WHERE bridging_mode != 0 ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("bridged servers: %w", err)
	}
	defer rows.Close()
	var out []BridgedServer
	for rows.Next() {
		var s BridgedServer
		if err := rows.Scan(&s.DiscordServerID, &s.Name); err != nil {
			return nil, fmt.Errorf("scan bridged server: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// discordEmoji is an emoji object as GET /guilds/{id}/emojis returns it.
type discordEmoji struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Animated bool   `json:"animated"`
	// Available is false when the server lost the boost level the emoji needs.
	Available bool `json:"available"`
}

// Refresh fetches every bridged server's emoji and replaces its catalog rows.
// It stops at the first server that fails, leaving the servers before it
// refreshed.
func (r Refresher) Refresh(database, bridgeDatabase *sql.DB, now time.Time) error {
	if r.AuthStoreURL == "" || r.AuthStoreToken == "" {
		return fmt.Errorf("%w: AUTH_STORE_URL and AUTH_STORE_TOKEN are needed to fetch the Discord bot token", ErrNotConfigured)
	}
	if r.BotCredentialID == "" {
		return fmt.Errorf("%w: DISCORD_EMOJI_BOT_CREDENTIAL_ID names no auth-store credential", ErrNotConfigured)
	}
	botToken, err := r.resolveBotToken()
	if err != nil {
		return err
	}
	servers, err := BridgedServers(bridgeDatabase)
	if err != nil {
		return err
	}
	for _, server := range servers {
		fetched, err := r.fetchServerEmoji(botToken, server.DiscordServerID)
		if err != nil {
			return fmt.Errorf("server %s (%s): %w", server.Name, server.DiscordServerID, err)
		}
		var emoji []db.DiscordCustomEmoji
		for _, e := range fetched {
			if !e.Available {
				continue
			}
			emoji = append(emoji, db.DiscordCustomEmoji{DiscordEmojiID: e.ID, Name: e.Name, Animated: e.Animated})
		}
		if err := db.ReplaceDiscordServerEmoji(database, server.DiscordServerID, server.Name, emoji, now); err != nil {
			return err
		}
	}
	return nil
}

func (r Refresher) resolveBotToken() (string, error) {
	req, err := http.NewRequest("GET", strings.TrimRight(r.AuthStoreURL, "/")+"/api/credentials/"+r.BotCredentialID+"/resolve", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+r.AuthStoreToken)
	req.Header.Set("X-Auth-App", "multichat")
	req.Header.Set("X-Auth-Reason", "list the custom emoji of bridged Discord servers")
	resp, err := r.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("auth-store: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("auth-store: %w", err)
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("auth-store: resolve credential %s: %d %s", r.BotCredentialID, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var out struct {
		AuthType string `json:"auth_type"`
		APIKey   string `json:"api_key"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("auth-store: credential %s: %w", r.BotCredentialID, err)
	}
	if out.AuthType != "api_key" || out.APIKey == "" {
		return "", fmt.Errorf("auth-store: credential %s is %q with no api_key; expected the Discord bot token", r.BotCredentialID, out.AuthType)
	}
	return out.APIKey, nil
}

func (r Refresher) fetchServerEmoji(botToken, serverID string) ([]discordEmoji, error) {
	req, err := http.NewRequest("GET", strings.TrimRight(r.DiscordAPIBaseURL, "/")+"/guilds/"+serverID+"/emojis", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bot "+botToken)
	resp, err := r.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("discord: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("discord: %w", err)
	}
	if resp.StatusCode != 200 {
		hint := ""
		if resp.StatusCode == 403 || resp.StatusCode == 404 {
			hint = " (is the bot a member of this server?)"
		}
		return nil, fmt.Errorf("discord: GET /guilds/%s/emojis: %d %s%s", serverID, resp.StatusCode, strings.TrimSpace(string(data)), hint)
	}
	var out []discordEmoji
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("discord: GET /guilds/%s/emojis: %w", serverID, err)
	}
	return out, nil
}

// OpenBridgeDatabaseForWriting opens the bridge's database to add emoji rows.
// The bridge has it open too (WAL mode); the busy timeout waits out its writes.
func OpenBridgeDatabaseForWriting(path string) (*sql.DB, error) {
	database, err := sql.Open("sqlite3", "file:"+path+"?mode=rw&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open DISCORD_DB %s for writing: %w", path, err)
	}
	return database, nil
}

// BridgeEmojiMXCs maps each Discord emoji id the bridge knows to its mxc:// URI.
func BridgeEmojiMXCs(bridgeDatabase *sql.DB) (map[string]string, error) {
	rows, err := bridgeDatabase.Query(`SELECT id, mxc FROM discord_file WHERE id IS NOT NULL AND id != '' AND emoji_name IS NOT NULL AND emoji_name != '' AND encrypted = 0 ORDER BY timestamp`)
	if err != nil {
		return nil, fmt.Errorf("bridge emoji: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, mxc string
		if err := rows.Scan(&id, &mxc); err != nil {
			return nil, fmt.Errorf("scan bridge emoji: %w", err)
		}
		out[id] = mxc
	}
	return out, rows.Err()
}

// emojiCDNURL is the address the bridge files an emoji under in discord_file
// (url is half its primary key), so a row multichat adds is the one the
// bridge finds when the emoji later comes from Discord. The bridge takes it
// from beeper/discordgo's EndpointEmoji and EndpointEmojiAnimated; the rows it
// wrote on this host show a static emoji as .png and an animated one as
// .webp?animated=true, served as image/webp.
func emojiCDNURL(cdnBaseURL, discordEmojiID string, animated bool) string {
	if animated {
		return strings.TrimRight(cdnBaseURL, "/") + "/emojis/" + discordEmojiID + ".webp?animated=true"
	}
	return strings.TrimRight(cdnBaseURL, "/") + "/emojis/" + discordEmojiID + ".png"
}

// Uploader puts a file on Synapse and returns its mxc:// URI.
type Uploader interface {
	UploadMedia(data []byte, contentType, filename string) (string, error)
}

// Teacher adds an emoji to the bridge's discord_file table.
type Teacher struct {
	HTTPClient *http.Client
	Uploader   Uploader
	// CDNBaseURL is DiscordCDNBaseURL outside tests. The row always records
	// the address under DiscordCDNBaseURL, as the bridge would.
	CDNBaseURL string
	Now        func() time.Time
}

// Teach returns the mxc:// URI the bridge knows emoji by, adding it to the
// bridge's database first when the bridge has not seen it.
func (t Teacher) Teach(bridgeDatabase *sql.DB, emoji db.DiscordCustomEmoji) (string, error) {
	known, err := BridgeEmojiMXCs(bridgeDatabase)
	if err != nil {
		return "", err
	}
	if mxc := known[emoji.DiscordEmojiID]; mxc != "" {
		return mxc, nil
	}
	recordedURL := emojiCDNURL(DiscordCDNBaseURL, emoji.DiscordEmojiID, emoji.Animated)
	resp, err := t.HTTPClient.Get(emojiCDNURL(t.CDNBaseURL, emoji.DiscordEmojiID, emoji.Animated))
	if err != nil {
		return "", fmt.Errorf("fetch emoji %s from Discord's CDN: %w", emoji.Name, err)
	}
	defer resp.Body.Close()
	imageData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("fetch emoji %s from Discord's CDN: %w", emoji.Name, err)
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("fetch emoji %s from Discord's CDN: %d", emoji.Name, resp.StatusCode)
	}
	mimeType := strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])
	if !strings.HasPrefix(mimeType, "image/") {
		return "", fmt.Errorf("fetch emoji %s from Discord's CDN: Content-Type %q is not an image", emoji.Name, mimeType)
	}
	mxc, err := t.Uploader.UploadMedia(imageData, mimeType, emoji.Name)
	if err != nil {
		return "", fmt.Errorf("upload emoji %s to Synapse: %w", emoji.Name, err)
	}
	if !strings.HasPrefix(mxc, "mxc://") {
		return "", fmt.Errorf("upload emoji %s to Synapse: got %q, not an mxc:// URI", emoji.Name, mxc)
	}
	// The bridge records dimensions only for images it can decode; it
	// leaves them NULL for webp.
	var width, height any
	if config, _, err := imageConfig(imageData); err == nil {
		width, height = config.Width, config.Height
	}
	if _, err := bridgeDatabase.Exec(`INSERT INTO discord_file (url, encrypted, mxc, id, emoji_name, size, width, height, mime_type, decryption_info, timestamp)
		VALUES (?, false, ?, ?, ?, ?, ?, ?, ?, NULL, ?) ON CONFLICT (url, encrypted) DO NOTHING`,
		recordedURL, mxc, emoji.DiscordEmojiID, emoji.Name, len(imageData), width, height, mimeType, t.Now().UnixMilli()); err != nil {
		return "", fmt.Errorf("add emoji %s to the bridge's discord_file: %w", emoji.Name, err)
	}
	// The bridge may have added the same url between the read and the
	// insert; its row wins, so read back whichever is there.
	var stored string
	if err := bridgeDatabase.QueryRow(`SELECT mxc FROM discord_file WHERE url = ? AND encrypted = false`, recordedURL).Scan(&stored); err != nil {
		return "", fmt.Errorf("read back emoji %s from the bridge's discord_file: %w", emoji.Name, err)
	}
	return stored, nil
}

func imageConfig(data []byte) (image.Config, string, error) {
	return image.DecodeConfig(bytes.NewReader(data))
}

// BridgeSeenEmoji returns every custom emoji the bridge has relayed from
// Discord, once per emoji id (the newest row when the bridge filed one under
// two addresses). The bridge records no server for them.
func BridgeSeenEmoji(bridgeDatabase *sql.DB) ([]db.DiscordCustomEmoji, error) {
	rows, err := bridgeDatabase.Query(`SELECT id, emoji_name, mxc, url, timestamp FROM discord_file
		WHERE id IS NOT NULL AND id != '' AND emoji_name IS NOT NULL AND emoji_name != '' AND encrypted = 0 ORDER BY timestamp`)
	if err != nil {
		return nil, fmt.Errorf("bridge emoji: %w", err)
	}
	defer rows.Close()
	byID := map[string]db.DiscordCustomEmoji{}
	var order []string
	for rows.Next() {
		var id, name, mxc, url string
		var timestamp int64
		if err := rows.Scan(&id, &name, &mxc, &url, &timestamp); err != nil {
			return nil, fmt.Errorf("scan bridge emoji: %w", err)
		}
		if _, seen := byID[id]; !seen {
			order = append(order, id)
		}
		byID[id] = db.DiscordCustomEmoji{
			// The bridge files some names as ":name:" (from a reaction's
			// shortcode); the catalog's names carry no colons.
			DiscordEmojiID: id, Name: strings.Trim(name, ":"),
			// Discord serves an animated emoji as .gif, or as .webp?animated=true.
			Animated:    strings.Contains(url, ".gif") || strings.Contains(url, "animated=true"),
			FetchedAt:   time.UnixMilli(timestamp).UTC(),
			ReactionKey: db.DiscordEmojiReactionKeyPrefix + id,
			BridgeMXC:   mxc,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]db.DiscordCustomEmoji, 0, len(order))
	for _, id := range order {
		out = append(out, byID[id])
	}
	return out, nil
}

// NitroChecker asks Discord whether the bridge's account has Nitro.
type NitroChecker struct {
	HTTPClient *http.Client
	// DiscordAPIBaseURL is DiscordAPIBaseURL outside tests.
	DiscordAPIBaseURL string
}

// ErrNoDiscordLogin is returned when the bridge holds no logged-in account.
var ErrNoDiscordLogin = errors.New("the Discord bridge has no logged-in account")

// Check reads the account's Discord login from the bridge's user table and
// makes one call, GET /users/@me, the call Discord's own app makes when it
// opens. The login is held only for that call: it is never logged, stored or
// put in an error. It returns the account's Discord user id and Discord's
// premium_type.
func (c NitroChecker) Check(bridgeDatabase *sql.DB) (string, int, error) {
	rows, err := bridgeDatabase.Query(`SELECT dcid, discord_token FROM "user" WHERE discord_token IS NOT NULL AND discord_token != '' AND dcid IS NOT NULL AND dcid != ''`)
	if err != nil {
		return "", 0, fmt.Errorf("read the bridge's Discord account: %w", err)
	}
	type login struct{ discordUserID, token string }
	var logins []login
	for rows.Next() {
		var l login
		if err := rows.Scan(&l.discordUserID, &l.token); err != nil {
			rows.Close()
			return "", 0, fmt.Errorf("read the bridge's Discord account: %w", err)
		}
		logins = append(logins, l)
	}
	rows.Close()
	if len(logins) == 0 {
		return "", 0, ErrNoDiscordLogin
	}
	if len(logins) > 1 {
		return "", 0, fmt.Errorf("the Discord bridge holds %d logged-in accounts; multichat expects one", len(logins))
	}
	account := logins[0]
	req, err := http.NewRequest("GET", strings.TrimRight(c.DiscordAPIBaseURL, "/")+"/users/@me", nil)
	if err != nil {
		return "", 0, err
	}
	// A user login goes bare, without the "Bot " a bot token takes.
	req.Header.Set("Authorization", account.token)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		// The request error names the URL, never the header.
		return "", 0, fmt.Errorf("discord: GET /users/@me: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("discord: GET /users/@me: %w", err)
	}
	if resp.StatusCode != 200 {
		return "", 0, fmt.Errorf("discord: GET /users/@me: %d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var me struct {
		ID          string `json:"id"`
		PremiumType *int   `json:"premium_type"`
	}
	if err := json.Unmarshal(data, &me); err != nil {
		return "", 0, fmt.Errorf("discord: GET /users/@me: %w", err)
	}
	if me.ID != account.discordUserID {
		return "", 0, fmt.Errorf("discord: GET /users/@me answered for user %s, but the bridge's account is %s", me.ID, account.discordUserID)
	}
	if me.PremiumType == nil {
		return "", 0, fmt.Errorf("discord: GET /users/@me for %s carried no premium_type", me.ID)
	}
	return me.ID, *me.PremiumType, nil
}
