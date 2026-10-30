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
