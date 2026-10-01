package contactlinks

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"unicode"

	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/matrix"
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

// PhoneNumberMatchResult counts what one pass did.
type PhoneNumberMatchResult struct {
	Linked  int `json:"linked"`
	Refused int `json:"refused"`
	Skipped int `json:"skipped_conflicts"`
}

// LinkByPhoneNumber links every pair of identities that share a phone number.
// Only identities Synapse knows (contacts) and that are not ours count. A new
// person is created only when one of them is someone we talk with; a group of
// strangers from group chats joins a person only if one of them already has
// one. A pair the operator marked as different people, or two people who
// already have separate ids, is left alone and counted.
func (l Linker) LinkByPhoneNumber(databases BridgeDatabases, serverName string, contacts []matrix.Contact, selfUserIDs, talkedWith map[string]bool) (PhoneNumberMatchResult, error) {
	var result PhoneNumberMatchResult
	byPhoneNumber, err := IdentitiesByPhoneNumber(databases, serverName)
	if err != nil {
		return result, err
	}
	whatsAppPhoneNumberPuppets(byPhoneNumber, contacts)
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
		worthAPerson := false
		for _, contact := range known {
			if talkedWith[contact.UserID] {
				worthAPerson = true
				break
			}
			link, err := db.ContactIdentityLinkOf(l.Database, contact.UserID)
			if err != nil {
				return result, err
			}
			if link != nil {
				worthAPerson = true
				break
			}
		}
		if !worthAPerson {
			continue
		}
		slices.SortStableFunc(known, func(a, b matrix.Contact) int {
			return namingRank(a) - namingRank(b)
		})
		anchor := known[0]
		newPersonDisplayName := anchor.DisplayName
		if !hasLetter(newPersonDisplayName) {
			newPersonDisplayName = "+" + phoneNumber
		}
		reason := "same phone number +" + phoneNumber
		for _, other := range known[1:] {
			anchorBefore, _, err := l.groupOf(anchor.UserID)
			if err != nil {
				return result, err
			}
			otherBefore, _, err := l.groupOf(other.UserID)
			if err != nil {
				return result, err
			}
			after, err := l.LinkSamePerson(anchor.UserID, other.UserID, newPersonDisplayName, db.ContactLinkSourcePhoneNumber, reason)
			switch {
			case errors.Is(err, db.ErrContactLinkRefused):
				result.Refused++
			case errors.Is(err, db.ErrContactLinkConflict):
				result.Skipped++
				log.Printf("contact links: %s and %s share %s but are already two people; left for the operator", anchor.UserID, other.UserID, reason)
			case err != nil:
				return result, err
			case anchorBefore != after || otherBefore != after:
				result.Linked++
			}
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
