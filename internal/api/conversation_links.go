package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/kayushkin/multichat/internal/db"
)

// conversationLinksHandler serves GET /conversations/links, every link with
// its rooms, and POST /conversations/links {room_ids}, which makes the rooms
// one conversation and answers the link they are in afterwards.
func (r *Router) conversationLinksHandler(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case "GET":
		links, err := db.ListConversationLinks(r.db)
		if err != nil {
			r.writeError(w, 500, err.Error())
			return
		}
		r.writeJSON(w, links)
	case "POST":
		var body struct {
			RoomIDs []string `json:"room_ids"`
		}
		decoder := json.NewDecoder(req.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil {
			r.writeError(w, 400, "body must be JSON {\"room_ids\":[…]}: "+err.Error())
			return
		}
		link, err := db.LinkConversations(r.db, body.RoomIDs)
		if err != nil {
			switch {
			case errors.Is(err, db.ErrInvalidRoomID), errors.Is(err, db.ErrTooFewRoomsToLink):
				r.writeError(w, 400, err.Error())
			case errors.Is(err, db.ErrRoomsInDifferentLinks):
				r.writeError(w, 409, err.Error())
			default:
				r.writeError(w, 500, err.Error())
			}
			return
		}
		r.writeJSON(w, link)
	default:
		r.writeError(w, 405, "Method not allowed")
	}
}

// unlinkConversationHandler serves POST /conversations/links/remove {room_id}:
// the room leaves its link, and a link left with one room goes. Answers every
// link afterwards.
func (r *Router) unlinkConversationHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	var body struct {
		RoomID string `json:"room_id"`
	}
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		r.writeError(w, 400, "body must be JSON {\"room_id\":…}: "+err.Error())
		return
	}
	if err := db.UnlinkConversation(r.db, body.RoomID); err != nil {
		if errors.Is(err, db.ErrInvalidRoomID) {
			r.writeError(w, 400, err.Error())
		} else {
			r.writeError(w, 500, err.Error())
		}
		return
	}
	links, err := db.ListConversationLinks(r.db)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	r.writeJSON(w, links)
}
