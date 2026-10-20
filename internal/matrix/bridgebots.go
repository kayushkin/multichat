package matrix

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
)

// BridgeBotMembership is a bridge bot a room's m.bridge state names, the
// protocol that state names (discordgo, telegram, …), and the bot's
// membership in that room ("" when it has none at all).
type BridgeBotMembership struct {
	BotUserID  string
	ProtocolID string
	Membership string
}

// RoomBridgeBots reads which bridge bots a room's m.bridge state events name
// (their bridgebot field), the protocol each names, and each bot's
// membership in the room. A room with
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
			Protocol   struct {
				ID string `json:"id"`
			} `json:"protocol"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &events); err != nil {
		return nil, fmt.Errorf("state of %s: %w", roomID, err)
	}
	membership := map[string]string{}
	protocolOfBot := map[string]string{}
	for _, event := range events {
		switch event.Type {
		case "m.room.member":
			membership[event.StateKey] = event.Content.Membership
		case "m.bridge":
			if event.Content.BridgeBot == "" {
				return nil, fmt.Errorf("room %s: m.bridge state %q names no bridgebot", roomID, event.StateKey)
			}
			if event.Content.Protocol.ID == "" {
				return nil, fmt.Errorf("room %s: m.bridge state %q names no protocol", roomID, event.StateKey)
			}
			protocolOfBot[event.Content.BridgeBot] = event.Content.Protocol.ID
		}
	}
	var out []BridgeBotMembership
	for bot, protocolID := range protocolOfBot {
		out = append(out, BridgeBotMembership{BotUserID: bot, ProtocolID: protocolID, Membership: membership[bot]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BotUserID < out[j].BotUserID })
	return out, nil
}
