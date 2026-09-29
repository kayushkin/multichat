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
	for i := range custom {
		custom[i].BridgeMXC = known[custom[i].DiscordEmojiID]
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
	return db.EmojiCatalog{DiscordCustomEmoji: custom, DiscordRefreshes: refreshes, FavoriteKeys: favorites, Settings: settings}, nil
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
	if err := db.ReplaceEmojiFavorites(r.db, body.FavoriteKeys); err != nil {
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
		SkinTone               *db.SkinTone `json:"skin_tone"`
		QuickReactionCount     *int         `json:"quick_reaction_count"`
		OfferOtherServersEmoji *bool        `json:"offer_other_servers_emoji"`
	}
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		r.writeError(w, 400, "body must be JSON {\"skin_tone\":…,\"quick_reaction_count\":…,\"offer_other_servers_emoji\":…}: "+err.Error())
		return
	}
	if body.SkinTone == nil || body.QuickReactionCount == nil || body.OfferOtherServersEmoji == nil {
		r.writeError(w, 400, "skin_tone, quick_reaction_count and offer_other_servers_emoji are all required")
		return
	}
	settings := db.EmojiSettings{SkinTone: *body.SkinTone, QuickReactionCount: *body.QuickReactionCount, OfferOtherServersEmoji: *body.OfferOtherServersEmoji}
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
