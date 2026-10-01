// Package contactlinks decides which bridge identities are one person. A
// person is a principal-store id; multichat stores which identities point at
// it. The operator, the phone-number matcher and the agent pass all link
// through Linker, so they obey the same rules: an automatic source never
// overrides the operator's "these are different people", and never merges two
// people who already have separate ids.
package contactlinks

import (
	"database/sql"
	"errors"
	"slices"

	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/principals"
)

// PrincipalStore is the part of principal-store linking needs.
type PrincipalStore interface {
	CreateContact(displayName string) (string, error)
	Get(id string) (principals.Principal, error)
}

// Linker links identities to people.
type Linker struct {
	Database   *sql.DB
	Principals PrincipalStore
}

// groupOf returns the person an identity is linked to ("" for none) and every
// identity on that person, or just the identity itself when unlinked.
func (l Linker) groupOf(contactUserID string) (string, []string, error) {
	link, err := db.ContactIdentityLinkOf(l.Database, contactUserID)
	if err != nil {
		return "", nil, err
	}
	if link == nil {
		return "", []string{contactUserID}, nil
	}
	group, err := db.ContactUserIDsOfPrincipal(l.Database, link.PrincipalID)
	return link.PrincipalID, group, err
}

// LinkSamePerson records that two identities are one person and returns that
// person's id. When neither is linked yet, a new contact is created in
// principal-store under newPersonDisplayName. When both already belong to
// different people, only the operator may merge them: every identity of the
// second person moves to the first.
func (l Linker) LinkSamePerson(contactUserID, otherContactUserID, newPersonDisplayName, linkedBy, reason string) (string, error) {
	if !slices.Contains(db.ContactLinkSources, linkedBy) {
		return "", db.ErrUnknownContactLinkSource
	}
	if contactUserID == otherContactUserID {
		return "", errors.New("an identity cannot be linked to itself")
	}
	firstPrincipal, firstGroup, err := l.groupOf(contactUserID)
	if err != nil {
		return "", err
	}
	secondPrincipal, secondGroup, err := l.groupOf(otherContactUserID)
	if err != nil {
		return "", err
	}
	if firstPrincipal != "" && firstPrincipal == secondPrincipal {
		return firstPrincipal, nil
	}
	if err := l.checkAllowed(firstGroup, secondGroup, linkedBy); err != nil {
		return "", err
	}
	if firstPrincipal != "" && secondPrincipal != "" && linkedBy != db.ContactLinkSourceOperator {
		return "", db.ErrContactLinkConflict
	}

	target, moving := firstPrincipal, secondGroup
	switch {
	case firstPrincipal != "":
	case secondPrincipal != "":
		target, moving = secondPrincipal, firstGroup
	default:
		target, err = l.Principals.CreateContact(newPersonDisplayName)
		if err != nil {
			return "", err
		}
		moving = []string{contactUserID, otherContactUserID}
	}
	if err := db.SetContactIdentityLinks(l.Database, moving, target, linkedBy, reason); err != nil {
		return "", err
	}
	if linkedBy == db.ContactLinkSourceOperator {
		if err := db.ClearContactLinkRefusalsBetween(l.Database, firstGroup, secondGroup); err != nil {
			return "", err
		}
	}
	return target, nil
}

// LinkToPrincipal points one identity at a person that already exists in
// principal-store, such as one with no chat identity yet.
func (l Linker) LinkToPrincipal(contactUserID, principalID, linkedBy, reason string) error {
	if !slices.Contains(db.ContactLinkSources, linkedBy) {
		return db.ErrUnknownContactLinkSource
	}
	if _, err := l.Principals.Get(principalID); err != nil {
		return err
	}
	currentPrincipal, _, err := l.groupOf(contactUserID)
	if err != nil {
		return err
	}
	if currentPrincipal == principalID {
		return nil
	}
	if currentPrincipal != "" && linkedBy != db.ContactLinkSourceOperator {
		return db.ErrContactLinkConflict
	}
	targetGroup, err := db.ContactUserIDsOfPrincipal(l.Database, principalID)
	if err != nil {
		return err
	}
	if err := l.checkAllowed([]string{contactUserID}, targetGroup, linkedBy); err != nil {
		return err
	}
	if err := db.SetContactIdentityLinks(l.Database, []string{contactUserID}, principalID, linkedBy, reason); err != nil {
		return err
	}
	if linkedBy == db.ContactLinkSourceOperator {
		return db.ClearContactLinkRefusalsBetween(l.Database, []string{contactUserID}, targetGroup)
	}
	return nil
}

func (l Linker) checkAllowed(firstGroup, secondGroup []string, linkedBy string) error {
	if linkedBy == db.ContactLinkSourceOperator {
		return nil
	}
	refused, err := db.AnyContactLinkRefused(l.Database, firstGroup, secondGroup)
	if err != nil {
		return err
	}
	if refused {
		return db.ErrContactLinkRefused
	}
	return nil
}
