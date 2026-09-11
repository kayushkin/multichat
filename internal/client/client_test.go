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
	}
	got, err := ResolveContact(contacts, "alex morgan", "whatsapp")
	if err != nil || got.UserID != "@whatsapp_1:x" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := ResolveContact(contacts, "Alex Morgan", "telegram"); err == nil || !strings.Contains(err.Error(), "no telegram identity") {
		t.Errorf("expected a no-identity error, got %v", err)
	}
	if _, err := ResolveContact(contacts, "Ana", "telegram"); err == nil || !strings.Contains(err.Error(), "2 contacts named") {
		t.Errorf("expected an ambiguity error, got %v", err)
	}
	if _, err := ResolveContact(contacts, "Nobody", "telegram"); err == nil {
		t.Error("expected a not-found error")
	}
}
