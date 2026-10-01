// Package contactlinks decides which bridge accounts are one person. A person
// is a people-store id (person_000001); each bridge account is a people-store
// identity of kind matrix_user, and people-store records who linked it and
// why. The operator's picker, the phone-number matcher and the review agent
// all link through Linker. people-store enforces the rules the automatic
// sources obey — they never relink a handle the operator took off a person,
// and never take a handle from one person to give it to another — and Linker
// adds the one people-store cannot see: an automatic source never merges two
// people who both have ids already.
package contactlinks

import (
	"errors"
	"fmt"

	"github.com/kayushkin/multichat/internal/matrix"
	"github.com/kayushkin/multichat/internal/people"
)

// The link sources multichat writes as. people-store owns the vocabulary
// (GET /link-sources); these are the two whose meaning multichat acts on.
const (
	// SourceOperator is the operator's own word: it overrides refusals and may
	// move an account from one person to another.
	SourceOperator = "operator"
	// SourcePhoneNumber is the phone-number matcher.
	SourcePhoneNumber = "phone_number"
)

var (
	// ErrAlreadyTwoPeople is an automatic source asking to join two accounts
	// that already belong to different people. Only the operator merges.
	ErrAlreadyTwoPeople = errors.New("these accounts already belong to two different people; only the operator can move one")
	// ErrUnknownPerson is a link to a person id people-store does not know.
	ErrUnknownPerson = errors.New("people-store has no person with that id")
)

// PeopleStore is the part of people-store linking needs.
type PeopleStore interface {
	OwnerOf(userID string) (*people.Identity, error)
	MatrixUserIdentities() (map[string]people.Identity, error)
	Person(id string) (people.Person, error)
	CreatePerson(displayName string, links []people.Link) (string, error)
	AddIdentity(personID string, link people.Link) (people.Identity, error)
	RemoveIdentity(identityID string) error
}

// Linker links bridge accounts to people.
type Linker struct {
	People PeopleStore
}

func link(userID, linkedBy, reason string) people.Link {
	return people.MatrixUserLink(userID, matrix.PlatformOfUserID(userID), linkedBy, reason)
}

func personOf(identity *people.Identity) string {
	if identity == nil {
		return ""
	}
	return identity.PersonID
}

// LinkSamePerson records that two accounts are one person and returns that
// person's id. An account with no person joins the other's; when neither has
// one, a person named newPersonDisplayName is made with both. When both
// already have different people, only the operator may link them, and then
// userID moves to otherUserID's person.
func (l Linker) LinkSamePerson(userID, otherUserID, newPersonDisplayName, linkedBy, reason string) (string, error) {
	if userID == otherUserID {
		return "", errors.New("an account cannot be linked to itself")
	}
	first, err := l.People.OwnerOf(userID)
	if err != nil {
		return "", err
	}
	second, err := l.People.OwnerOf(otherUserID)
	if err != nil {
		return "", err
	}
	firstPerson, secondPerson := personOf(first), personOf(second)
	switch {
	case firstPerson != "" && firstPerson == secondPerson:
		return firstPerson, nil
	case firstPerson != "" && secondPerson != "":
		if linkedBy != SourceOperator {
			return "", ErrAlreadyTwoPeople
		}
		moved := link(userID, linkedBy, reason)
		moved.Move = true
		_, err := l.People.AddIdentity(secondPerson, moved)
		return secondPerson, err
	case firstPerson != "":
		_, err := l.People.AddIdentity(firstPerson, link(otherUserID, linkedBy, reason))
		return firstPerson, err
	case secondPerson != "":
		_, err := l.People.AddIdentity(secondPerson, link(userID, linkedBy, reason))
		return secondPerson, err
	default:
		return l.People.CreatePerson(newPersonDisplayName, []people.Link{link(userID, linkedBy, reason), link(otherUserID, linkedBy, reason)})
	}
}

// LinkToPerson points one account at a person who already exists, such as one
// with no chat account yet. An account that has a person already moves only
// when the operator says so.
func (l Linker) LinkToPerson(userID, personID, linkedBy, reason string) error {
	if _, err := l.People.Person(personID); err != nil {
		if errors.Is(err, people.ErrNotFound) {
			return fmt.Errorf("%w: %s", ErrUnknownPerson, personID)
		}
		return err
	}
	current, err := l.People.OwnerOf(userID)
	if err != nil {
		return err
	}
	if personOf(current) == personID {
		return nil
	}
	added := link(userID, linkedBy, reason)
	if current != nil {
		if linkedBy != SourceOperator {
			return ErrAlreadyTwoPeople
		}
		added.Move = true
	}
	_, err = l.People.AddIdentity(personID, added)
	return err
}

// Unlink takes an account off its person and returns the identity it had, or
// nil when it had none. people-store records that the account is not that
// person, so no automatic source links it back.
func (l Linker) Unlink(userID string) (*people.Identity, error) {
	current, err := l.People.OwnerOf(userID)
	if err != nil || current == nil {
		return nil, err
	}
	return current, l.People.RemoveIdentity(current.ID)
}
