package api

import (
	"database/sql"
	"errors"
	"log"

	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/matrix"
)

// GoogleMessagesLinkResult is what one pass of LinkGoogleMessagesTwins did.
type GoogleMessagesLinkResult struct {
	// Conversations is how many Google conversations two or more rooms carry.
	Conversations int
	// Linked is how many of them were not already one link.
	Linked int
	// LeftApart is how many have rooms in two different links.
	LeftApart int
}

// LinkGoogleMessagesTwins links the rooms that carry one Google conversation
// (the mautrix bridge's and the web bridge's, which share Google's
// conversation id), so the Conversations page lists them as one entry with a
// tab per room. The rooms stay separate, so the old bridge's history is a tab
// away. Rooms already in two different links are left apart and logged:
// joining two links is a person's choice.
func (r *Router) LinkGoogleMessagesTwins() (GoogleMessagesLinkResult, error) {
	conversations, err := r.listConversations()
	if err != nil {
		return GoogleMessagesLinkResult{}, err
	}
	return linkGoogleMessagesTwins(r.db, conversations)
}

func linkGoogleMessagesTwins(database *sql.DB, conversations []matrix.Conversation) (GoogleMessagesLinkResult, error) {
	links, err := db.ListConversationLinks(database)
	if err != nil {
		return GoogleMessagesLinkResult{}, err
	}
	linkOf := map[string]int64{}
	for _, link := range links {
		for _, roomID := range link.RoomIDs {
			linkOf[roomID] = link.ID
		}
	}
	var result GoogleMessagesLinkResult
	for googleID, roomIDs := range matrix.GoogleMessagesTwins(conversations) {
		result.Conversations++
		first, together := linkOf[roomIDs[0]], true
		for _, roomID := range roomIDs[1:] {
			together = together && first != 0 && linkOf[roomID] == first
		}
		if together {
			continue
		}
		if _, err := db.LinkConversations(database, roomIDs); err != nil {
			if errors.Is(err, db.ErrRoomsInDifferentLinks) {
				log.Printf("Google Messages conversation %s: rooms %v are in different links; left apart", googleID, roomIDs)
				result.LeftApart++
				continue
			}
			return result, err
		}
		result.Linked++
	}
	return result, nil
}
