package matrix

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// inviteRoom is one room under rooms.invite in a /sync answer: the stripped
// state the inviter shared, which includes our own m.room.member invite.
type inviteRoom struct {
	InviteState struct {
		Events []struct {
			Type     string `json:"type"`
			StateKey string `json:"state_key"`
			Sender   string `json:"sender"`
			Content  struct {
				Membership string `json:"membership"`
			} `json:"content"`
		} `json:"events"`
	} `json:"invite_state"`
}

// invitersOf maps each invited room to the user who sent our invite. A room
// whose stripped state carries no invite for us is an error: Synapse always
// includes it, so its absence means the answer is not what this code expects.
func (c *Client) invitersOf(rooms map[string]inviteRoom) (map[string]string, error) {
	if len(rooms) == 0 {
		return map[string]string{}, nil
	}
	self, err := c.WhoAmI()
	if err != nil {
		return nil, err
	}
	inviters := map[string]string{}
	for roomID, room := range rooms {
		for _, event := range room.InviteState.Events {
			if event.Type == "m.room.member" && event.StateKey == self && event.Content.Membership == "invite" {
				inviters[roomID] = event.Sender
			}
		}
		if inviters[roomID] == "" {
			return nil, fmt.Errorf("sync: invite to %s carries no m.room.member invite for %s", roomID, self)
		}
	}
	return inviters, nil
}

// pendingInvitesFilter asks for invites only: no timeline, no state, nothing
// else, so listing them does not pull every joined room's history.
var pendingInvitesFilter = url.QueryEscape(`{"room":{"timeline":{"limit":0},"state":{"types":[]},"ephemeral":{"types":[]},"account_data":{"types":[]}},"presence":{"types":[]},"account_data":{"types":[]}}`)

// PendingInvites returns every room we are invited to and have not joined,
// mapped to the user who invited us. An incremental sync reports an invite
// only once, when it arrives, so this full sync is how invites that arrived
// while nobody was accepting them are found.
func (c *Client) PendingInvites() (map[string]string, error) {
	data, err := c.request("GET", "/_matrix/client/v3/sync?timeout=0&filter="+pendingInvitesFilter, nil)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Rooms struct {
			Invite map[string]inviteRoom `json:"invite"`
		} `json:"rooms"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("pending invites: %w", err)
	}
	return c.invitersOf(raw.Rooms.Invite)
}

// RateLimitedError is Synapse refusing a request with 429 M_LIMIT_EXCEEDED.
// RetryAfter is how long it asked us to wait.
type RateLimitedError struct {
	Path       string
	RetryAfter time.Duration
}

func (e *RateLimitedError) Error() string {
	return "matrix " + e.Path + ": rate limited, retry after " + e.RetryAfter.String()
}

// JoinRoom accepts an invite to a room. Synapse limits how fast one account
// may join rooms; a refusal for that reason is a *RateLimitedError.
func (c *Client) JoinRoom(roomID string) error {
	return c.postRateLimited("/_matrix/client/v3/rooms/"+url.PathEscape(roomID)+"/join", `{}`)
}

// InviteUser invites a user to a room. Synapse limits how many invites one
// user may receive; a refusal for that reason is a *RateLimitedError.
func (c *Client) InviteUser(roomID, userID string) error {
	body, err := json.Marshal(map[string]string{"user_id": userID})
	if err != nil {
		return err
	}
	return c.postRateLimited("/_matrix/client/v3/rooms/"+url.PathEscape(roomID)+"/invite", string(body))
}

// postRateLimited POSTs body to path and turns a 429 into a
// *RateLimitedError carrying how long Synapse asked us to wait.
func (c *Client) postRateLimited(path, body string) error {
	status, data, err := c.requestWithStatus("POST", path, strings.NewReader(body))
	if err != nil {
		return err
	}
	if status == 429 {
		var limit struct {
			RetryAfterMilliseconds int64 `json:"retry_after_ms"`
		}
		if err := json.Unmarshal(data, &limit); err != nil || limit.RetryAfterMilliseconds <= 0 {
			return fmt.Errorf("matrix POST %s: 429 without retry_after_ms: %s", path, data)
		}
		return &RateLimitedError{Path: path, RetryAfter: time.Duration(limit.RetryAfterMilliseconds) * time.Millisecond}
	}
	if status >= 400 {
		return fmt.Errorf("matrix POST %s: %d %s", path, status, data)
	}
	return nil
}
