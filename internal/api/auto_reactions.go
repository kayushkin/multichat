package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/kayushkin/multichat/internal/db"
)

// Auto-reactions: our own account reacts with an emoji to every message one
// person sends (internal/inbound/auto_reactor.go). The reaction goes out as
// the operator's own account on the platform.
//
//	GET    /auto-reactions        list, each with the sender's and room's names
//	POST   /auto-reactions        {sender_user_id, room_id, reaction_key, enabled?}
//	PATCH  /auto-reactions/{id}   {reaction_key?, enabled?}
//	DELETE /auto-reactions/{id}

// maxReactionKeyBytes bounds reaction_key: the longest unicode emoji is under
// 40 bytes, and an mxc:// URI of a custom emoji well under this.
const maxReactionKeyBytes = 255

func checkAutoReactionKey(key string) error {
	if key == "" {
		return errors.New("reaction_key is required")
	}
	if strings.HasPrefix(key, db.DiscordEmojiReactionKeyPrefix) {
		return fmt.Errorf("reaction_key %q: a catalog custom emoji is not supported here yet; use a unicode emoji", key)
	}
	if !utf8.ValidString(key) || len(key) > maxReactionKeyBytes || strings.ContainsAny(key, " \t\n") {
		return fmt.Errorf("reaction_key %q is not an emoji", key)
	}
	return nil
}

// withDisplayNames fills the display-only names from Matrix.
func (r *Router) withDisplayNames(rule db.AutoReaction) (db.AutoReaction, error) {
	if r.matrix == nil {
		return rule, nil
	}
	name, err := r.matrix.DisplayNameOf(rule.SenderUserID)
	if err != nil {
		return rule, fmt.Errorf("display name of %s: %w", rule.SenderUserID, err)
	}
	rule.SenderDisplayName = name
	if rule.RoomID != "" {
		roomName, err := r.matrix.RoomName(rule.RoomID)
		if err != nil {
			return rule, fmt.Errorf("name of room %s: %w", rule.RoomID, err)
		}
		rule.RoomName = roomName
	}
	return rule, nil
}

func (r *Router) writeAutoReaction(w http.ResponseWriter, status int, rule db.AutoReaction) {
	named, err := r.withDisplayNames(rule)
	if err != nil {
		r.writeError(w, 502, err.Error())
		return
	}
	w.WriteHeader(status)
	r.writeJSON(w, named)
}

func (r *Router) autoReactionsHandler(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case "GET":
		rules, err := db.ListAutoReactions(r.db)
		if err != nil {
			r.writeError(w, 500, err.Error())
			return
		}
		for i := range rules {
			if rules[i], err = r.withDisplayNames(rules[i]); err != nil {
				r.writeError(w, 502, err.Error())
				return
			}
		}
		r.writeJSON(w, map[string]any{"auto_reactions": rules})
	case "POST":
		var body struct {
			SenderUserID string `json:"sender_user_id"`
			RoomID       string `json:"room_id"`
			ReactionKey  string `json:"reaction_key"`
			Enabled      *bool  `json:"enabled"`
		}
		if err := decodeStrict(req, &body); err != nil {
			r.writeError(w, 400, err.Error())
			return
		}
		if !strings.HasPrefix(body.SenderUserID, "@") || !strings.Contains(body.SenderUserID, ":") {
			r.writeError(w, 400, fmt.Sprintf("sender_user_id %q is not a Matrix user id (expected @platform_id:server)", body.SenderUserID))
			return
		}
		if body.RoomID != "" && (!strings.HasPrefix(body.RoomID, "!") || !strings.Contains(body.RoomID, ":")) {
			r.writeError(w, 400, fmt.Sprintf("room_id %q is not a Matrix room id (expected !id:server, or \"\" for every room)", body.RoomID))
			return
		}
		if err := checkAutoReactionKey(body.ReactionKey); err != nil {
			r.writeError(w, 400, err.Error())
			return
		}
		rule, err := db.CreateAutoReaction(r.db, body.SenderUserID, body.RoomID, body.ReactionKey, body.Enabled == nil || *body.Enabled)
		if errors.Is(err, db.ErrAutoReactionExists) {
			r.writeError(w, 409, err.Error())
			return
		}
		if err != nil {
			r.writeError(w, 500, err.Error())
			return
		}
		r.writeAutoReaction(w, 201, rule)
	default:
		r.writeError(w, 405, "Method not allowed")
	}
}

func (r *Router) autoReactionHandler(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.ParseInt(strings.TrimPrefix(req.URL.Path, "/auto-reactions/"), 10, 64)
	if err != nil {
		r.writeError(w, 400, "rule id must be an integer")
		return
	}
	switch req.Method {
	case "PATCH":
		var change db.AutoReactionChange
		if err := decodeStrict(req, &change); err != nil {
			r.writeError(w, 400, err.Error())
			return
		}
		if change.ReactionKey != nil {
			if err := checkAutoReactionKey(*change.ReactionKey); err != nil {
				r.writeError(w, 400, err.Error())
				return
			}
		}
		rule, err := db.UpdateAutoReaction(r.db, id, change)
		if errors.Is(err, db.ErrAutoReactionExists) {
			r.writeError(w, 409, err.Error())
			return
		}
		if r.writeAutoReactionLookupError(w, err) {
			return
		}
		r.writeAutoReaction(w, 200, rule)
	case "DELETE":
		if r.writeAutoReactionLookupError(w, db.DeleteAutoReaction(r.db, id)) {
			return
		}
		w.WriteHeader(204)
	default:
		r.writeError(w, 405, "Method not allowed")
	}
}

func (r *Router) writeAutoReactionLookupError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, db.ErrNotFound) {
		r.writeError(w, 404, "no such auto-reaction")
	} else {
		r.writeError(w, 500, err.Error())
	}
	return true
}
