package contactlinks

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"unicode"

	"github.com/kayushkin/multichat/internal/matrix"
	"github.com/kayushkin/multichat/internal/people"
)

// BridgeDatabases are the bridge SQLite files that know phone numbers. An
// empty path leaves that bridge out.
type BridgeDatabases struct {
	GoogleMessages string
	WhatsApp       string
	Signal         string
	Telegram       string
}

// phoneNamingPreference orders the platforms whose display name a new person
// takes: the phone's own contact list first, since the operator wrote it.
var phoneNamingPreference = []string{"gmessages", "whatsapp", "signal", "telegram"}

func digitsOnly(phoneNumber string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, phoneNumber)
}

// IdentitiesByPhoneNumber reads every bridge's phone numbers and returns, per
// number (digits only), the Matrix ids of the puppets that carry it.
func IdentitiesByPhoneNumber(databases BridgeDatabases, serverName string) (map[string][]string, error) {
	byPhoneNumber := map[string][]string{}
	add := func(phoneNumber, userID string) {
		digits := digitsOnly(phoneNumber)
		if len(digits) < 7 || slices.Contains(byPhoneNumber[digits], userID) {
			return
		}
		byPhoneNumber[digits] = append(byPhoneNumber[digits], userID)
	}
	sources := []struct {
		path, name, query string
		userID            func(id string) string
	}{
		{databases.GoogleMessages, "GMESSAGES_DB",
			`SELECT id, json_extract(metadata, '$.phone') FROM ghost WHERE json_extract(metadata, '$.phone') IS NOT NULL`,
			func(id string) string { return "@gmessages_" + id + ":" + serverName }},
		{databases.WhatsApp, "WHATSAPP_DB",
			`SELECT lid, pn FROM whatsmeow_lid_map`,
			func(id string) string { return "@whatsapp_lid-" + id + ":" + serverName }},
		{databases.Signal, "SIGNAL_DB",
			`SELECT aci_uuid, e164_number FROM signalmeow_recipients WHERE aci_uuid IS NOT NULL AND e164_number != ''`,
			func(id string) string { return "@signal_" + id + ":" + serverName }},
		{databases.Telegram, "TELEGRAM_DB",
			`SELECT CAST(id AS TEXT), phone FROM puppet WHERE phone IS NOT NULL AND phone != ''`,
			func(id string) string { return "@telegram_" + id + ":" + serverName }},
	}
	for _, source := range sources {
		if source.path == "" {
			continue
		}
		database, err := sql.Open("sqlite3", "file:"+source.path+"?mode=ro")
		if err != nil {
			return nil, fmt.Errorf("open %s %s: %w", source.name, source.path, err)
		}
		rows, err := database.Query(source.query)
		if err != nil {
			database.Close()
			return nil, fmt.Errorf("read phone numbers from %s %s: %w", source.name, source.path, err)
		}
		for rows.Next() {
			var id, phoneNumber string
			if err := rows.Scan(&id, &phoneNumber); err != nil {
				rows.Close()
				database.Close()
				return nil, fmt.Errorf("read phone numbers from %s: %w", source.name, err)
			}
			add(phoneNumber, source.userID(id))
		}
		rows.Close()
		database.Close()
	}
	return byPhoneNumber, nil
}

// whatsAppPhoneNumberPuppets adds the WhatsApp puppets whose id is the phone
// number itself (@whatsapp_<digits>), which no table needs to translate.
func whatsAppPhoneNumberPuppets(byPhoneNumber map[string][]string, contacts []matrix.Contact) {
	for _, contact := range contacts {
		localpart := strings.SplitN(contact.UserID, ":", 2)[0]
		digits, found := strings.CutPrefix(localpart, "@whatsapp_")
		if !found || digits == "" || digitsOnly(digits) != digits {
			continue
		}
		if !slices.Contains(byPhoneNumber[digits], contact.UserID) {
			byPhoneNumber[digits] = append(byPhoneNumber[digits], contact.UserID)
		}
	}
}

func hasLetter(name string) bool {
	return strings.IndexFunc(name, unicode.IsLetter) >= 0
}

// PhoneNumberMatchResult counts what one pass did: accounts attached to a
// person, accounts people-store refused for the person they would have joined
// (the operator took them off), and numbers left alone because their accounts
// already belong to two or more people.
type PhoneNumberMatchResult struct {
	Linked  int `json:"linked"`
	Refused int `json:"refused"`
	Skipped int `json:"skipped_conflicts"`
}

// LinkByPhoneNumber links every set of accounts that share a phone number.
// Only accounts Synapse knows (contacts) and that are not ours count. When one
// of them belongs to a person, the rest join that person; when none does, a
// new person is made with all of them, but only when one is someone we talk
// with, so a group of strangers from group chats makes no person. Accounts
// that already belong to two people are left for the operator and counted.
func (l Linker) LinkByPhoneNumber(databases BridgeDatabases, serverName string, contacts []matrix.Contact, selfUserIDs, talkedWith map[string]bool) (PhoneNumberMatchResult, error) {
	var result PhoneNumberMatchResult
	byPhoneNumber, err := IdentitiesByPhoneNumber(databases, serverName)
	if err != nil {
		return result, err
	}
	whatsAppPhoneNumberPuppets(byPhoneNumber, contacts)
	return l.linkByPhoneNumber(byPhoneNumber, contacts, selfUserIDs, talkedWith)
}

// linkByPhoneNumber is LinkByPhoneNumber after the numbers are read.
func (l Linker) linkByPhoneNumber(byPhoneNumber map[string][]string, contacts []matrix.Contact, selfUserIDs, talkedWith map[string]bool) (PhoneNumberMatchResult, error) {
	var result PhoneNumberMatchResult
	owners, err := l.People.MatrixUserIdentities()
	if err != nil {
		return result, err
	}
	contactsByUserID := map[string]matrix.Contact{}
	for _, contact := range contacts {
		contactsByUserID[contact.UserID] = contact
	}
	for phoneNumber, userIDs := range byPhoneNumber {
		var known []matrix.Contact
		for _, userID := range userIDs {
			if contact, ok := contactsByUserID[userID]; ok && !selfUserIDs[userID] {
				known = append(known, contact)
			}
		}
		if len(known) < 2 {
			continue
		}
		slices.SortStableFunc(known, func(a, b matrix.Contact) int {
			return namingRank(a) - namingRank(b)
		})
		var persons []string
		worthAPerson := false
		for _, contact := range known {
			if owner, ok := owners[contact.UserID]; ok {
				if !slices.Contains(persons, owner.PersonID) {
					persons = append(persons, owner.PersonID)
				}
			}
			worthAPerson = worthAPerson || talkedWith[contact.UserID]
		}
		reason := "same phone number +" + phoneNumber
		switch {
		case len(persons) > 1:
			result.Skipped++
			log.Printf("contact links: %s is on %d people %v; left for the operator", reason, len(persons), persons)
		case len(persons) == 1:
			for _, contact := range known {
				if _, owned := owners[contact.UserID]; owned {
					continue
				}
				identity, err := l.People.AddIdentity(persons[0], link(contact.UserID, SourcePhoneNumber, reason))
				switch {
				case errors.Is(err, people.ErrConflict):
					result.Refused++
				case err != nil:
					return result, err
				default:
					owners[contact.UserID] = identity
					result.Linked++
				}
			}
		case worthAPerson:
			newPersonDisplayName := known[0].DisplayName
			if !hasLetter(newPersonDisplayName) {
				newPersonDisplayName = "+" + phoneNumber
			}
			links := make([]people.Link, len(known))
			for i, contact := range known {
				links[i] = link(contact.UserID, SourcePhoneNumber, reason)
			}
			personID, err := l.People.CreatePerson(newPersonDisplayName, links)
			if errors.Is(err, people.ErrConflict) {
				// One of them was linked since the pass read the owners.
				result.Skipped++
				log.Printf("contact links: %s: %v", reason, err)
				continue
			}
			if err != nil {
				return result, err
			}
			for _, contact := range known {
				owners[contact.UserID] = people.Identity{PersonID: personID, Kind: people.IdentityKindMatrixUser, Value: contact.UserID}
			}
			result.Linked += len(known)
		}
	}
	return result, nil
}

// namingRank puts identities with a real name first, then by platform
// preference, so a new person is named after the best name available.
func namingRank(contact matrix.Contact) int {
	rank := slices.Index(phoneNamingPreference, contact.Platform)
	if rank < 0 {
		rank = len(phoneNamingPreference)
	}
	if !hasLetter(contact.DisplayName) {
		rank += 100
	}
	return rank
}
