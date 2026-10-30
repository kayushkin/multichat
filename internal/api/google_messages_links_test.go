package api

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/matrix"
)

func TestLinkGoogleMessagesTwinsLinksRoomsOfOneConversationOnce(t *testing.T) {
	database, err := db.Initialize(filepath.Join(t.TempDir(), "multichat.db"))
	if err != nil {
		t.Fatal(err)
	}
	// !other is linked by hand with a Discord room; its twin joins that link.
	if _, err := db.LinkConversations(database, []string{"!other-web:x", "!discord:x"}); err != nil {
		t.Fatal(err)
	}
	conversations := []matrix.Conversation{
		{RoomID: "!bridge:x", GoogleMessagesConversationID: "39"},
		{RoomID: "!web:x", GoogleMessagesConversationID: "39"},
		{RoomID: "!other-bridge:x", GoogleMessagesConversationID: "40"},
		{RoomID: "!other-web:x", GoogleMessagesConversationID: "40"},
		{RoomID: "!alone:x", GoogleMessagesConversationID: "41"},
	}
	result, err := linkGoogleMessagesTwins(database, conversations)
	if err != nil {
		t.Fatal(err)
	}
	if result != (GoogleMessagesLinkResult{Conversations: 2, Linked: 2}) {
		t.Fatalf("first pass %+v", result)
	}
	again, err := linkGoogleMessagesTwins(database, conversations)
	if err != nil {
		t.Fatal(err)
	}
	if again != (GoogleMessagesLinkResult{Conversations: 2}) {
		t.Fatalf("second pass %+v", again)
	}
	links, err := db.ListConversationLinks(database)
	if err != nil {
		t.Fatal(err)
	}
	var rooms [][]string
	for _, link := range links {
		rooms = append(rooms, link.RoomIDs)
	}
	want := [][]string{{"!discord:x", "!other-bridge:x", "!other-web:x"}, {"!bridge:x", "!web:x"}}
	if !reflect.DeepEqual(rooms, want) {
		t.Fatalf("links %v, want %v", rooms, want)
	}
}
