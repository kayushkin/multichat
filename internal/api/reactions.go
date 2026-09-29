package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/discordemoji"
	"github.com/kayushkin/multichat/internal/matrix"
)

// ownSenders names our own accounts for reaction groups: the Matrix account
// multichat acts as, and our puppets on each platform. Without a Matrix
// client only the puppets are known.
func (r *Router) ownSenders() (db.OwnSenders, error) {
	own := db.OwnSenders{PuppetUserIDs: matrix.SelfUserIDs}
	if r.matrix == nil {
		return own, nil
	}
	self, err := r.matrix.WhoAmI()
	if err != nil {
		return db.OwnSenders{}, err
	}
	own.MatrixUserID = self
	return own, nil
}

// postReactionHandler serves POST /conversations/{room_id}/reactions
// {"event_id": message, "key": emoji}: react to a message as our Matrix
// account. The bridge relays it to the platform as our own account, so the
// people in the room see it. The key is a unicode emoji, the mxc:// URI of a
// custom emoji the bridge knows, or "discord-emoji:<id>" for any emoji in the
// custom emoji catalog (GET /emoji): multichat teaches the bridge that emoji
// if it has not seen it, then reacts with its mxc:// URI, and answers the key
// it used. The reaction is logged at once so the next read shows it; the sync
// worker's copy of the same event is then a no-op.
func (r *Router) postReactionHandler(w http.ResponseWriter, req *http.Request, roomID string) {
	if req.Method != "POST" {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	if r.matrix == nil {
		r.writeError(w, 503, "Matrix client not configured")
		return
	}
	var body struct {
		EventID string `json:"event_id"`
		Key     string `json:"key"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		r.writeError(w, 400, "body must be JSON {\"event_id\":…,\"key\":…}: "+err.Error())
		return
	}
	if body.EventID == "" {
		r.writeError(w, 400, "event_id is required")
		return
	}
	if body.Key == "" {
		r.writeError(w, 400, "key is required")
		return
	}
	shortcode := ""
	if discordEmojiID, custom := strings.CutPrefix(body.Key, db.DiscordEmojiReactionKeyPrefix); custom {
		emoji, err := db.DiscordCustomEmojiByID(r.db, discordEmojiID)
		if errors.Is(err, db.ErrNotFound) {
			r.writeError(w, 400, "key "+body.Key+" is not in the custom emoji catalog; POST /emoji/discord/refresh fetches it")
			return
		}
		if err != nil {
			r.writeError(w, 500, err.Error())
			return
		}
		if r.discordDBPath == "" {
			r.writeError(w, 503, "DISCORD_DB is not set, so the Discord bridge cannot be taught emoji "+emoji.Name)
			return
		}
		bridge, err := discordemoji.OpenBridgeDatabaseForWriting(r.discordDBPath)
		if err != nil {
			r.writeError(w, 500, err.Error())
			return
		}
		teacher := r.discordEmojiTeacher
		teacher.Uploader = r.matrix
		mxc, err := teacher.Teach(bridge, emoji)
		bridge.Close()
		if err != nil {
			r.writeError(w, 502, err.Error())
			return
		}
		body.Key = mxc
		shortcode = ":" + emoji.Name + ":"
	}
	self, err := r.matrix.WhoAmI()
	if err != nil {
		r.writeError(w, 502, err.Error())
		return
	}
	reactionEventID, err := r.matrix.SendReaction(roomID, body.EventID, body.Key)
	if err != nil {
		r.writeError(w, 502, err.Error())
		return
	}
	displayName, err := r.matrix.DisplayNameOf(self)
	if err != nil {
		r.writeError(w, 502, "reaction "+reactionEventID+" was sent but could not be logged: "+err.Error())
		return
	}
	content, err := json.Marshal(map[string]any{
		"m.relates_to": map[string]string{"rel_type": "m.annotation", "event_id": body.EventID, "key": body.Key},
	})
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	if err := db.LogReaction(r.db, db.LoggedReaction{
		EventID:           reactionEventID,
		TargetEventID:     body.EventID,
		RoomID:            roomID,
		SenderUserID:      self,
		SenderDisplayName: displayName,
		Key:               body.Key,
		Shortcode:         shortcode,
		ContentJSON:       string(content),
		SentAt:            time.Now(),
	}); err != nil {
		r.writeError(w, 500, "reaction "+reactionEventID+" was sent but could not be logged: "+err.Error())
		return
	}
	r.writeJSON(w, map[string]string{"event_id": reactionEventID, "key": body.Key})
}

// takeBackReactionHandler serves DELETE /conversations/{room_id}/reactions/{event_id}:
// take back a reaction our Matrix account posted, by redacting it. A reaction
// someone else made — including our own puppet, from the platform's own app —
// is a 403: multichat cannot redact another account's event.
func (r *Router) takeBackReactionHandler(w http.ResponseWriter, req *http.Request, roomID, reactionEventID string) {
	if req.Method != "DELETE" {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	if r.matrix == nil {
		r.writeError(w, 503, "Matrix client not configured")
		return
	}
	reaction, err := db.LoggedReactionByEventID(r.db, reactionEventID)
	if errors.Is(err, db.ErrNotFound) {
		r.writeError(w, 404, "no reaction "+reactionEventID+" in the reaction log")
		return
	}
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	if reaction.RoomID != roomID {
		r.writeError(w, 404, "reaction "+reactionEventID+" is not in room "+roomID)
		return
	}
	if reaction.RedactedAt != nil {
		r.writeError(w, 409, "reaction "+reactionEventID+" was already taken back")
		return
	}
	self, err := r.matrix.WhoAmI()
	if err != nil {
		r.writeError(w, 502, err.Error())
		return
	}
	if reaction.SenderUserID != self {
		r.writeError(w, 403, "reaction "+reactionEventID+" was made by "+reaction.SenderUserID+", not by "+self)
		return
	}
	redactionEventID, err := r.matrix.Redact(roomID, reactionEventID)
	if err != nil {
		r.writeError(w, 502, err.Error())
		return
	}
	if _, err := db.MarkReactionRedacted(r.db, reactionEventID, redactionEventID, self, time.Now()); err != nil {
		r.writeError(w, 500, "reaction was taken back but the log could not be updated: "+err.Error())
		return
	}
	r.writeJSON(w, map[string]string{"redaction_event_id": redactionEventID})
}
