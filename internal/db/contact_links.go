package db

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"
)

// A contact identity is one bridge puppet (@discord_…, @gmessages_…). Linking
// says which person it is, by the id principal-store owns; multichat never
// names a person itself. A refusal says two identities are not the same
// person, so no automatic pass links them again. A suggestion is a link an
// agent was not sure enough to make. A review marks an identity the agent pass
// has already looked at.
const contactLinkSchema = `
CREATE TABLE IF NOT EXISTS contact_identity_links (
	contact_user_id TEXT PRIMARY KEY,
	principal_id    TEXT NOT NULL,
	linked_by       TEXT NOT NULL,
	reason          TEXT NOT NULL DEFAULT '',
	linked_at       INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS contact_identity_links_principal ON contact_identity_links(principal_id);
CREATE TABLE IF NOT EXISTS contact_link_refusals (
	first_contact_user_id  TEXT NOT NULL,
	second_contact_user_id TEXT NOT NULL,
	refused_by             TEXT NOT NULL,
	refused_at             INTEGER NOT NULL,
	PRIMARY KEY (first_contact_user_id, second_contact_user_id),
	CHECK (first_contact_user_id < second_contact_user_id)
);
CREATE TABLE IF NOT EXISTS contact_link_suggestions (
	id                    INTEGER PRIMARY KEY AUTOINCREMENT,
	contact_user_id       TEXT NOT NULL,
	other_contact_user_id TEXT NOT NULL,
	reason                TEXT NOT NULL,
	suggested_by          TEXT NOT NULL,
	created_at            INTEGER NOT NULL,
	status                TEXT NOT NULL DEFAULT 'open',
	decided_by            TEXT NOT NULL DEFAULT '',
	decided_at            INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS contact_link_suggestions_open_pair
	ON contact_link_suggestions(contact_user_id, other_contact_user_id) WHERE status = 'open';
CREATE TABLE IF NOT EXISTS contact_review_claims (
	contact_user_id TEXT PRIMARY KEY,
	claimed_by      TEXT NOT NULL,
	claimed_at      INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS contact_reviews (
	contact_user_id TEXT PRIMARY KEY,
	reviewed_by     TEXT NOT NULL,
	reviewed_at     INTEGER NOT NULL
);
`

func createContactLinkSchema(database *sql.DB) error {
	if _, err := database.Exec(contactLinkSchema); err != nil {
		return fmt.Errorf("create contact link schema: %w", err)
	}
	return nil
}

// Who made a link, refusal or decision. The operator's word overrides a
// refusal; the automatic sources never do.
const (
	ContactLinkSourceOperator    = "operator"
	ContactLinkSourcePhoneNumber = "phone_number"
	ContactLinkSourceAgent       = "agent"
)

// ContactLinkSources is the served vocabulary of linked_by values.
var ContactLinkSources = []string{ContactLinkSourceOperator, ContactLinkSourcePhoneNumber, ContactLinkSourceAgent}

// The states of a suggestion.
const (
	ContactLinkSuggestionOpen     = "open"
	ContactLinkSuggestionAccepted = "accepted"
	ContactLinkSuggestionRejected = "rejected"
)

var (
	ErrUnknownContactLinkSource = fmt.Errorf("linked_by must be one of %v", ContactLinkSources)
	ErrContactLinkRefused       = errors.New("these identities were marked as different people; only the operator can link them")
	ErrContactLinkConflict      = errors.New("these identities already belong to two different people; only the operator can merge them")
	ErrUnknownSuggestion        = errors.New("no open suggestion has that id")
)

// ContactIdentityLink is one row of contact_identity_links.
type ContactIdentityLink struct {
	ContactUserID string `json:"contact_user_id"`
	PrincipalID   string `json:"principal_id"`
	LinkedBy      string `json:"linked_by"`
	Reason        string `json:"reason"`
	LinkedAt      int64  `json:"linked_at"`
}

// ContactLinkSuggestion is one row of contact_link_suggestions.
type ContactLinkSuggestion struct {
	ID                 int64  `json:"id"`
	ContactUserID      string `json:"contact_user_id"`
	OtherContactUserID string `json:"other_contact_user_id"`
	Reason             string `json:"reason"`
	SuggestedBy        string `json:"suggested_by"`
	CreatedAt          int64  `json:"created_at"`
	Status             string `json:"status"`
	DecidedBy          string `json:"decided_by,omitempty"`
	DecidedAt          int64  `json:"decided_at,omitempty"`
}

// ListContactIdentityLinks returns every link, keyed by identity.
func ListContactIdentityLinks(database *sql.DB) (map[string]ContactIdentityLink, error) {
	rows, err := database.Query(`SELECT contact_user_id, principal_id, linked_by, reason, linked_at FROM contact_identity_links`)
	if err != nil {
		return nil, fmt.Errorf("list contact identity links: %w", err)
	}
	defer rows.Close()
	links := map[string]ContactIdentityLink{}
	for rows.Next() {
		var link ContactIdentityLink
		if err := rows.Scan(&link.ContactUserID, &link.PrincipalID, &link.LinkedBy, &link.Reason, &link.LinkedAt); err != nil {
			return nil, fmt.Errorf("list contact identity links: %w", err)
		}
		links[link.ContactUserID] = link
	}
	return links, rows.Err()
}

// ContactIdentityLinkOf returns the identity's link, or nil when it has none.
func ContactIdentityLinkOf(database *sql.DB, contactUserID string) (*ContactIdentityLink, error) {
	var link ContactIdentityLink
	err := database.QueryRow(`SELECT contact_user_id, principal_id, linked_by, reason, linked_at FROM contact_identity_links WHERE contact_user_id = ?`, contactUserID).
		Scan(&link.ContactUserID, &link.PrincipalID, &link.LinkedBy, &link.Reason, &link.LinkedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read link of %s: %w", contactUserID, err)
	}
	return &link, nil
}

// ContactUserIDsOfPrincipal returns every identity linked to a person.
func ContactUserIDsOfPrincipal(database *sql.DB, principalID string) ([]string, error) {
	rows, err := database.Query(`SELECT contact_user_id FROM contact_identity_links WHERE principal_id = ? ORDER BY contact_user_id`, principalID)
	if err != nil {
		return nil, fmt.Errorf("list identities of %s: %w", principalID, err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("list identities of %s: %w", principalID, err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func orderedPair(first, second string) (string, string) {
	if first < second {
		return first, second
	}
	return second, first
}

// AnyContactLinkRefused reports whether any identity in one set was marked as
// a different person from any identity in the other.
func AnyContactLinkRefused(database *sql.DB, firstSet, secondSet []string) (bool, error) {
	for _, first := range firstSet {
		for _, second := range secondSet {
			if first == second {
				continue
			}
			low, high := orderedPair(first, second)
			var count int
			if err := database.QueryRow(`SELECT COUNT(*) FROM contact_link_refusals WHERE first_contact_user_id = ? AND second_contact_user_id = ?`, low, high).Scan(&count); err != nil {
				return false, fmt.Errorf("read contact link refusals: %w", err)
			}
			if count > 0 {
				return true, nil
			}
		}
	}
	return false, nil
}

// SetContactIdentityLinks points every identity at one person in a single
// transaction, replacing any link they had.
func SetContactIdentityLinks(database *sql.DB, contactUserIDs []string, principalID, linkedBy, reason string) error {
	if !slices.Contains(ContactLinkSources, linkedBy) {
		return ErrUnknownContactLinkSource
	}
	transaction, err := database.Begin()
	if err != nil {
		return fmt.Errorf("link identities: %w", err)
	}
	defer transaction.Rollback()
	now := time.Now().Unix()
	for _, contactUserID := range contactUserIDs {
		if _, err := transaction.Exec(`INSERT INTO contact_identity_links (contact_user_id, principal_id, linked_by, reason, linked_at) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (contact_user_id) DO UPDATE SET principal_id = excluded.principal_id, linked_by = excluded.linked_by, reason = excluded.reason, linked_at = excluded.linked_at`,
			contactUserID, principalID, linkedBy, reason, now); err != nil {
			return fmt.Errorf("link %s to %s: %w", contactUserID, principalID, err)
		}
	}
	return transaction.Commit()
}

// ClearContactLinkRefusalsBetween forgets that any identity of one set was
// marked as a different person from any of the other: the operator has just
// said they are the same person.
func ClearContactLinkRefusalsBetween(database *sql.DB, firstSet, secondSet []string) error {
	for _, first := range firstSet {
		for _, second := range secondSet {
			if first == second {
				continue
			}
			low, high := orderedPair(first, second)
			if _, err := database.Exec(`DELETE FROM contact_link_refusals WHERE first_contact_user_id = ? AND second_contact_user_id = ?`, low, high); err != nil {
				return fmt.Errorf("clear refusal between %s and %s: %w", first, second, err)
			}
		}
	}
	return nil
}

// RemoveContactIdentityLink unlinks an identity and records that it is not
// the same person as any identity left on that person, so no automatic pass
// links it back. It returns the link removed, or nil when there was none.
func RemoveContactIdentityLink(database *sql.DB, contactUserID, removedBy string) (*ContactIdentityLink, error) {
	if !slices.Contains(ContactLinkSources, removedBy) {
		return nil, ErrUnknownContactLinkSource
	}
	link, err := ContactIdentityLinkOf(database, contactUserID)
	if err != nil || link == nil {
		return nil, err
	}
	others, err := ContactUserIDsOfPrincipal(database, link.PrincipalID)
	if err != nil {
		return nil, err
	}
	transaction, err := database.Begin()
	if err != nil {
		return nil, fmt.Errorf("unlink %s: %w", contactUserID, err)
	}
	defer transaction.Rollback()
	if _, err := transaction.Exec(`DELETE FROM contact_identity_links WHERE contact_user_id = ?`, contactUserID); err != nil {
		return nil, fmt.Errorf("unlink %s: %w", contactUserID, err)
	}
	now := time.Now().Unix()
	for _, other := range others {
		if other == contactUserID {
			continue
		}
		low, high := orderedPair(contactUserID, other)
		if _, err := transaction.Exec(`INSERT INTO contact_link_refusals (first_contact_user_id, second_contact_user_id, refused_by, refused_at) VALUES (?, ?, ?, ?)
			ON CONFLICT DO UPDATE SET refused_by = excluded.refused_by, refused_at = excluded.refused_at`, low, high, removedBy, now); err != nil {
			return nil, fmt.Errorf("refuse %s with %s: %w", contactUserID, other, err)
		}
	}
	return link, transaction.Commit()
}

// RefuseContactLink records that two identities are not the same person.
func RefuseContactLink(database *sql.DB, first, second, refusedBy string) error {
	low, high := orderedPair(first, second)
	_, err := database.Exec(`INSERT INTO contact_link_refusals (first_contact_user_id, second_contact_user_id, refused_by, refused_at) VALUES (?, ?, ?, ?)
		ON CONFLICT DO UPDATE SET refused_by = excluded.refused_by, refused_at = excluded.refused_at`, low, high, refusedBy, time.Now().Unix())
	if err != nil {
		return fmt.Errorf("refuse %s with %s: %w", first, second, err)
	}
	return nil
}

// AddContactLinkSuggestion stores an open suggestion. The same open pair
// twice keeps the first and returns it.
func AddContactLinkSuggestion(database *sql.DB, contactUserID, otherContactUserID, reason, suggestedBy string) (ContactLinkSuggestion, error) {
	low, high := orderedPair(contactUserID, otherContactUserID)
	if _, err := database.Exec(`INSERT INTO contact_link_suggestions (contact_user_id, other_contact_user_id, reason, suggested_by, created_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING`, low, high, reason, suggestedBy, time.Now().Unix()); err != nil {
		return ContactLinkSuggestion{}, fmt.Errorf("add suggestion: %w", err)
	}
	suggestions, err := listContactLinkSuggestions(database, `WHERE contact_user_id = ? AND other_contact_user_id = ? AND status = 'open'`, low, high)
	if err != nil {
		return ContactLinkSuggestion{}, err
	}
	return suggestions[0], nil
}

// ListOpenContactLinkSuggestions returns the open suggestions, oldest first.
func ListOpenContactLinkSuggestions(database *sql.DB) ([]ContactLinkSuggestion, error) {
	return listContactLinkSuggestions(database, `WHERE status = 'open'`)
}

// OpenContactLinkSuggestion returns an open suggestion, or ErrUnknownSuggestion.
func OpenContactLinkSuggestion(database *sql.DB, id int64) (ContactLinkSuggestion, error) {
	suggestions, err := listContactLinkSuggestions(database, `WHERE id = ? AND status = 'open'`, id)
	if err != nil {
		return ContactLinkSuggestion{}, err
	}
	if len(suggestions) == 0 {
		return ContactLinkSuggestion{}, ErrUnknownSuggestion
	}
	return suggestions[0], nil
}

func listContactLinkSuggestions(database *sql.DB, where string, arguments ...any) ([]ContactLinkSuggestion, error) {
	rows, err := database.Query(`SELECT id, contact_user_id, other_contact_user_id, reason, suggested_by, created_at, status, decided_by, decided_at
		FROM contact_link_suggestions `+where+` ORDER BY id`, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list suggestions: %w", err)
	}
	defer rows.Close()
	suggestions := []ContactLinkSuggestion{}
	for rows.Next() {
		var s ContactLinkSuggestion
		if err := rows.Scan(&s.ID, &s.ContactUserID, &s.OtherContactUserID, &s.Reason, &s.SuggestedBy, &s.CreatedAt, &s.Status, &s.DecidedBy, &s.DecidedAt); err != nil {
			return nil, fmt.Errorf("list suggestions: %w", err)
		}
		suggestions = append(suggestions, s)
	}
	return suggestions, rows.Err()
}

// DecideContactLinkSuggestion closes an open suggestion as accepted or
// rejected. Rejecting also records a refusal between the pair.
func DecideContactLinkSuggestion(database *sql.DB, id int64, status, decidedBy string) error {
	suggestion, err := OpenContactLinkSuggestion(database, id)
	if err != nil {
		return err
	}
	if _, err := database.Exec(`UPDATE contact_link_suggestions SET status = ?, decided_by = ?, decided_at = ? WHERE id = ?`, status, decidedBy, time.Now().Unix(), id); err != nil {
		return fmt.Errorf("decide suggestion %d: %w", id, err)
	}
	if status == ContactLinkSuggestionRejected {
		return RefuseContactLink(database, suggestion.ContactUserID, suggestion.OtherContactUserID, decidedBy)
	}
	return nil
}

// ReviewedContactUserIDs returns the identities the agent pass has looked at.
func ReviewedContactUserIDs(database *sql.DB) (map[string]bool, error) {
	rows, err := database.Query(`SELECT contact_user_id FROM contact_reviews`)
	if err != nil {
		return nil, fmt.Errorf("list contact reviews: %w", err)
	}
	defer rows.Close()
	reviewed := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("list contact reviews: %w", err)
		}
		reviewed[id] = true
	}
	return reviewed, rows.Err()
}

// MarkContactsReviewed records that the agent pass has looked at identities.
func MarkContactsReviewed(database *sql.DB, contactUserIDs []string, reviewedBy string) error {
	transaction, err := database.Begin()
	if err != nil {
		return fmt.Errorf("mark reviewed: %w", err)
	}
	defer transaction.Rollback()
	now := time.Now().Unix()
	for _, id := range contactUserIDs {
		if _, err := transaction.Exec(`INSERT INTO contact_reviews (contact_user_id, reviewed_by, reviewed_at) VALUES (?, ?, ?)
			ON CONFLICT (contact_user_id) DO UPDATE SET reviewed_by = excluded.reviewed_by, reviewed_at = excluded.reviewed_at`, id, reviewedBy, now); err != nil {
			return fmt.Errorf("mark %s reviewed: %w", id, err)
		}
	}
	return transaction.Commit()
}

// ContactUserIDsWeHaveTalkedWith returns every bridge puppet that has sent a
// message into the log: the people the agent pass is worth spending on.
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

// ContactReviewClaimLifetime is how long a claim keeps an identity out of
// other reviewers' batches. A session that dies mid-batch lets go when it
// runs out.
const ContactReviewClaimLifetime = 30 * 60

// ActiveContactReviewClaims returns the identities claimed within the claim
// lifetime and not reviewed since.
func ActiveContactReviewClaims(database *sql.DB) (map[string]bool, error) {
	rows, err := database.Query(`SELECT claim.contact_user_id FROM contact_review_claims AS claim
		LEFT JOIN contact_reviews AS review ON review.contact_user_id = claim.contact_user_id AND review.reviewed_at >= claim.claimed_at
		WHERE claim.claimed_at > ? AND review.contact_user_id IS NULL`, time.Now().Unix()-ContactReviewClaimLifetime)
	if err != nil {
		return nil, fmt.Errorf("list contact review claims: %w", err)
	}
	defer rows.Close()
	claimed := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("list contact review claims: %w", err)
		}
		claimed[id] = true
	}
	return claimed, rows.Err()
}

// ClaimContactsForReview records that a reviewer is working on identities.
func ClaimContactsForReview(database *sql.DB, contactUserIDs []string, claimedBy string) error {
	transaction, err := database.Begin()
	if err != nil {
		return fmt.Errorf("claim contacts: %w", err)
	}
	defer transaction.Rollback()
	now := time.Now().Unix()
	for _, id := range contactUserIDs {
		if _, err := transaction.Exec(`INSERT INTO contact_review_claims (contact_user_id, claimed_by, claimed_at) VALUES (?, ?, ?)
			ON CONFLICT (contact_user_id) DO UPDATE SET claimed_by = excluded.claimed_by, claimed_at = excluded.claimed_at`, id, claimedBy, now); err != nil {
			return fmt.Errorf("claim %s: %w", id, err)
		}
	}
	return transaction.Commit()
}
