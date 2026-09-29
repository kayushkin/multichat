package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Tags are multichat's own labels. contact_tags puts one on a person's
// identity; conversation_tags puts one on a Matrix room. SQLite here runs
// without foreign keys, so deleting a tag must clear both by hand
// (DeleteTagAndAssignments).
const tagSchema = `
CREATE TABLE IF NOT EXISTS tags (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL UNIQUE,
	color TEXT NOT NULL DEFAULT '#f5d40c',
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS contact_tags (
	contact_user_id TEXT NOT NULL,
	tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
	PRIMARY KEY (contact_user_id, tag_id)
);
CREATE TABLE IF NOT EXISTS conversation_tags (
	room_id TEXT NOT NULL,
	tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
	created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (room_id, tag_id)
);
`

func createTagSchema(database *sql.DB) error {
	if _, err := database.Exec(tagSchema); err != nil {
		return fmt.Errorf("create tag schema: %w", err)
	}
	return nil
}

// Tag is one row of tags.
type Tag struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

var (
	ErrUnknownTag    = errors.New("no tag has that id")
	ErrInvalidRoomID = errors.New("room_id must be a Matrix room id (\"!…:server\")")
)

// AssignConversationTag puts a tag on a room. Assigning it twice is not an
// error. A tag id that does not exist is ErrUnknownTag.
func AssignConversationTag(database *sql.DB, roomID string, tagID int64) error {
	if !strings.HasPrefix(roomID, "!") || !strings.Contains(roomID, ":") {
		return ErrInvalidRoomID
	}
	var exists int
	if err := database.QueryRow(`SELECT COUNT(*) FROM tags WHERE id = ?`, tagID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return fmt.Errorf("%w: %d", ErrUnknownTag, tagID)
	}
	_, err := database.Exec(`INSERT OR IGNORE INTO conversation_tags (room_id, tag_id) VALUES (?, ?)`, roomID, tagID)
	return err
}

// RemoveConversationTag takes a tag off a room. Removing one the room does not
// have is not an error.
func RemoveConversationTag(database *sql.DB, roomID string, tagID int64) error {
	_, err := database.Exec(`DELETE FROM conversation_tags WHERE room_id = ? AND tag_id = ?`, roomID, tagID)
	return err
}

// ConversationTagMap returns every room's tags, keyed by room id, each list
// sorted by name. A room with no tags is absent.
func ConversationTagMap(database *sql.DB) (map[string][]Tag, error) {
	rows, err := database.Query(`SELECT ct.room_id, t.id, t.name, t.color FROM conversation_tags ct JOIN tags t ON ct.tag_id = t.id ORDER BY ct.room_id, t.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string][]Tag{}
	for rows.Next() {
		var roomID string
		var tag Tag
		if err := rows.Scan(&roomID, &tag.ID, &tag.Name, &tag.Color); err != nil {
			return nil, err
		}
		result[roomID] = append(result[roomID], tag)
	}
	return result, rows.Err()
}

// DeleteTagAndAssignments deletes a tag and takes it off every contact and
// every room, in one transaction.
func DeleteTagAndAssignments(database *sql.DB, tagID int64) error {
	transaction, err := database.Begin()
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	for _, statement := range []string{
		`DELETE FROM contact_tags WHERE tag_id = ?`,
		`DELETE FROM conversation_tags WHERE tag_id = ?`,
		`DELETE FROM tags WHERE id = ?`,
	} {
		if _, err := transaction.Exec(statement, tagID); err != nil {
			return err
		}
	}
	return transaction.Commit()
}
