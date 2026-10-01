package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// A conversation link says that rooms on different apps are one conversation
// — a group that texts and also talks on Discord. conversation_links holds the
// link's own id; conversation_link_rooms puts each room in at most one link.
// The rooms stay separate: a link changes only how the rooms are listed.
const conversationLinkSchema = `
CREATE TABLE IF NOT EXISTS conversation_links (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS conversation_link_rooms (
	room_id TEXT PRIMARY KEY,
	link_id INTEGER NOT NULL REFERENCES conversation_links(id),
	linked_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS conversation_link_rooms_link_id ON conversation_link_rooms(link_id);
`

func createConversationLinkSchema(database *sql.DB) error {
	if _, err := database.Exec(conversationLinkSchema); err != nil {
		return fmt.Errorf("create conversation link schema: %w", err)
	}
	return nil
}

// ConversationLink is one link and its rooms, sorted by room id.
type ConversationLink struct {
	ID      int64    `json:"id"`
	RoomIDs []string `json:"room_ids"`
}

var (
	ErrTooFewRoomsToLink     = errors.New("room_ids must name at least two different rooms")
	ErrRoomsInDifferentLinks = errors.New("those rooms are already in different links; take one out first")
)

func isMatrixRoomID(roomID string) bool {
	return strings.HasPrefix(roomID, "!") && strings.Contains(roomID, ":")
}

// LinkConversations makes the rooms one conversation. When none is linked yet
// it makes a new link; when some already share a link, the rest join it.
// Rooms already in two different links are ErrRoomsInDifferentLinks, since
// merging two links is a choice for a person to make one room at a time.
func LinkConversations(database *sql.DB, roomIDs []string) (ConversationLink, error) {
	distinct := map[string]bool{}
	for _, roomID := range roomIDs {
		if !isMatrixRoomID(roomID) {
			return ConversationLink{}, fmt.Errorf("%w: %q", ErrInvalidRoomID, roomID)
		}
		distinct[roomID] = true
	}
	if len(distinct) < 2 {
		return ConversationLink{}, ErrTooFewRoomsToLink
	}
	transaction, err := database.Begin()
	if err != nil {
		return ConversationLink{}, err
	}
	defer transaction.Rollback()
	existingLinkIDs := map[int64]bool{}
	for roomID := range distinct {
		var linkID int64
		err := transaction.QueryRow(`SELECT link_id FROM conversation_link_rooms WHERE room_id = ?`, roomID).Scan(&linkID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return ConversationLink{}, err
		}
		existingLinkIDs[linkID] = true
	}
	if len(existingLinkIDs) > 1 {
		return ConversationLink{}, ErrRoomsInDifferentLinks
	}
	var linkID int64
	for existing := range existingLinkIDs {
		linkID = existing
	}
	if linkID == 0 {
		result, err := transaction.Exec(`INSERT INTO conversation_links DEFAULT VALUES`)
		if err != nil {
			return ConversationLink{}, err
		}
		if linkID, err = result.LastInsertId(); err != nil {
			return ConversationLink{}, err
		}
	}
	for roomID := range distinct {
		if _, err := transaction.Exec(`INSERT OR IGNORE INTO conversation_link_rooms (room_id, link_id) VALUES (?, ?)`, roomID, linkID); err != nil {
			return ConversationLink{}, err
		}
	}
	if err := transaction.Commit(); err != nil {
		return ConversationLink{}, err
	}
	links, err := ListConversationLinks(database)
	if err != nil {
		return ConversationLink{}, err
	}
	for _, link := range links {
		if link.ID == linkID {
			return link, nil
		}
	}
	return ConversationLink{}, fmt.Errorf("conversation link %d vanished after it was written", linkID)
}

// UnlinkConversation takes a room out of its link. A link left with one room
// is deleted, since one room is not a link. A room in no link is not an error.
func UnlinkConversation(database *sql.DB, roomID string) error {
	if !isMatrixRoomID(roomID) {
		return fmt.Errorf("%w: %q", ErrInvalidRoomID, roomID)
	}
	transaction, err := database.Begin()
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	var linkID int64
	err = transaction.QueryRow(`SELECT link_id FROM conversation_link_rooms WHERE room_id = ?`, roomID).Scan(&linkID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := transaction.Exec(`DELETE FROM conversation_link_rooms WHERE room_id = ?`, roomID); err != nil {
		return err
	}
	var remaining int
	if err := transaction.QueryRow(`SELECT COUNT(*) FROM conversation_link_rooms WHERE link_id = ?`, linkID).Scan(&remaining); err != nil {
		return err
	}
	if remaining < 2 {
		for _, statement := range []string{
			`DELETE FROM conversation_link_rooms WHERE link_id = ?`,
			`DELETE FROM conversation_links WHERE id = ?`,
		} {
			if _, err := transaction.Exec(statement, linkID); err != nil {
				return err
			}
		}
	}
	return transaction.Commit()
}

// ListConversationLinks returns every link with its rooms, oldest link first.
func ListConversationLinks(database *sql.DB) ([]ConversationLink, error) {
	rows, err := database.Query(`SELECT link_id, room_id FROM conversation_link_rooms ORDER BY link_id, room_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	links := []ConversationLink{}
	for rows.Next() {
		var linkID int64
		var roomID string
		if err := rows.Scan(&linkID, &roomID); err != nil {
			return nil, err
		}
		if len(links) == 0 || links[len(links)-1].ID != linkID {
			links = append(links, ConversationLink{ID: linkID})
		}
		links[len(links)-1].RoomIDs = append(links[len(links)-1].RoomIDs, roomID)
	}
	return links, rows.Err()
}
