package people

import (
	"reflect"
	"strings"
	"testing"
)

func TestOperatorAccountsAddsEveryAccountOfTheUsersPerson(t *testing.T) {
	identities := map[string]Identity{
		"@discord_1:x":     {PersonID: "person_000001"},
		"@gmessages_1.9:x": {PersonID: "person_000001"},
		"@gmessages_1.5:x": {PersonID: "person_000002"},
	}
	person, accounts, err := OperatorAccounts(identities, map[string]bool{"@discord_1:x": true, "@gmessages_1.2:x": true})
	if err != nil {
		t.Fatal(err)
	}
	if person != "person_000001" || !reflect.DeepEqual(accounts, []string{"@discord_1:x", "@gmessages_1.9:x"}) {
		t.Fatalf("%s %v", person, accounts)
	}
	if person, accounts, err := OperatorAccounts(identities, map[string]bool{"@admin:x": true}); err != nil || person != "" || accounts != nil {
		t.Fatalf("nobody: %q %v %v", person, accounts, err)
	}
	if _, _, err := OperatorAccounts(identities, map[string]bool{"@discord_1:x": true, "@gmessages_1.5:x": true}); err == nil || !strings.Contains(err.Error(), "2 persons") {
		t.Fatalf("two persons: %v", err)
	}
}
