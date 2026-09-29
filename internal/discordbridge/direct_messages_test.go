package discordbridge

import (
	"reflect"
	"testing"
)

func TestDirectMessageRoomIDsAreOneToOneDMsWithARoom(t *testing.T) {
	roomIDs, err := DirectMessageRoomIDs(throwawayBridge(t))
	if err != nil {
		t.Fatal(err)
	}
	// Not the group DM, not a DM with no room yet, not a server channel.
	if want := map[string]bool{"!dm:x": true}; !reflect.DeepEqual(roomIDs, want) {
		t.Fatalf("DirectMessageRoomIDs = %v, want %v", roomIDs, want)
	}
}
