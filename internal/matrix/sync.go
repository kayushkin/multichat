package matrix

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// TimelineEvent is one event out of a room's /sync timeline.
type TimelineEvent struct {
	EventID        string          `json:"event_id"`
	Type           string          `json:"type"`
	Sender         string          `json:"sender"`
	OriginServerTS int64           `json:"origin_server_ts"`
	Content        json.RawMessage `json:"content"`
	// Redacts is the event an m.room.redaction removes, in room versions
	// before 11. From version 11 it moved into content; RedactedEventID
	// reads both.
	Redacts string `json:"redacts"`
	// Unsigned.RedactedBecause is set when the event was already redacted by
	// the time it was read; its content is then empty.
	Unsigned struct {
		RedactedBecause *TimelineEvent `json:"redacted_because"`
	} `json:"unsigned"`
}

// RedactedEventID returns the event an m.room.redaction removes.
func (e TimelineEvent) RedactedEventID() (string, error) {
	if e.Redacts != "" {
		return e.Redacts, nil
	}
	var content struct {
		Redacts string `json:"redacts"`
	}
	if err := json.Unmarshal(e.Content, &content); err != nil {
		return "", fmt.Errorf("redaction %s: %w", e.EventID, err)
	}
	if content.Redacts == "" {
		return "", fmt.Errorf("redaction %s names no event", e.EventID)
	}
	return content.Redacts, nil
}

// MessageContent is the content of an m.room.message event.
type MessageContent struct {
	MsgType string `json:"msgtype"`
	Body    string `json:"body"`
}

// SyncResponse is the part of a /sync answer this service reads: the token to
// resume from, per joined room the new timeline events, and the rooms we have
// been invited to.
type SyncResponse struct {
	NextBatch string
	Rooms     map[string][]TimelineEvent
	// Invites maps each room we are invited to but have not joined to the
	// user who invited us.
	Invites map[string]string
	// PrevBatch holds, for each room whose timeline was truncated, the token
	// that pages backward from the oldest event delivered into the gap.
	PrevBatch map[string]string
}

// syncFilter asks Synapse for messages, stickers, reactions, redactions and
// the bridges' message send statuses only, with the member
// state lazy-loaded so a sync does not carry every membership of every room.
// The timeline limit is per room per sync; a room that produced more than
// this many events between two syncs is reported with limited=true and the
// oldest are dropped, which the worker logs rather than hides.
const syncTimelineLimit = 50

var syncFilter = url.QueryEscape(`{"room":{"timeline":{"types":["m.room.message","m.sticker","m.reaction","m.room.redaction","com.beeper.message_send_status"],"limit":` + strconv.Itoa(syncTimelineLimit) + `},"state":{"lazy_load_members":true,"types":[]},"ephemeral":{"types":[]},"account_data":{"types":[]}},"presence":{"types":[]},"account_data":{"types":[]}}`)

// Sync long-polls /sync. With since == "" it is the initial sync, which
// returns the token to start from and whatever recent timeline Synapse
// chooses to include; a caller doing an initial sync should keep the token
// and ignore the events, because they are history, not new arrivals.
// timeoutMilliseconds is how long Synapse holds the request open waiting for
// something to happen.
func (c *Client) Sync(since string, timeoutMilliseconds int) (*SyncResponse, error) {
	path := "/_matrix/client/v3/sync?filter=" + syncFilter + "&timeout=" + strconv.Itoa(timeoutMilliseconds)
	if since != "" {
		path += "&since=" + url.QueryEscape(since)
	}
	data, err := c.request("GET", path, nil)
	if err != nil {
		return nil, err
	}
	var raw struct {
		NextBatch string `json:"next_batch"`
		Rooms     struct {
			Join map[string]struct {
				Timeline struct {
					Events    []TimelineEvent `json:"events"`
					Limited   bool            `json:"limited"`
					PrevBatch string          `json:"prev_batch"`
				} `json:"timeline"`
			} `json:"join"`
			Invite map[string]inviteRoom `json:"invite"`
		} `json:"rooms"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("sync: %w", err)
	}
	if raw.NextBatch == "" {
		return nil, fmt.Errorf("sync: empty next_batch")
	}
	invites, err := c.invitersOf(raw.Rooms.Invite)
	if err != nil {
		return nil, err
	}
	out := &SyncResponse{NextBatch: raw.NextBatch, Rooms: map[string][]TimelineEvent{}, Invites: invites, PrevBatch: map[string]string{}}
	var limited []string
	for roomID, room := range raw.Rooms.Join {
		if len(room.Timeline.Events) == 0 {
			continue
		}
		out.Rooms[roomID] = room.Timeline.Events
		if room.Timeline.Limited && since != "" {
			limited = append(limited, roomID)
			out.PrevBatch[roomID] = room.Timeline.PrevBatch
		}
	}
	if len(limited) > 0 {
		// Not fatal — the messages we did get are real — but the gap must be
		// visible, so it is an error the worker logs and moves past.
		return out, &TimelineGapError{Rooms: limited}
	}
	return out, nil
}

// TimelineGapError reports rooms whose timeline was truncated by
// syncTimelineLimit, meaning some messages between two syncs were not
// delivered.
type TimelineGapError struct{ Rooms []string }

func (e *TimelineGapError) Error() string {
	return "sync: timeline truncated (more than " + strconv.Itoa(syncTimelineLimit) + " new events) in " + strings.Join(e.Rooms, ", ")
}

func isMatrixNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), ": 404 ")
}

// RoomEventsBefore pages backward through a room's messages, stickers,
// reactions and redactions from a token, newest first, and returns the token
// for the next older page ("" when the room has no more). An empty from
// starts at the room's newest event.
func (c *Client) RoomEventsBefore(roomID, from string, limit int) ([]TimelineEvent, string, error) {
	path := "/_matrix/client/v3/rooms/" + url.PathEscape(roomID) + "/messages?dir=b&limit=" + strconv.Itoa(limit) +
		"&filter=" + url.QueryEscape(`{"types":["m.room.message","m.sticker","m.reaction","m.room.redaction","com.beeper.message_send_status"]}`)
	if from != "" {
		path += "&from=" + url.QueryEscape(from)
	}
	data, err := c.request("GET", path, nil)
	if err != nil {
		return nil, "", err
	}
	var page struct {
		Chunk []TimelineEvent `json:"chunk"`
		End   string          `json:"end"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, "", fmt.Errorf("messages of %s: %w", roomID, err)
	}
	return page.Chunk, page.End, nil
}
