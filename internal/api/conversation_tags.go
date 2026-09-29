package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/kayushkin/multichat/internal/db"
)

// conversationTagsHandler serves POST /conversations/tags {room_id, tag_id},
// which puts a tag on a room, and DELETE /conversations/tags?room=…&tag=…,
// which takes it off. Both answer the room's tags afterwards.
func (r *Router) conversationTagsHandler(w http.ResponseWriter, req *http.Request) {
	var roomID string
	var tagID int64
	switch req.Method {
	case "POST":
		var body struct {
			RoomID string `json:"room_id"`
			TagID  int64  `json:"tag_id"`
		}
		decoder := json.NewDecoder(req.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil {
			r.writeError(w, 400, "body must be JSON {\"room_id\":…,\"tag_id\":…}: "+err.Error())
			return
		}
		if body.RoomID == "" || body.TagID == 0 {
			r.writeError(w, 400, "room_id and tag_id are required")
			return
		}
		roomID, tagID = body.RoomID, body.TagID
		if err := db.AssignConversationTag(r.db, roomID, tagID); err != nil {
			switch {
			case errors.Is(err, db.ErrInvalidRoomID):
				r.writeError(w, 400, err.Error())
			case errors.Is(err, db.ErrUnknownTag):
				r.writeError(w, 404, err.Error())
			default:
				r.writeError(w, 500, err.Error())
			}
			return
		}
	case "DELETE":
		roomID = req.URL.Query().Get("room")
		parsed, err := strconv.ParseInt(req.URL.Query().Get("tag"), 10, 64)
		if roomID == "" || err != nil {
			r.writeError(w, 400, "room and a numeric tag are required")
			return
		}
		tagID = parsed
		if err := db.RemoveConversationTag(r.db, roomID, tagID); err != nil {
			r.writeError(w, 500, err.Error())
			return
		}
	default:
		r.writeError(w, 405, "Method not allowed")
		return
	}
	tagMap, err := db.ConversationTagMap(r.db)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	tags := tagMap[roomID]
	if tags == nil {
		tags = []db.Tag{}
	}
	r.writeJSON(w, map[string]any{"room_id": roomID, "tags": tags})
}

// bulkConversationTagsHandler serves GET /conversations/tags/bulk: every
// room's own tags, keyed by room id.
func (r *Router) bulkConversationTagsHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "GET" {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	tagMap, err := db.ConversationTagMap(r.db)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	r.writeJSON(w, tagMap)
}
