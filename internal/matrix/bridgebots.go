package matrix

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
)

// BridgeBotMembership is a bridge bot a room's m.bridge state names, with
// the bot's membership in that room ("" when it has none at all).
type BridgeBotMembership struct {
	BotUserID  string
	Membership string
}

// RoomBridgeBots reads which bridge bots a room's m.bridge state events name
// (their bridgebot field) and each bot's membership in the room. A room with
// no m.bridge state gives none.
func (c *Client) RoomBridgeBots(roomID string) ([]BridgeBotMembership, error) {
	data, err := c.request("GET", "/_matrix/client/v3/rooms/"+url.PathEscape(roomID)+"/state", nil)
	if err != nil {
		return nil, err
	}
	var events []struct {
		Type     string `json:"type"`
		StateKey string `json:"state_key"`
		Content  struct {
			BridgeBot  string `json:"bridgebot"`
			Membership string `json:"membership"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &events); err != nil {
		return nil, fmt.Errorf("state of %s: %w", roomID, err)
	}
	membership := map[string]string{}
	bots := map[string]bool{}
	for _, event := range events {
		switch event.Type {
		case "m.room.member":
			membership[event.StateKey] = event.Content.Membership
		case "m.bridge":
			if event.Content.BridgeBot == "" {
				return nil, fmt.Errorf("room %s: m.bridge state %q names no bridgebot", roomID, event.StateKey)
			}
			bots[event.Content.BridgeBot] = true
		}
	}
	var out []BridgeBotMembership
	for bot := range bots {
		out = append(out, BridgeBotMembership{BotUserID: bot, Membership: membership[bot]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BotUserID < out[j].BotUserID })
	return out, nil
}
