package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/discordbridge"
	"github.com/kayushkin/multichat/internal/discordemoji"
)

// emojiCatalog reads the whole catalog, with each custom emoji's mxc:// URI
// from the bridge's database when the bridge knows it.
func (r *Router) emojiCatalog() (db.EmojiCatalog, error) {
	custom, err := db.DiscordCustomEmojiList(r.db)
	if err != nil {
		return db.EmojiCatalog{}, err
	}
	bridge, err := discordbridge.Open(r.discordDBPath)
	if err != nil {
		return db.EmojiCatalog{}, err
	}
	defer bridge.Close()
	known, err := discordemoji.BridgeEmojiMXCs(bridge)
	if err != nil {
		return db.EmojiCatalog{}, err
	}
	inCatalog := map[string]bool{}
	for i := range custom {
		custom[i].BridgeMXC = known[custom[i].DiscordEmojiID]
		inCatalog[custom[i].DiscordEmojiID] = true
	}
	seen, err := discordemoji.BridgeSeenEmoji(bridge)
	if err != nil {
		return db.EmojiCatalog{}, err
	}
	seenElsewhere := []db.DiscordCustomEmoji{}
	for _, emoji := range seen {
		if !inCatalog[emoji.DiscordEmojiID] {
			seenElsewhere = append(seenElsewhere, emoji)
		}
	}
	nitro, nitroError, err := db.DiscordNitroStatus(r.db)
	if err != nil {
		return db.EmojiCatalog{}, err
	}
	refreshes, err := db.DiscordEmojiRefreshes(r.db)
	if err != nil {
		return db.EmojiCatalog{}, err
	}
	favorites, err := db.EmojiFavoriteKeys(r.db)
	if err != nil {
		return db.EmojiCatalog{}, err
	}
	settings, err := db.ReadEmojiSettings(r.db)
	if err != nil {
		return db.EmojiCatalog{}, err
	}
	return db.EmojiCatalog{DiscordCustomEmoji: custom, DiscordSeenEmoji: seenElsewhere, DiscordRefreshes: refreshes,
		DiscordNitro: nitro, DiscordNitroError: nitroError, DiscordServerSweep: r.discordServerSweeper.Status(),
		FavoriteKeys: favorites, Settings: settings}, nil
}

// emojiHandler serves GET /emoji: every bridged Discord server's custom emoji
// as last fetched, and the operator's favourite emoji and emoji settings.
// Nothing here calls Discord; POST /emoji/discord/refresh does.
func (r *Router) emojiHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "GET" {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	if r.discordDBPath == "" {
		r.writeError(w, 503, "DISCORD_DB is not set, so the Discord bridge's emoji cannot be read")
		return
	}
	catalog, err := r.emojiCatalog()
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	r.writeJSON(w, catalog)
}

// refreshDiscordEmojiHandler serves POST /emoji/discord/refresh: list each
// bridged server's custom emoji from Discord with the bot token and replace
// the catalog, then answer the catalog as GET /emoji does.
func (r *Router) refreshDiscordEmojiHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	if r.discordDBPath == "" {
		r.writeError(w, 503, "DISCORD_DB is not set, so the bridged Discord servers cannot be listed")
		return
	}
	bridge, err := discordbridge.Open(r.discordDBPath)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	defer bridge.Close()
	if err := r.discordEmojiRefresher.Refresh(r.db, bridge, time.Now()); err != nil {
		if errors.Is(err, discordemoji.ErrNotConfigured) {
			r.writeError(w, 503, err.Error())
			return
		}
		r.writeError(w, 502, err.Error())
		return
	}
	catalog, err := r.emojiCatalog()
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	r.writeJSON(w, catalog)
}

// emojiFavoritesHandler serves PUT /emoji/favorites {"favorite_keys": [...]}:
// replace the favourites with these, in this order.
func (r *Router) emojiFavoritesHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "PUT" {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	var body struct {
		FavoriteKeys []string `json:"favorite_keys"`
	}
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		r.writeError(w, 400, "body must be JSON {\"favorite_keys\":[…]}: "+err.Error())
		return
	}
	if body.FavoriteKeys == nil {
		r.writeError(w, 400, "favorite_keys is required; send [] for none")
		return
	}
	seenIDs := map[string]bool{}
	if r.discordDBPath != "" {
		bridge, err := discordbridge.Open(r.discordDBPath)
		if err != nil {
			r.writeError(w, 500, err.Error())
			return
		}
		seen, err := discordemoji.BridgeSeenEmoji(bridge)
		bridge.Close()
		if err != nil {
			r.writeError(w, 500, err.Error())
			return
		}
		for _, emoji := range seen {
			seenIDs[emoji.DiscordEmojiID] = true
		}
	}
	if err := db.ReplaceEmojiFavorites(r.db, body.FavoriteKeys, seenIDs); err != nil {
		if errors.Is(err, db.ErrInvalidEmojiKey) {
			r.writeError(w, 400, err.Error())
			return
		}
		r.writeError(w, 500, err.Error())
		return
	}
	keys, err := db.EmojiFavoriteKeys(r.db)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	r.writeJSON(w, map[string][]string{"favorite_keys": keys})
}

// emojiSettingsHandler serves PUT /emoji/settings with every field of
// db.EmojiSettings; a missing or unknown field is a 400.
func (r *Router) emojiSettingsHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "PUT" {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	var body struct {
		SkinTone           *db.SkinTone `json:"skin_tone"`
		QuickReactionCount *int         `json:"quick_reaction_count"`
	}
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		r.writeError(w, 400, "body must be JSON {\"skin_tone\":…,\"quick_reaction_count\":…}: "+err.Error())
		return
	}
	if body.SkinTone == nil || body.QuickReactionCount == nil {
		r.writeError(w, 400, "skin_tone and quick_reaction_count are both required")
		return
	}
	settings := db.EmojiSettings{SkinTone: *body.SkinTone, QuickReactionCount: *body.QuickReactionCount}
	if err := db.SaveEmojiSettings(r.db, settings); err != nil {
		if errors.Is(err, db.ErrInvalidEmojiSettings) {
			r.writeError(w, 400, err.Error())
			return
		}
		r.writeError(w, 500, err.Error())
		return
	}
	r.writeJSON(w, settings)
}

// sameLocalDay reports whether two times fall on the same calendar day in
// the host's time zone.
func sameLocalDay(a, b time.Time) bool {
	ay, am, ad := a.Local().Date()
	by, bm, bd := b.Local().Date()
	return ay == by && am == bm && ad == bd
}

// discordNitroCheckHandler serves POST /emoji/discord/nitro-check: ask
// Discord whether the bridge's account has Nitro, at most once a calendar day
// (the host's), unless ?force=true. The Messages pages call it when they
// open, so no check is made on a day nobody reads messages. A failed check is
// recorded too, so a failing login is not retried all day. The day's first
// check also starts the sweep that lists every other server's emoji (see
// discordemoji.ServerSweeper). Answers {"discord_nitro",
// "discord_nitro_error", "checked_now", "discord_server_sweep"}.
func (r *Router) discordNitroCheckHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	if r.discordDBPath == "" {
		r.writeError(w, 503, "DISCORD_DB is not set, so the Discord account cannot be read")
		return
	}
	now := time.Now()
	last, err := db.LastDiscordNitroCheckAt(r.db)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	checkedNow := false
	if last.IsZero() || !sameLocalDay(last, now) || req.URL.Query().Get("force") == "true" {
		bridge, err := discordbridge.Open(r.discordDBPath)
		if err != nil {
			r.writeError(w, 500, err.Error())
			return
		}
		discordUserID, premiumType, checkErr := r.discordNitroChecker.Check(bridge)
		bridge.Close()
		if checkErr != nil {
			err = db.RecordDiscordNitroCheck(r.db, now, "", nil, checkErr.Error())
		} else {
			err = db.RecordDiscordNitroCheck(r.db, now, discordUserID, &premiumType, "")
		}
		if err != nil {
			r.writeError(w, 500, err.Error())
			return
		}
		checkedNow = true
		if req.URL.Query().Get("force") != "true" {
			r.discordServerSweeper.Start(r.db, r.discordDBPath)
		}
	}
	nitro, nitroError, err := db.DiscordNitroStatus(r.db)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	r.writeJSON(w, map[string]any{"discord_nitro": nitro, "discord_nitro_error": nitroError, "checked_now": checkedNow,
		"discord_server_sweep": r.discordServerSweeper.Status()})
}

// discordServerSweepHandler serves POST /emoji/discord/server-sweep: start
// the sweep that lists every other server's emoji now, rather than with the
// next day's first Nitro check (the Emoji page's button). It keeps the same
// pace and lists only servers not listed in the past week; a running sweep is
// left alone. Answers {"started", "discord_server_sweep"}.
func (r *Router) discordServerSweepHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	if r.discordDBPath == "" {
		r.writeError(w, 503, "DISCORD_DB is not set, so the Discord account cannot be read")
		return
	}
	started := r.discordServerSweeper.Start(r.db, r.discordDBPath)
	r.writeJSON(w, map[string]any{"started": started, "discord_server_sweep": r.discordServerSweeper.Status()})
}
