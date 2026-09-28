package api

import (
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/matrix"
)

// maxMessageLogLimit caps one page of the message log; older pages are read
// with before=.
const maxMessageLogLimit = 1000

// messageLogHandler serves GET /message-log: logged messages newest first,
// each deleted one with the edits made to it before it went.
//
//	platform, network_id, room_id, sender_user_id  exact matches
//	q        substring of the body, any case
//	deleted  "true" for deleted messages only
//	before   RFC 3339 time; messages sent strictly before it (the next page)
//	limit    1–1000, default 200
func (r *Router) messageLogHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "GET" {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	query := req.URL.Query()
	filter := db.MessageLogFilter{
		Platform:     query.Get("platform"),
		NetworkID:    query.Get("network_id"),
		RoomID:       query.Get("room_id"),
		SenderUserID: query.Get("sender_user_id"),
		Query:        query.Get("q"),
		Limit:        200,
	}
	if filter.Platform != "" && !slices.Contains(matrix.KnownPlatforms, filter.Platform) {
		r.writeError(w, 400, "platform must be one of GET /inbound-platforms")
		return
	}
	switch query.Get("deleted") {
	case "", "false":
	case "true":
		filter.DeletedOnly = true
	default:
		r.writeError(w, 400, "deleted must be true or false")
		return
	}
	if before := query.Get("before"); before != "" {
		t, err := time.Parse(time.RFC3339Nano, before)
		if err != nil {
			r.writeError(w, 400, "before must be an RFC 3339 time")
			return
		}
		filter.Before = t
	}
	if l := query.Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n <= 0 || n > maxMessageLogLimit {
			r.writeError(w, 400, "limit must be an integer from 1 to "+strconv.Itoa(maxMessageLogLimit))
			return
		}
		filter.Limit = n
	}
	messages, err := db.ListLoggedMessages(r.db, filter)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	ids := make([]string, len(messages))
	for i, m := range messages {
		ids[i] = m.EventID
	}
	edits, err := db.EditsOf(r.db, ids)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	r.writeJSON(w, db.MessageLogPage{Messages: messages, Edits: edits})
}
