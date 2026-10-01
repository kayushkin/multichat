package contactlinks

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/principals"
)

type fakePrincipalStore struct {
	names map[string]string
}

func (f *fakePrincipalStore) CreateContact(displayName string) (string, error) {
	id := fmt.Sprintf("principal_%06d", len(f.names)+1)
	f.names[id] = displayName
	return id, nil
}

func (f *fakePrincipalStore) Get(id string) (principals.Principal, error) {
	name, ok := f.names[id]
	if !ok {
		return principals.Principal{}, principals.ErrUnknownPrincipal
	}
	return principals.Principal{ID: id, DisplayName: name}, nil
}

func newLinker(t *testing.T) (Linker, *fakePrincipalStore, *sql.DB) {
	database, err := db.Initialize(filepath.Join(t.TempDir(), "multichat.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	store := &fakePrincipalStore{names: map[string]string{}}
	return Linker{Database: database, Principals: store}, store, database
}

const (
	texts   = "@gmessages_1.630:x"
	discord = "@discord_4939:x"
	whatsUp = "@whatsapp_15550100002:x"
)

func principalOf(t *testing.T, database *sql.DB, userID string) string {
	t.Helper()
	link, err := db.ContactIdentityLinkOf(database, userID)
	if err != nil {
		t.Fatal(err)
	}
	if link == nil {
		return ""
	}
	return link.PrincipalID
}

func TestLinkingTwoUnlinkedIdentitiesCreatesOnePerson(t *testing.T) {
	linker, store, database := newLinker(t)
	principalID, err := linker.LinkSamePerson(texts, discord, "Priya", db.ContactLinkSourceAgent, "she signs her texts Zora")
	if err != nil {
		t.Fatal(err)
	}
	if store.names[principalID] != "Priya" || principalOf(t, database, texts) != principalID || principalOf(t, database, discord) != principalID {
		t.Fatalf("got %s named %q; texts→%s discord→%s", principalID, store.names[principalID], principalOf(t, database, texts), principalOf(t, database, discord))
	}
	again, err := linker.LinkSamePerson(discord, texts, "ignored", db.ContactLinkSourcePhoneNumber, "again")
	if err != nil || again != principalID || len(store.names) != 1 {
		t.Fatalf("linking the same pair again: %s %v, %d people", again, err, len(store.names))
	}
	if _, err := linker.LinkSamePerson(whatsUp, texts, "ignored", db.ContactLinkSourcePhoneNumber, "same number"); err != nil {
		t.Fatal(err)
	}
	if principalOf(t, database, whatsUp) != principalID || len(store.names) != 1 {
		t.Fatalf("a third identity joins the existing person, not a new one")
	}
}

func TestAnUnlinkIsARefusalThatOnlyTheOperatorOverrides(t *testing.T) {
	linker, _, database := newLinker(t)
	principalID, err := linker.LinkSamePerson(texts, discord, "Priya", db.ContactLinkSourceAgent, "guess")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.RemoveContactIdentityLink(database, discord, db.ContactLinkSourceOperator); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{db.ContactLinkSourceAgent, db.ContactLinkSourcePhoneNumber} {
		if _, err := linker.LinkSamePerson(discord, texts, "x", source, "again"); !errors.Is(err, db.ErrContactLinkRefused) {
			t.Errorf("%s relinking an unlinked pair: got %v, want a refusal", source, err)
		}
		if err := linker.LinkToPrincipal(discord, principalID, source, "again"); !errors.Is(err, db.ErrContactLinkRefused) {
			t.Errorf("%s relinking to the person: got %v, want a refusal", source, err)
		}
	}
	if _, err := linker.LinkSamePerson(discord, texts, "x", db.ContactLinkSourceOperator, "it is her"); err != nil {
		t.Fatalf("the operator overrides a refusal: %v", err)
	}
	if principalOf(t, database, discord) != principalID {
		t.Fatal("the operator's link joins the existing person")
	}
	if refused, _ := db.AnyContactLinkRefused(database, []string{discord}, []string{texts}); refused {
		t.Fatal("the operator's link clears the refusal")
	}
}

func TestOnlyTheOperatorMergesTwoPeople(t *testing.T) {
	linker, _, database := newLinker(t)
	first, _ := linker.LinkSamePerson(texts, whatsUp, "Priya", db.ContactLinkSourcePhoneNumber, "same number")
	second, _ := linker.LinkSamePerson(discord, "@telegram_9:x", "Zora", db.ContactLinkSourceAgent, "same handle")
	if _, err := linker.LinkSamePerson(texts, discord, "x", db.ContactLinkSourceAgent, "merge"); !errors.Is(err, db.ErrContactLinkConflict) {
		t.Fatalf("an agent merging two people: got %v, want a conflict", err)
	}
	merged, err := linker.LinkSamePerson(texts, discord, "x", db.ContactLinkSourceOperator, "merge")
	if err != nil || merged != first {
		t.Fatalf("operator merge: %s %v", merged, err)
	}
	for _, userID := range []string{texts, whatsUp, discord, "@telegram_9:x"} {
		if principalOf(t, database, userID) != first {
			t.Errorf("%s is on %s, want every identity of %s moved to %s", userID, principalOf(t, database, userID), second, first)
		}
	}
}

func TestAnUnknownSourceCreatesNoOne(t *testing.T) {
	linker, store, _ := newLinker(t)
	if _, err := linker.LinkSamePerson(texts, discord, "Priya", "someone", "why"); !errors.Is(err, db.ErrUnknownContactLinkSource) {
		t.Fatalf("got %v", err)
	}
	if len(store.names) != 0 {
		t.Fatal("a refused link must not leave a person behind in principal-store")
	}
}
