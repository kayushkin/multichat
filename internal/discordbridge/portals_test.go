package discordbridge

import (
	"testing"

	"github.com/kayushkin/multichat/internal/matrix"
)

func TestPortalsByRoomIDGiveEachRoomItsChannelAndMarkOneToOneDMs(t *testing.T) {
	portals, err := PortalsByRoomID(throwawayBridge(t))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]matrix.DiscordPortal{
		"!dm:x":       {ChannelID: "555", DirectMessage: true},
		"!group-dm:x": {ChannelID: "557"},
		"!general:x":  {ChannelID: "1001"},
	}
	for roomID, want := range cases {
		if got := portals[roomID]; got != want {
			t.Errorf("PortalsByRoomID()[%q] = %+v, want %+v", roomID, got, want)
		}
	}
	// A channel the bridge has made no room for has no entry.
	for _, portal := range portals {
		if portal.ChannelID == "556" || portal.ChannelID == "1005" {
			t.Errorf("a channel with no room is listed: %+v", portal)
		}
	}
}

func TestDirectMessagePortalWithFindsTheDMAndItsRoom(t *testing.T) {
	bridge := throwawayBridge(t)
	cases := []struct {
		discordUserID string
		want          DirectMessagePortal
		found         bool
	}{
		{"42", DirectMessagePortal{ChannelID: "555", RoomID: "!dm:x"}, true},
		{"43", DirectMessagePortal{ChannelID: "556"}, true},
		{"44", DirectMessagePortal{}, false},
	}
	for _, c := range cases {
		got, found, err := DirectMessagePortalWith(bridge, c.discordUserID)
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want || found != c.found {
			t.Errorf("DirectMessagePortalWith(%q) = %+v, %v; want %+v, %v", c.discordUserID, got, found, c.want, c.found)
		}
	}
}
