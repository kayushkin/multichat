package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/kayushkin/multichat/internal/botaccounts"
)

// sendDirectMessageHandler — POST /messages/send {"user_id":…,"body":…},
// or {"account_id":…,"user_id" or "channel_id":…,"body":…} to send as one of
// our bot accounts (sendAsBot).
//
// Sends body to one person, identified by their bridge puppet id, in the
// direct-message room we already share with them; a room is created only
// when none exists. A Discord person's room is the one the Discord bridge
// records for the DM, opened through the bridge bot when it has none
// (discordDirectMessageRoom); a Discord DM the bridge does not know is a 409. This is the route for anything that wants to "message X
// on WhatsApp" without knowing room ids: it is what /conversations/new never
// was — that one creates a fresh room on every call.
func (r *Router) sendDirectMessageHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	var body struct {
		AccountID string `json:"account_id"`
		UserID    string `json:"user_id"`
		ChannelID string `json:"channel_id"`
		Body      string `json:"body"`
	}
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		r.writeError(w, 400, "invalid JSON: "+err.Error())
		return
	}
	if body.AccountID != "" {
		r.sendAsBot(w, body.AccountID, body.UserID, body.ChannelID, body.Body)
		return
	}
	if body.ChannelID != "" {
		r.writeError(w, 400, "channel_id goes with account_id: it names a bot's channel; the operator's own account sends by user_id")
		return
	}
	if r.matrix == nil {
		r.writeError(w, 503, "Matrix client not configured")
		return
	}
	if body.UserID == "" {
		r.writeError(w, 400, "user_id is required (a bridge puppet id such as @whatsapp_15551234567:chat.kayushkin.com)")
		return
	}
	if body.Body == "" {
		r.writeError(w, 400, "body is required")
		return
	}
	var roomID string
	var err error
	createdRoom := false
	if isDiscordPuppet(body.UserID) && r.discordDBPath != "" {
		roomID, createdRoom, err = r.discordDirectMessageRoom(body.UserID)
		if errors.Is(err, errDiscordDirectMessageHasNoRoom) {
			r.writeError(w, 409, err.Error())
			return
		}
		if err != nil {
			r.writeError(w, 502, "find the Discord direct message room: "+err.Error())
			return
		}
	} else {
		roomID, err = r.matrix.FindDirectMessageRoom(body.UserID)
		if err != nil {
			r.writeError(w, 502, "find direct message room: "+err.Error())
			return
		}
	}
	if roomID == "" {
		roomID, err = r.matrix.CreateDM(body.UserID)
		if err != nil {
			r.writeError(w, 502, "create direct message room: "+err.Error())
			return
		}
		createdRoom = true
	}
	eventID, err := r.matrix.SendMessage(roomID, body.Body)
	if err != nil {
		r.writeError(w, 502, "send: "+err.Error())
		return
	}
	r.writeJSON(w, map[string]any{
		"room_id":      roomID,
		"event_id":     eventID,
		"created_room": createdRoom,
	})
}

// sendAsBot sends body as one of our bot accounts, to the direct message with
// a platform user id or to a channel id, and answers the platform's message
// ids (more than one when the body is longer than one message).
func (r *Router) sendAsBot(w http.ResponseWriter, accountID, userID, channelID, body string) {
	if r.bots == nil {
		r.writeError(w, 503, "multichat holds no bot accounts: MESSAGE_STORE_URL is not set")
		return
	}
	if (userID == "") == (channelID == "") {
		r.writeError(w, 400, "a send as a bot names exactly one of user_id (the person's platform user id, such as a Discord user id) and channel_id")
		return
	}
	if body == "" {
		r.writeError(w, 400, "body is required")
		return
	}
	result, err := r.bots.Send(accountID, userID, channelID, body)
	if errors.Is(err, botaccounts.ErrUnknownAccount) {
		r.writeError(w, 404, err.Error()+"; GET /accounts lists them")
		return
	}
	if errors.Is(err, botaccounts.ErrNotConnected) {
		r.writeError(w, 503, err.Error())
		return
	}
	if err != nil {
		if len(result.MessageIDs) > 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(502)
			json.NewEncoder(w).Encode(map[string]any{"error": err.Error(), "account_id": result.AccountID, "channel_id": result.ChannelID, "message_ids": result.MessageIDs})
			return
		}
		r.writeError(w, 502, err.Error())
		return
	}
	r.writeJSON(w, result)
}
