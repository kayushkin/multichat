package db

import (
	"database/sql"
	"fmt"
)

// multichat linked contact identities to people in its own tables
// (contact_identity_links, contact_link_refusals, contact_link_suggestions,
// contact_review_claims, contact_reviews) for a few hours on 2026-10-01. Those
// rows were copied to people-store, which owns people, links, suggestions and
// the review queue, by scripts/migrate-contact-links-to-people-store.py. The
// tables stay in multichat.db and nothing reads or writes them.

// ContactUserIDsWeHaveTalkedWith returns every bridge puppet that has sent a
// message into the log: with the one-to-one chat partners, the people worth a
// person of their own.
func ContactUserIDsWeHaveTalkedWith(database *sql.DB) (map[string]bool, error) {
	rows, err := database.Query(`SELECT DISTINCT sender_user_id FROM message_log WHERE sender_user_id != ''`)
	if err != nil {
		return nil, fmt.Errorf("list message senders: %w", err)
	}
	defer rows.Close()
	senders := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("list message senders: %w", err)
		}
		senders[id] = true
	}
	return senders, rows.Err()
}
