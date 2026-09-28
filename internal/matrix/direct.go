package matrix

import (
	"encoding/json"
	"fmt"
	"net/url"
)

// WhoAmI returns the Matrix user id behind this client's access token
// (@admin:chat.kayushkin.com in production). It never changes for the life of
// a token, so the first answer is kept on the client; a failed call is not
// kept, so a Synapse that was briefly down is asked again next time.
func (c *Client) WhoAmI() (string, error) {
	c.selfUserIDMutex.Lock()
	defer c.selfUserIDMutex.Unlock()
	if c.selfUserID != "" {
		return c.selfUserID, nil
	}
	data, err := c.request("GET", "/_matrix/client/v3/account/whoami", nil)
	if err != nil {
		return "", err
	}
	var resp struct {
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("whoami: %w", err)
	}
	if resp.UserID == "" {
		return "", fmt.Errorf("whoami: empty user_id in %s", data)
	}
	c.selfUserID = resp.UserID
	return c.selfUserID, nil
}

// JoinedMemberIDs returns the user ids currently joined to a room.
func (c *Client) JoinedMemberIDs(roomID string) ([]string, error) {
	data, err := c.request("GET", "/_matrix/client/v3/rooms/"+url.PathEscape(roomID)+"/members?membership=join", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Chunk []struct {
			StateKey string `json:"state_key"`
		} `json:"chunk"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("members of %s: %w", roomID, err)
	}
	ids := make([]string, 0, len(resp.Chunk))
	for _, m := range resp.Chunk {
		ids = append(ids, m.StateKey)
	}
	return ids, nil
}

// OtherPeopleInRoom filters a member list down to the people who are not us:
// it drops the bridge bots, our own Matrix user and every puppet that
// represents us on a platform (SelfUserIDs). What is left is who we are
// talking to — one id for a direct message, several for a group.
func (c *Client) OtherPeopleInRoom(memberIDs []string) ([]string, error) {
	self, err := c.WhoAmI()
	if err != nil {
		return nil, err
	}
	var people []string
	for _, id := range memberIDs {
		if id == self || SelfUserIDs[id] || IsBridgeBot(id) {
			continue
		}
		people = append(people, id)
	}
	return people, nil
}

// FindDirectMessageRoom returns the id of the joined room that is a direct
// message with userID — the room whose only other person is that user — or
// "" when no such room exists. It scans joined rooms rather than reading
// m.direct account data because the mautrix bridges create portal rooms
// without writing m.direct for this account (measured: the account has no
// m.direct at all).
func (c *Client) FindDirectMessageRoom(userID string) (string, error) {
	joinedRoomIDs, err := c.JoinedRoomIDs()
	if err != nil {
		return "", err
	}
	for _, roomID := range joinedRoomIDs {
		members, err := c.JoinedMemberIDs(roomID)
		if err != nil {
			return "", err
		}
		people, err := c.OtherPeopleInRoom(members)
		if err != nil {
			return "", err
		}
		if len(people) == 1 && people[0] == userID {
			return roomID, nil
		}
	}
	return "", nil
}

// RoomName returns the m.room.name of a room, "" when it has none (a direct
// message has none).
func (c *Client) RoomName(roomID string) (string, error) {
	data, err := c.request("GET", "/_matrix/client/v3/rooms/"+url.PathEscape(roomID)+"/state/m.room.name", nil)
	if err != nil {
		// Synapse answers 404 M_NOT_FOUND for a room with no name; that is
		// the normal case for a DM, not a failure.
		if isMatrixNotFound(err) {
			return "", nil
		}
		return "", err
	}
	var evt struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &evt); err != nil {
		return "", fmt.Errorf("room name of %s: %w", roomID, err)
	}
	return evt.Name, nil
}

// DisplayNameOf returns a user's global display name, "" when unset.
func (c *Client) DisplayNameOf(userID string) (string, error) {
	data, err := c.request("GET", "/_matrix/client/v3/profile/"+url.PathEscape(userID)+"/displayname", nil)
	if err != nil {
		if isMatrixNotFound(err) {
			return "", nil
		}
		return "", err
	}
	var resp struct {
		DisplayName string `json:"displayname"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("displayname of %s: %w", userID, err)
	}
	return stripBridgeSuffix(resp.DisplayName), nil
}

// BridgeInfo is what a bridge states about the platform room behind a Matrix
// room, from the room's m.bridge state event (MSC2346). On Discord the
// network is the server and the channel is the channel.
type BridgeInfo struct {
	NetworkID          string
	NetworkName        string
	ChannelID          string
	ChannelName        string
	ChannelExternalURL string
}

// RoomBridgeInfo reads a room's m.bridge state event. A room with none (a
// plain Matrix room, or a bridge that does not set one) gives an empty
// BridgeInfo; a room with more than one is an error, since there is then no
// single answer.
func (c *Client) RoomBridgeInfo(roomID string) (BridgeInfo, error) {
	data, err := c.request("GET", "/_matrix/client/v3/rooms/"+url.PathEscape(roomID)+"/state", nil)
	if err != nil {
		return BridgeInfo{}, err
	}
	var events []struct {
		Type    string `json:"type"`
		Content struct {
			Network struct {
				ID          string `json:"id"`
				DisplayName string `json:"displayname"`
			} `json:"network"`
			Channel struct {
				ID          string `json:"id"`
				DisplayName string `json:"displayname"`
				ExternalURL string `json:"external_url"`
			} `json:"channel"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &events); err != nil {
		return BridgeInfo{}, fmt.Errorf("state of %s: %w", roomID, err)
	}
	var found []BridgeInfo
	for _, event := range events {
		if event.Type != "m.bridge" {
			continue
		}
		found = append(found, BridgeInfo{
			NetworkID:          event.Content.Network.ID,
			NetworkName:        event.Content.Network.DisplayName,
			ChannelID:          event.Content.Channel.ID,
			ChannelName:        event.Content.Channel.DisplayName,
			ChannelExternalURL: event.Content.Channel.ExternalURL,
		})
	}
	switch len(found) {
	case 0:
		return BridgeInfo{}, nil
	case 1:
		return found[0], nil
	default:
		return BridgeInfo{}, fmt.Errorf("room %s has %d m.bridge state events", roomID, len(found))
	}
}

// JoinedRoomIDs lists the rooms this account has joined. A room it is only
// invited to is not among them.
func (c *Client) JoinedRoomIDs() ([]string, error) {
	data, err := c.request("GET", "/_matrix/client/v3/joined_rooms", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		JoinedRooms []string `json:"joined_rooms"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("joined_rooms: %w", err)
	}
	return resp.JoinedRooms, nil
}
