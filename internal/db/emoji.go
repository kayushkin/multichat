package db

// The emoji records: the custom emoji of every bridged Discord server, as
// Discord listed them at the last refresh, and the operator's favourite emoji
// and emoji settings. Rendered to TypeScript (tygo.yaml) for bridge-ui's
// pickers and Emoji page.

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// DiscordEmojiReactionKeyPrefix starts the key of a custom Discord emoji in
// multichat: "discord-emoji:<discord emoji id>". It is the key an archive
// row's reactions carry, the key a favourite is stored under, and a key
// POST /conversations/{room_id}/reactions accepts.
const DiscordEmojiReactionKeyPrefix = "discord-emoji:"

const emojiSchema = `
-- The custom emoji of each bridged Discord server, as Discord's
-- GET /guilds/{id}/emojis listed them at the last refresh. Discord owns every
-- id. A refresh replaces a server's rows whole, so an emoji deleted on
-- Discord leaves the catalog.
CREATE TABLE IF NOT EXISTS discord_custom_emoji (
	discord_emoji_id     TEXT PRIMARY KEY,
	name                 TEXT NOT NULL,
	animated             INTEGER NOT NULL,
	discord_server_id    TEXT NOT NULL,
	discord_server_name  TEXT NOT NULL,
	fetched_at           DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_discord_custom_emoji_server ON discord_custom_emoji(discord_server_id);

-- When each bridged server's emoji were last fetched.
CREATE TABLE IF NOT EXISTS discord_custom_emoji_refreshes (
	discord_server_id    TEXT PRIMARY KEY,
	discord_server_name  TEXT NOT NULL,
	refreshed_at         DATETIME NOT NULL
);

-- The operator's favourite emoji, in the order they chose. emoji_key is a
-- unicode emoji or "discord-emoji:<id>".
CREATE TABLE IF NOT EXISTS emoji_favorites (
	emoji_key  TEXT PRIMARY KEY,
	position   INTEGER NOT NULL
);

-- The operator's emoji settings: one row.
CREATE TABLE IF NOT EXISTS emoji_settings (
	id                            INTEGER PRIMARY KEY CHECK (id = 1),
	skin_tone                     TEXT NOT NULL DEFAULT '',
	quick_reaction_count          INTEGER NOT NULL DEFAULT 5,
	offer_other_servers_emoji     INTEGER NOT NULL DEFAULT 0
);
`

func createEmojiSchema(database *sql.DB) error {
	if _, err := database.Exec(emojiSchema); err != nil {
		return fmt.Errorf("emoji schema: %w", err)
	}
	return nil
}

// DiscordCustomEmoji is one custom emoji of a bridged Discord server.
type DiscordCustomEmoji struct {
	DiscordEmojiID string `json:"discord_emoji_id"`
	// Name is the emoji's name without colons, e.g. "partyParrot".
	Name     string `json:"name"`
	Animated bool   `json:"animated"`
	// DiscordServerID is the server the emoji belongs to.
	DiscordServerID   string    `json:"discord_server_id"`
	DiscordServerName string    `json:"discord_server_name"`
	FetchedAt         time.Time `json:"fetched_at"`
	// ReactionKey is "discord-emoji:<discord_emoji_id>".
	ReactionKey string `json:"reaction_key"`
	// BridgeMXC is the mxc:// URI the Discord bridge knows the emoji by, the
	// key its reactions carry in the reaction log. Empty until the bridge has
	// seen the emoji or multichat has taught it; reacting with it teaches it.
	BridgeMXC string `json:"bridge_mxc"`
}

// DiscordServerEmojiRefresh is when one bridged server's emoji were last fetched.
type DiscordServerEmojiRefresh struct {
	DiscordServerID   string    `json:"discord_server_id"`
	DiscordServerName string    `json:"discord_server_name"`
	RefreshedAt       time.Time `json:"refreshed_at"`
}

// SkinTone is a Fitzpatrick modifier applied to unicode emoji that take one;
// "" leaves them yellow.
type SkinTone string

// The skin tones, as the Unicode modifier each one appends.
var SkinTones = []SkinTone{"", "🏻", "🏼", "🏽", "🏾", "🏿"}

// EmojiSettings are the operator's emoji settings.
type EmojiSettings struct {
	SkinTone SkinTone `json:"skin_tone"`
	// QuickReactionCount is how many favourites the quick-react bar under a
	// message shows, 0 to MaxQuickReactionCount.
	QuickReactionCount int `json:"quick_reaction_count"`
	// OfferOtherServersEmoji offers a Discord room the custom emoji of every
	// bridged server, not only its own. Discord refuses another server's
	// emoji to an account without Nitro.
	OfferOtherServersEmoji bool `json:"offer_other_servers_emoji"`
}

// MaxQuickReactionCount bounds EmojiSettings.QuickReactionCount.
const MaxQuickReactionCount = 12

// EmojiCatalog is everything a picker needs: every bridged server's custom
// emoji, when each server was last refreshed, and the operator's favourites
// and settings.
type EmojiCatalog struct {
	DiscordCustomEmoji []DiscordCustomEmoji        `json:"discord_custom_emoji"`
	DiscordRefreshes   []DiscordServerEmojiRefresh `json:"discord_refreshes"`
	// FavoriteKeys are unicode emoji and "discord-emoji:<id>" keys, in order.
	FavoriteKeys []string      `json:"favorite_keys"`
	Settings     EmojiSettings `json:"settings"`
}

// ReplaceDiscordServerEmoji replaces one server's catalog rows with emoji, in
// one transaction, and records the refresh.
func ReplaceDiscordServerEmoji(database *sql.DB, serverID, serverName string, emoji []DiscordCustomEmoji, fetchedAt time.Time) error {
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM discord_custom_emoji WHERE discord_server_id = ?`, serverID); err != nil {
		return fmt.Errorf("clear emoji of server %s: %w", serverID, err)
	}
	for _, e := range emoji {
		if _, err := tx.Exec(`INSERT INTO discord_custom_emoji (discord_emoji_id, name, animated, discord_server_id, discord_server_name, fetched_at)
			VALUES (?, ?, ?, ?, ?, ?)`, e.DiscordEmojiID, e.Name, e.Animated, serverID, serverName, fetchedAt.UTC()); err != nil {
			return fmt.Errorf("store emoji %s of server %s: %w", e.DiscordEmojiID, serverID, err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO discord_custom_emoji_refreshes (discord_server_id, discord_server_name, refreshed_at) VALUES (?, ?, ?)
		ON CONFLICT(discord_server_id) DO UPDATE SET discord_server_name = excluded.discord_server_name, refreshed_at = excluded.refreshed_at`,
		serverID, serverName, fetchedAt.UTC()); err != nil {
		return fmt.Errorf("record refresh of server %s: %w", serverID, err)
	}
	return tx.Commit()
}

// DiscordCustomEmojiList returns the catalog, by server then name. BridgeMXC
// is left empty: the bridge's database holds it.
func DiscordCustomEmojiList(database *sql.DB) ([]DiscordCustomEmoji, error) {
	rows, err := database.Query(`SELECT discord_emoji_id, name, animated, discord_server_id, discord_server_name, fetched_at
		FROM discord_custom_emoji ORDER BY discord_server_name, discord_server_id, name COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("list custom emoji: %w", err)
	}
	defer rows.Close()
	out := []DiscordCustomEmoji{}
	for rows.Next() {
		var e DiscordCustomEmoji
		if err := rows.Scan(&e.DiscordEmojiID, &e.Name, &e.Animated, &e.DiscordServerID, &e.DiscordServerName, &e.FetchedAt); err != nil {
			return nil, fmt.Errorf("scan custom emoji: %w", err)
		}
		e.ReactionKey = DiscordEmojiReactionKeyPrefix + e.DiscordEmojiID
		out = append(out, e)
	}
	return out, rows.Err()
}

// DiscordCustomEmojiByID returns one catalog emoji, or ErrNotFound.
func DiscordCustomEmojiByID(database *sql.DB, discordEmojiID string) (DiscordCustomEmoji, error) {
	var e DiscordCustomEmoji
	err := database.QueryRow(`SELECT discord_emoji_id, name, animated, discord_server_id, discord_server_name, fetched_at
		FROM discord_custom_emoji WHERE discord_emoji_id = ?`, discordEmojiID).
		Scan(&e.DiscordEmojiID, &e.Name, &e.Animated, &e.DiscordServerID, &e.DiscordServerName, &e.FetchedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return DiscordCustomEmoji{}, ErrNotFound
	}
	if err != nil {
		return DiscordCustomEmoji{}, fmt.Errorf("custom emoji %s: %w", discordEmojiID, err)
	}
	e.ReactionKey = DiscordEmojiReactionKeyPrefix + e.DiscordEmojiID
	return e, nil
}

// DiscordEmojiRefreshes returns when each server was last refreshed.
func DiscordEmojiRefreshes(database *sql.DB) ([]DiscordServerEmojiRefresh, error) {
	rows, err := database.Query(`SELECT discord_server_id, discord_server_name, refreshed_at FROM discord_custom_emoji_refreshes ORDER BY discord_server_name`)
	if err != nil {
		return nil, fmt.Errorf("list emoji refreshes: %w", err)
	}
	defer rows.Close()
	out := []DiscordServerEmojiRefresh{}
	for rows.Next() {
		var r DiscordServerEmojiRefresh
		if err := rows.Scan(&r.DiscordServerID, &r.DiscordServerName, &r.RefreshedAt); err != nil {
			return nil, fmt.Errorf("scan emoji refresh: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// EmojiFavoriteKeys returns the favourites in order.
func EmojiFavoriteKeys(database *sql.DB) ([]string, error) {
	rows, err := database.Query(`SELECT emoji_key FROM emoji_favorites ORDER BY position`)
	if err != nil {
		return nil, fmt.Errorf("list favourite emoji: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("scan favourite emoji: %w", err)
		}
		out = append(out, key)
	}
	return out, rows.Err()
}

// ErrInvalidEmojiKey is returned for a favourite that is neither a unicode
// emoji nor a custom emoji in the catalog.
var ErrInvalidEmojiKey = errors.New("invalid emoji key")

// ReplaceEmojiFavorites stores keys as the favourites, in that order. Each is
// a unicode emoji (short text, no spaces) or "discord-emoji:<id>" of an emoji
// in the catalog; a repeated key is refused rather than dropped.
func ReplaceEmojiFavorites(database *sql.DB, keys []string) error {
	seen := map[string]bool{}
	for _, key := range keys {
		if seen[key] {
			return fmt.Errorf("%w: %q is listed twice", ErrInvalidEmojiKey, key)
		}
		seen[key] = true
		if id, custom := strings.CutPrefix(key, DiscordEmojiReactionKeyPrefix); custom {
			if _, err := DiscordCustomEmojiByID(database, id); errors.Is(err, ErrNotFound) {
				return fmt.Errorf("%w: %q is not in the custom emoji catalog", ErrInvalidEmojiKey, key)
			} else if err != nil {
				return err
			}
			continue
		}
		if key == "" || len(key) > 64 || strings.ContainsAny(key, " \t\n:") {
			return fmt.Errorf("%w: %q is not a unicode emoji or a discord-emoji:<id> key", ErrInvalidEmojiKey, key)
		}
	}
	tx, err := database.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM emoji_favorites`); err != nil {
		return fmt.Errorf("clear favourite emoji: %w", err)
	}
	for position, key := range keys {
		if _, err := tx.Exec(`INSERT INTO emoji_favorites (emoji_key, position) VALUES (?, ?)`, key, position); err != nil {
			return fmt.Errorf("store favourite emoji %q: %w", key, err)
		}
	}
	return tx.Commit()
}

// DefaultEmojiSettings are the settings before the operator changes any.
var DefaultEmojiSettings = EmojiSettings{SkinTone: "", QuickReactionCount: 5, OfferOtherServersEmoji: false}

// ReadEmojiSettings returns the settings, DefaultEmojiSettings until they are first saved.
func ReadEmojiSettings(database *sql.DB) (EmojiSettings, error) {
	var s EmojiSettings
	err := database.QueryRow(`SELECT skin_tone, quick_reaction_count, offer_other_servers_emoji FROM emoji_settings WHERE id = 1`).
		Scan(&s.SkinTone, &s.QuickReactionCount, &s.OfferOtherServersEmoji)
	if errors.Is(err, sql.ErrNoRows) {
		return DefaultEmojiSettings, nil
	}
	if err != nil {
		return EmojiSettings{}, fmt.Errorf("emoji settings: %w", err)
	}
	return s, nil
}

// ErrInvalidEmojiSettings is returned for a setting out of range.
var ErrInvalidEmojiSettings = errors.New("invalid emoji settings")

// SaveEmojiSettings validates and stores the settings.
func SaveEmojiSettings(database *sql.DB, s EmojiSettings) error {
	knownTone := false
	for _, tone := range SkinTones {
		knownTone = knownTone || tone == s.SkinTone
	}
	if !knownTone {
		return fmt.Errorf("%w: skin_tone %q is not \"\" or one of the five Fitzpatrick modifiers U+1F3FB–U+1F3FF", ErrInvalidEmojiSettings, s.SkinTone)
	}
	if s.QuickReactionCount < 0 || s.QuickReactionCount > MaxQuickReactionCount {
		return fmt.Errorf("%w: quick_reaction_count %d is not between 0 and %d", ErrInvalidEmojiSettings, s.QuickReactionCount, MaxQuickReactionCount)
	}
	_, err := database.Exec(`INSERT INTO emoji_settings (id, skin_tone, quick_reaction_count, offer_other_servers_emoji) VALUES (1, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET skin_tone = excluded.skin_tone, quick_reaction_count = excluded.quick_reaction_count,
		offer_other_servers_emoji = excluded.offer_other_servers_emoji`, s.SkinTone, s.QuickReactionCount, s.OfferOtherServersEmoji)
	if err != nil {
		return fmt.Errorf("save emoji settings: %w", err)
	}
	return nil
}
