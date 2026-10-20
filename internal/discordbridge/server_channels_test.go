package discordbridge

import "testing"

func TestServerChannelRoomsKeepVoiceChannelsAndLeaveOutCategoriesAndDirectMessages(t *testing.T) {
	rooms, err := ServerChannelRooms(throwawayBridge(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, roomID := range []string{"!general:x", "!quiet:x", "!news:x", "!voice:x", "!other:x"} {
		if _, found := rooms[roomID]; !found {
			t.Errorf("%s is missing", roomID)
		}
	}
	for _, roomID := range []string{"!category:x", "!dm:x", "!group-dm:x"} {
		if _, found := rooms[roomID]; found {
			t.Errorf("%s is listed", roomID)
		}
	}
	general := rooms["!general:x"]
	if general.DiscordServerID != "900" || general.ServerName != "Riverside" || general.ChannelName != "#general" || general.CategoryName != "Text Channels" {
		t.Errorf("general: %+v", general)
	}
	if len(rooms) != 5 {
		t.Errorf("%d rooms: %+v", len(rooms), rooms)
	}
}
