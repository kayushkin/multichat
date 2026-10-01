package contactlinks

import (
	"errors"
	"fmt"
	"testing"

	"github.com/kayushkin/multichat/internal/matrix"
	"github.com/kayushkin/multichat/internal/people"
)

// fakePeopleStore keeps the rules people-store enforces that matter here: one
// handle per person, refusals for every source but the operator, a move only
// from the operator, and new persons with their handles all or nothing.
type fakePeopleStore struct {
	names      map[string]string
	identities map[string]people.Identity // by user id
	refused    map[string]bool            // user id + person id
	created    int
}

func newFakePeopleStore() *fakePeopleStore {
	return &fakePeopleStore{names: map[string]string{}, identities: map[string]people.Identity{}, refused: map[string]bool{}}
}

func (f *fakePeopleStore) OwnerOf(userID string) (*people.Identity, error) {
	identity, ok := f.identities[userID]
	if !ok {
		return nil, nil
	}
	return &identity, nil
}

func (f *fakePeopleStore) MatrixUserIdentities() (map[string]people.Identity, error) {
	copied := map[string]people.Identity{}
	for userID, identity := range f.identities {
		copied[userID] = identity
	}
	return copied, nil
}

func (f *fakePeopleStore) Person(id string) (people.Person, error) {
	name, ok := f.names[id]
	if !ok {
		return people.Person{}, people.ErrNotFound
	}
	return people.Person{ID: id, DisplayName: name}, nil
}

func (f *fakePeopleStore) CreatePerson(displayName string, links []people.Link) (string, error) {
	for _, link := range links {
		if _, owned := f.identities[link.Value]; owned {
			return "", fmt.Errorf("%w: %s is owned", people.ErrConflict, link.Value)
		}
	}
	f.created++
	id := fmt.Sprintf("person_%06d", f.created)
	f.names[id] = displayName
	for _, link := range links {
		f.identities[link.Value] = people.Identity{ID: "identity_" + link.Value, PersonID: id, Kind: link.Kind, Value: link.Value, Label: link.Label, LinkedBy: link.LinkedBy, Reason: link.Reason}
	}
	return id, nil
}

func (f *fakePeopleStore) AddIdentity(personID string, link people.Link) (people.Identity, error) {
	if owner, owned := f.identities[link.Value]; owned {
		if owner.PersonID == personID {
			return owner, nil
		}
		if !link.Move || link.LinkedBy != SourceOperator {
			return people.Identity{}, fmt.Errorf("%w: owned by %s", people.ErrConflict, owner.PersonID)
		}
		f.refused[link.Value+owner.PersonID] = true
	}
	if f.refused[link.Value+personID] {
		if link.LinkedBy != SourceOperator {
			return people.Identity{}, fmt.Errorf("%w: refused", people.ErrConflict)
		}
		delete(f.refused, link.Value+personID)
	}
	identity := people.Identity{ID: "identity_" + link.Value, PersonID: personID, Kind: link.Kind, Value: link.Value, Label: link.Label, LinkedBy: link.LinkedBy, Reason: link.Reason}
	f.identities[link.Value] = identity
	return identity, nil
}

func (f *fakePeopleStore) RemoveIdentity(identityID string) error {
	for userID, identity := range f.identities {
		if identity.ID == identityID {
			f.refused[userID+identity.PersonID] = true
			delete(f.identities, userID)
			return nil
		}
	}
	return people.ErrNotFound
}

func (f *fakePeopleStore) personOf(userID string) string { return f.identities[userID].PersonID }

const (
	texts   = "@gmessages_1.630:x"
	discord = "@discord_4939:x"
	whatsUp = "@whatsapp_15550100002:x"
)

func TestLinkingTwoUnlinkedAccountsCreatesOnePerson(t *testing.T) {
	store := newFakePeopleStore()
	linker := Linker{People: store}
	personID, err := linker.LinkSamePerson(texts, discord, "Priya", "agent", "she signs her texts Zora")
	if err != nil {
		t.Fatal(err)
	}
	if store.names[personID] != "Priya" || store.personOf(texts) != personID || store.personOf(discord) != personID {
		t.Fatalf("got %s named %q; texts→%s discord→%s", personID, store.names[personID], store.personOf(texts), store.personOf(discord))
	}
	if identity := store.identities[discord]; identity.Label != "discord" || identity.LinkedBy != "agent" || identity.Reason != "she signs her texts Zora" {
		t.Errorf("identity = %+v", identity)
	}
	again, err := linker.LinkSamePerson(discord, texts, "ignored", SourcePhoneNumber, "again")
	if err != nil || again != personID || store.created != 1 {
		t.Fatalf("linking the same pair again: %s %v, %d people", again, err, store.created)
	}
	if _, err := linker.LinkSamePerson(whatsUp, texts, "ignored", SourcePhoneNumber, "same number"); err != nil {
		t.Fatal(err)
	}
	if store.personOf(whatsUp) != personID || store.created != 1 {
		t.Fatalf("a third account joins the existing person, not a new one")
	}
}

func TestOnlyTheOperatorMovesAnAccountBetweenPeople(t *testing.T) {
	store := newFakePeopleStore()
	linker := Linker{People: store}
	first, _ := linker.LinkSamePerson(texts, whatsUp, "Ada", SourcePhoneNumber, "same number")
	second, _ := store.CreatePerson("Twili", []people.Link{people.MatrixUserLink(discord, "discord", "agent", "r")})
	if _, err := linker.LinkSamePerson(texts, discord, "x", "agent", "a guess"); !errors.Is(err, ErrAlreadyTwoPeople) {
		t.Errorf("agent merging two people = %v", err)
	}
	if err := linker.LinkToPerson(texts, second, SourcePhoneNumber, "r"); !errors.Is(err, ErrAlreadyTwoPeople) {
		t.Errorf("automatic move = %v", err)
	}
	if err := linker.LinkToPerson(texts, second, SourceOperator, "the operator says so"); err != nil {
		t.Fatal(err)
	}
	if store.personOf(texts) != second || store.personOf(whatsUp) != first {
		t.Errorf("after the operator's move: texts→%s whatsapp→%s", store.personOf(texts), store.personOf(whatsUp))
	}
	if err := linker.LinkToPerson(texts, "person_999999", SourceOperator, "r"); !errors.Is(err, ErrUnknownPerson) {
		t.Errorf("unknown person = %v", err)
	}
}

func TestAnUnlinkedAccountIsNotLinkedBackByThePhoneMatcher(t *testing.T) {
	store := newFakePeopleStore()
	linker := Linker{People: store}
	personID, _ := linker.LinkSamePerson(texts, whatsUp, "Ada", SourcePhoneNumber, "same number")
	removed, err := linker.Unlink(texts)
	if err != nil || removed == nil || removed.PersonID != personID {
		t.Fatalf("unlink = %+v, %v", removed, err)
	}
	if _, err := linker.LinkSamePerson(texts, whatsUp, "Ada", SourcePhoneNumber, "same number"); !errors.Is(err, people.ErrConflict) {
		t.Errorf("relinking after the operator separated them = %v", err)
	}
	if _, err := linker.LinkSamePerson(texts, whatsUp, "Ada", SourceOperator, "linked by the operator in dash"); err != nil {
		t.Errorf("the operator's own link = %v", err)
	}
	if nothing, err := linker.Unlink(discord); err != nil || nothing != nil {
		t.Errorf("unlinking an account with no person = %+v, %v", nothing, err)
	}
}

func TestThePhoneMatcherMakesPeopleOnlyForThoseWeTalkWith(t *testing.T) {
	store := newFakePeopleStore()
	linker := Linker{People: store}
	contacts := []matrix.Contact{
		{UserID: "@whatsapp_15550001:x", DisplayName: "Stranger", Platform: "whatsapp"},
		{UserID: "@whatsapp_15550002:x", DisplayName: "Friend", Platform: "whatsapp"},
		{UserID: "@whatsapp_15550003:x", DisplayName: "Known", Platform: "whatsapp"},
	}
	// Three numbers, each on one WhatsApp puppet; a second puppet per number
	// comes from the same digits under a LID id the test adds by hand.
	byNumber := map[string][]string{}
	whatsAppPhoneNumberPuppets(byNumber, contacts)
	if len(byNumber) != 3 {
		t.Fatalf("puppets by number = %v", byNumber)
	}
	contacts = append(contacts,
		matrix.Contact{UserID: "@gmessages_1.1:x", DisplayName: "15550001", Platform: "gmessages"},
		matrix.Contact{UserID: "@gmessages_1.2:x", DisplayName: "Friend", Platform: "gmessages"},
		matrix.Contact{UserID: "@gmessages_1.3:x", DisplayName: "Known", Platform: "gmessages"},
	)
	known, _ := store.CreatePerson("Known", []people.Link{people.MatrixUserLink("@whatsapp_15550003:x", "whatsapp", SourceOperator, "")})
	result, err := linker.linkByPhoneNumber(map[string][]string{
		"15550001": {"@whatsapp_15550001:x", "@gmessages_1.1:x"},
		"15550002": {"@whatsapp_15550002:x", "@gmessages_1.2:x"},
		"15550003": {"@whatsapp_15550003:x", "@gmessages_1.3:x"},
	}, contacts, map[string]bool{}, map[string]bool{"@gmessages_1.2:x": true})
	if err != nil {
		t.Fatal(err)
	}
	if store.personOf("@whatsapp_15550001:x") != "" {
		t.Errorf("strangers nobody talks with got a person")
	}
	friend := store.personOf("@gmessages_1.2:x")
	if friend == "" || store.personOf("@whatsapp_15550002:x") != friend || store.names[friend] != "Friend" {
		t.Errorf("friend → %q named %q", friend, store.names[friend])
	}
	if store.personOf("@gmessages_1.3:x") != known {
		t.Errorf("an account sharing a number with a person did not join them")
	}
	if result.Linked != 3 || result.Refused != 0 || result.Skipped != 0 {
		t.Errorf("result = %+v", result)
	}
}
