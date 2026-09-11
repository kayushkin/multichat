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
}

// MessageContent is the content of an m.room.message event.
type MessageContent struct {
	MsgType string `json:"msgtype"`
	Body    string `json:"body"`
}

// SyncResponse is the part of a /sync answer this service reads: the token to
// resume from and, per joined room, the new timeline events.
type SyncResponse struct {
	NextBatch string
	Rooms     map[string][]TimelineEvent
}

// syncFilter asks Synapse for m.room.message events only, with the member
// state lazy-loaded so a sync does not carry every membership of every room.
// The timeline limit is per room per sync; a room that produced more than
// this many messages between two syncs is reported with limited=true and the
// oldest are dropped, which the worker logs rather than hides.
const syncTimelineLimit = 50

var syncFilter = url.QueryEscape(`{"room":{"timeline":{"types":["m.room.message"],"limit":` + strconv.Itoa(syncTimelineLimit) + `},"state":{"lazy_load_members":true,"types":[]},"ephemeral":{"types":[]},"account_data":{"types":[]}},"presence":{"types":[]},"account_data":{"types":[]}}`)

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
					Events  []TimelineEvent `json:"events"`
					Limited bool            `json:"limited"`
				} `json:"timeline"`
			} `json:"join"`
		} `json:"rooms"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("sync: %w", err)
	}
	if raw.NextBatch == "" {
		return nil, fmt.Errorf("sync: empty next_batch")
	}
	out := &SyncResponse{NextBatch: raw.NextBatch, Rooms: map[string][]TimelineEvent{}}
	var limited []string
	for roomID, room := range raw.Rooms.Join {
		if len(room.Timeline.Events) == 0 {
			continue
		}
		out.Rooms[roomID] = room.Timeline.Events
		if room.Timeline.Limited && since != "" {
			limited = append(limited, roomID)
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
	return "sync: timeline truncated (more than " + strconv.Itoa(syncTimelineLimit) + " new messages) in " + strings.Join(e.Rooms, ", ")
}

func isMatrixNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), ": 404 ")
}
