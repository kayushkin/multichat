package people

import (
	"fmt"
	"sort"
)

// OperatorAccounts finds the person people-store gives any of the user's own
// accounts to (selfUserIDs, which multichat reads from the bridges) and
// returns every bridge account of that person. A bridge can miss one of the
// user's own accounts: mautrix-gmessages lists only the participant ids
// Google marks as the user's, and Google gives the user's number a second id
// in some groups. Once people-store gives that account to the user's person,
// it is the user's here too. personID is "" when people-store gives none of
// selfUserIDs to anyone; two different persons are an error, since then
// people-store does not know who the user is.
func OperatorAccounts(identities map[string]Identity, selfUserIDs map[string]bool) (personID string, accounts []string, err error) {
	owners := map[string]bool{}
	for userID := range selfUserIDs {
		if identity, linked := identities[userID]; linked {
			owners[identity.PersonID] = true
		}
	}
	if len(owners) > 1 {
		names := make([]string, 0, len(owners))
		for owner := range owners {
			names = append(names, owner)
		}
		sort.Strings(names)
		return "", nil, fmt.Errorf("people-store gives the user's own accounts to %d persons (%v); it should be one", len(owners), names)
	}
	for owner := range owners {
		personID = owner
	}
	if personID == "" {
		return "", nil, nil
	}
	for userID, identity := range identities {
		if identity.PersonID == personID {
			accounts = append(accounts, userID)
		}
	}
	sort.Strings(accounts)
	return personID, accounts, nil
}
