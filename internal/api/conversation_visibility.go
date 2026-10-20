package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/matrix"
)

// markHiddenConversations fills each conversation's Visibility and Hidden
// from the operator's choices and the auto-hide rule.
func (r *Router) markHiddenConversations(conversations []matrix.Conversation) error {
	visibilityByRoomID, err := db.ConversationVisibilityMap(r.db)
	if err != nil {
		return err
	}
	settings, err := db.ReadConversationVisibilitySettings(r.db)
	if err != nil {
		return err
	}
	for index := range conversations {
		visibility, chosen := visibilityByRoomID[conversations[index].RoomID]
		if !chosen {
			visibility = db.ConversationVisibilityAuto
		}
		conversations[index].Visibility = string(visibility)
		conversations[index].Hidden = db.ConversationIsHidden(visibility, conversations[index].MemberCount, settings)
	}
	return nil
}

// conversationVisibilityHandler serves PUT /conversations/{room_id}/visibility
// {"visibility": "hidden" | "shown" | "auto"} and answers the room's choice.
func (r *Router) conversationVisibilityHandler(w http.ResponseWriter, req *http.Request, roomID string) {
	if req.Method != "PUT" {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	var body struct {
		Visibility db.ConversationVisibility `json:"visibility"`
	}
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		r.writeError(w, 400, "body must be JSON {\"visibility\":…}: "+err.Error())
		return
	}
	if err := db.SetConversationVisibility(r.db, roomID, body.Visibility); err != nil {
		if errors.Is(err, db.ErrInvalidRoomID) || errors.Is(err, db.ErrInvalidConversationVisibility) {
			r.writeError(w, 400, err.Error())
			return
		}
		r.writeError(w, 500, err.Error())
		return
	}
	r.writeJSON(w, map[string]any{"room_id": roomID, "visibility": body.Visibility})
}

// conversationVisibilitySettingsHandler serves GET and PUT
// /conversations/visibility-settings, the auto-hide rule. PUT takes both
// fields.
func (r *Router) conversationVisibilitySettingsHandler(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case "GET":
	case "PUT":
		var body struct {
			AutoHideEnabled              *bool `json:"auto_hide_enabled"`
			AutoHideMemberCountThreshold *int  `json:"auto_hide_member_count_threshold"`
		}
		decoder := json.NewDecoder(req.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil {
			r.writeError(w, 400, "body must be JSON {\"auto_hide_enabled\":…,\"auto_hide_member_count_threshold\":…}: "+err.Error())
			return
		}
		if body.AutoHideEnabled == nil || body.AutoHideMemberCountThreshold == nil {
			r.writeError(w, 400, "auto_hide_enabled and auto_hide_member_count_threshold are both required")
			return
		}
		settings := db.ConversationVisibilitySettings{
			AutoHideEnabled:              *body.AutoHideEnabled,
			AutoHideMemberCountThreshold: *body.AutoHideMemberCountThreshold,
		}
		if err := db.SaveConversationVisibilitySettings(r.db, settings); err != nil {
			if errors.Is(err, db.ErrInvalidConversationVisibilitySettings) {
				r.writeError(w, 400, err.Error())
				return
			}
			r.writeError(w, 500, err.Error())
			return
		}
	default:
		r.writeError(w, 405, "Method not allowed")
		return
	}
	settings, err := db.ReadConversationVisibilitySettings(r.db)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	r.writeJSON(w, settings)
}
