package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/kayushkin/multichat/internal/awaitingreply"
	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/matrix"
)

// awaitingReplyHandler serves GET /awaiting-reply: the conversations holding
// a message from a tagged person that came after the operator's last word,
// and the ones that stopped waiting in the last week.
func (r *Router) awaitingReplyHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "GET" {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	if r.matrix == nil {
		r.writeError(w, 503, "Matrix client not configured")
		return
	}
	if !r.people.Configured() {
		r.writeError(w, 503, "PEOPLE_STORE_URL is not set: the awaiting-reply list counts people, and people-store owns them")
		return
	}
	now := time.Now().UTC()
	settings, err := db.ReadAwaitingReplySettings(r.db)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	in := awaitingreply.Inputs{Settings: settings, Now: now, OwnUserIDs: matrix.SelfUserIDs}
	if in.TaggedAccounts, err = db.ContactUserIDsWithTags(r.db, settings.TagIDs); err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	if in.TaggedRooms, err = db.RoomIDsWithTags(r.db, settings.TagIDs); err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	identities, err := r.people.MatrixUserIdentities()
	if err != nil {
		r.writeError(w, 502, "read accounts from people-store: "+err.Error())
		return
	}
	persons, err := r.people.Persons()
	if err != nil {
		r.writeError(w, 502, "read people from people-store: "+err.Error())
		return
	}
	in.AccountPersons = map[string]string{}
	for userID, identity := range identities {
		in.AccountPersons[userID] = identity.PersonID
	}
	in.PersonNames = map[string]string{}
	for id, person := range persons {
		in.PersonNames[id] = person.DisplayName
	}

	counted := awaitingreply.CountedAccounts(in.TaggedAccounts, in.AccountPersons)
	since := now.Add(-time.Duration(settings.MaxMessageAgeDays) * 24 * time.Hour)
	if in.Messages, err = db.MessagesFromSendersOrInRoomsSince(r.db, keys(counted), keys(in.TaggedRooms), since); err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	if in.NewestOwnMessageAt, err = db.NewestMessageSentAtByRoomFrom(r.db, keys(matrix.SelfUserIDs), since); err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	if in.RoomClears, err = db.AwaitingReplyRoomClears(r.db); err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	conversations, err := r.listConversations()
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	in.Conversations = map[string]matrix.Conversation{}
	for _, conversation := range conversations {
		in.Conversations[conversation.RoomID] = conversation
	}
	r.writeJSON(w, awaitingreply.Compute(in))
}

// awaitingReplySettingsHandler serves GET and PUT /awaiting-reply/settings.
// PUT takes the whole settings object; an unknown field is a 400.
func (r *Router) awaitingReplySettingsHandler(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case "GET":
		settings, err := db.ReadAwaitingReplySettings(r.db)
		if err != nil {
			r.writeError(w, 500, err.Error())
			return
		}
		r.writeJSON(w, settings)
	case "PUT":
		var body struct {
			TagIDs              []int64 `json:"tag_ids"`
			MaxGroupMemberCount *int    `json:"max_group_member_count"`
			MaxMessageAgeDays   *int    `json:"max_message_age_days"`
			ClearWhenOpened     *bool   `json:"clear_when_opened"`
			ClearWhenReplied    *bool   `json:"clear_when_replied"`
		}
		decoder := json.NewDecoder(req.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&body); err != nil {
			r.writeError(w, 400, "body must be the whole settings object {tag_ids, max_group_member_count, max_message_age_days, clear_when_opened, clear_when_replied}: "+err.Error())
			return
		}
		if body.TagIDs == nil || body.MaxGroupMemberCount == nil || body.MaxMessageAgeDays == nil || body.ClearWhenOpened == nil || body.ClearWhenReplied == nil {
			r.writeError(w, 400, "every field is required: tag_ids, max_group_member_count, max_message_age_days, clear_when_opened, clear_when_replied")
			return
		}
		saved, err := db.SaveAwaitingReplySettings(r.db, db.AwaitingReplySettings{
			TagIDs:              body.TagIDs,
			MaxGroupMemberCount: *body.MaxGroupMemberCount,
			MaxMessageAgeDays:   *body.MaxMessageAgeDays,
			ClearWhenOpened:     *body.ClearWhenOpened,
			ClearWhenReplied:    *body.ClearWhenReplied,
		}, time.Now())
		if errors.Is(err, db.ErrInvalidAwaitingReplySettings) {
			r.writeError(w, 400, err.Error())
			return
		}
		if err != nil {
			r.writeError(w, 500, err.Error())
			return
		}
		r.writeJSON(w, saved)
	default:
		r.writeError(w, 405, "Method not allowed")
	}
}

// awaitingReplyClearsHandler serves POST /awaiting-reply/clears
// {room_id, reason, cleared_through}: the operator opened or dismissed the
// room having seen it up to cleared_through, the sent_at of the newest
// message they were shown. A message sent after it brings the room back.
func (r *Router) awaitingReplyClearsHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	var body struct {
		RoomID         string    `json:"room_id"`
		Reason         string    `json:"reason"`
		ClearedThrough time.Time `json:"cleared_through"`
	}
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		r.writeError(w, 400, "body must be JSON {\"room_id\":…,\"reason\":…,\"cleared_through\":…}: "+err.Error())
		return
	}
	clear, err := db.ClearAwaitingReplyRoom(r.db, body.RoomID, body.ClearedThrough, body.Reason, time.Now())
	if errors.Is(err, db.ErrInvalidAwaitingReplyClear) {
		r.writeError(w, 400, err.Error())
		return
	}
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	r.writeJSON(w, clear)
}

func keys[V any](set map[string]V) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	return out
}
