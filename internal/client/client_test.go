package client

import (
	"strings"
	"testing"
)

func TestResolveContactIsExactAndRefusesToGuess(t *testing.T) {
	contacts := []UnifiedContact{
		{DisplayName: "Alex Morgan", Platforms: []string{"whatsapp", "meta"}, Identities: []Identity{{UserID: "@whatsapp_1:x", Platform: "whatsapp"}, {UserID: "@meta_1:x", Platform: "meta"}}},
		{DisplayName: "Ana", Identities: []Identity{{UserID: "@telegram_2:x", Platform: "telegram"}}},
		{DisplayName: "ana", Identities: []Identity{{UserID: "@telegram_3:x", Platform: "telegram"}}},
		{PersonID: "person_000040", DisplayName: "Priya", Identities: []Identity{
			{UserID: "@gmessages_1.630:x", Platform: "gmessages", DisplayName: "Priya"},
			{UserID: "@discord_4939:x", Platform: "discord", DisplayName: "Zora Star"},
		}},
	}
	got, err := ResolveContact(contacts, "alex morgan", "whatsapp")
	if err != nil || got.UserID != "@whatsapp_1:x" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := ResolveContact(contacts, "Alex Morgan", "telegram"); err == nil || !strings.Contains(err.Error(), "has a telegram identity") {
		t.Errorf("expected a no-identity error, got %v", err)
	}
	if _, err := ResolveContact(contacts, "Ana", "telegram"); err == nil || !strings.Contains(err.Error(), "2 telegram identities") {
		t.Errorf("expected an ambiguity error, got %v", err)
	}
	for _, name := range []string{"Priya", "zora star"} {
		if got, err := ResolveContact(contacts, name, "discord"); err != nil || got.UserID != "@discord_4939:x" {
			t.Errorf("%q on discord: got %+v, %v", name, got, err)
		}
	}
	if _, err := ResolveContact(contacts, "Nobody", "telegram"); err == nil {
		t.Error("expected a not-found error")
	}
}

func TestResolvePersonContactJoinsOnThePersonID(t *testing.T) {
	contacts := []UnifiedContact{
		{PersonID: "person_000040", DisplayName: "Priya", Identities: []Identity{
			{UserID: "@gmessages_1.630:x", Platform: "gmessages"},
			{UserID: "@discord_4939:x", Platform: "discord"},
			{UserID: "@discord_5000:x", Platform: "discord"},
		}},
		{DisplayName: "person_000040", Identities: []Identity{{UserID: "@telegram_2:x", Platform: "telegram"}}},
	}
	if got, err := ResolvePersonContact(contacts, "person_000040", "gmessages"); err != nil || got.UserID != "@gmessages_1.630:x" {
		t.Errorf("gmessages: %+v, %v", got, err)
	}
	if _, err := ResolvePersonContact(contacts, "person_000040", "discord"); err == nil || !strings.Contains(err.Error(), "2 discord accounts") {
		t.Errorf("ambiguous: %v", err)
	}
	if _, err := ResolvePersonContact(contacts, "person_000040", "telegram"); err == nil {
		t.Error("a contact whose name is the id must not match")
	}
	if _, err := ResolvePersonContact(contacts, "person_000099", "telegram"); err == nil {
		t.Error("expected a not-found error")
	}
}
