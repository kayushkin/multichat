package api

import (
	"encoding/json"
	"net/http"
)

// sendDirectMessageHandler — POST /messages/send {"user_id":…,"body":…}.
//
// Sends body to one person, identified by their bridge puppet id, in the
// direct-message room we already share with them; a room is created only
// when none exists. This is the route for anything that wants to "message X
// on WhatsApp" without knowing room ids: it is what /conversations/new never
// was — that one creates a fresh room on every call.
func (r *Router) sendDirectMessageHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	if r.matrix == nil {
		r.writeError(w, 503, "Matrix client not configured")
		return
	}
	var body struct {
		UserID string `json:"user_id"`
		Body   string `json:"body"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		r.writeError(w, 400, "invalid JSON: "+err.Error())
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
	roomID, err := r.matrix.FindDirectMessageRoom(body.UserID)
	if err != nil {
		r.writeError(w, 502, "find direct message room: "+err.Error())
		return
	}
	createdRoom := false
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
