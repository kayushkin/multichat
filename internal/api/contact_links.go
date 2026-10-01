package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/kayushkin/multichat/internal/contactlinks"
	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/discordbridge"
	"github.com/kayushkin/multichat/internal/matrix"
	"github.com/kayushkin/multichat/internal/people"
)

func (r *Router) setupContactLinkRoutes() {
	r.mux.HandleFunc("GET /contacts/unified", r.unifiedContactsHandler)
	r.mux.HandleFunc("GET /contacts/links/sources", r.contactLinkSourcesHandler)
	r.mux.HandleFunc("POST /contacts/links", r.linkContactHandler)
	r.mux.HandleFunc("POST /contacts/links/remove", r.unlinkContactHandler)
	r.mux.HandleFunc("POST /contacts/links/phone-match", r.phoneMatchHandler)
}

func (r *Router) contactLinker() contactlinks.Linker {
	return contactlinks.Linker{People: r.people}
}

// requireContactLinking answers 503 naming what is missing and returns false
// when linking cannot work.
func (r *Router) requireContactLinking(w http.ResponseWriter) bool {
	if !r.people.Configured() {
		r.writeError(w, 503, "PEOPLE_STORE_URL is not set; people and their linked accounts live there")
		return false
	}
	if r.matrix == nil {
		r.writeError(w, 503, "the Matrix client is not configured (SYNAPSE_URL and MATRIX_TOKEN)")
		return false
	}
	return true
}

// writeContactLinkError maps the linking errors onto status codes.
func (r *Router) writeContactLinkError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, people.ErrInvalid), errors.Is(err, contactlinks.ErrUnknownPerson):
		r.writeError(w, 400, err.Error())
	case errors.Is(err, people.ErrConflict), errors.Is(err, contactlinks.ErrAlreadyTwoPeople):
		r.writeError(w, 409, err.Error())
	case errors.Is(err, people.ErrNotFound), errors.Is(err, people.ErrPeopleStoreFailed):
		r.writeError(w, 502, err.Error())
	default:
		r.writeError(w, 500, err.Error())
	}
}

func decodeStrictJSON(req *http.Request, into any) error {
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	return decoder.Decode(into)
}

// checkContactUserID refuses anything that is not someone else's bridge puppet.
func checkContactUserID(field, userID string) error {
	if matrix.PlatformOfUserID(userID) == "" {
		return fmt.Errorf("%s must be a bridge puppet id such as @gmessages_1.630:chat.kayushkin.com, got %q", field, userID)
	}
	if matrix.SelfUserIDs[userID] {
		return fmt.Errorf("%s %s is our own account, not a contact", field, userID)
	}
	return nil
}

// contactLinkSourcesHandler serves people-store's link sources, which own
// the vocabulary of linked_by.
func (r *Router) contactLinkSourcesHandler(w http.ResponseWriter, req *http.Request) {
	sources, err := r.people.LinkSources()
	if err != nil {
		r.writeContactLinkError(w, err)
		return
	}
	r.writeJSON(w, sources)
}

// UnifiedIdentity is one platform account of a person.
type UnifiedIdentity struct {
	UserID      string `json:"user_id"`
	Platform    string `json:"platform"`
	DisplayName string `json:"display_name"`
	Avatar      string `json:"avatar_url,omitempty"`
	LinkedBy    string `json:"linked_by,omitempty"`
	LinkReason  string `json:"link_reason,omitempty"`
}

// UnifiedContact is one person: every account attached to one people-store
// person, or a single account attached to no one yet. PersonDeletedAt is set
// when the operator deleted the person in people-store; their accounts stay
// attached until someone takes them off.
type UnifiedContact struct {
	PersonID        string            `json:"person_id,omitempty"`
	PersonDeletedAt int64             `json:"person_deleted_at,omitempty"`
	DisplayName     string            `json:"display_name"`
	Identities      []UnifiedIdentity `json:"identities"`
	Platforms       []string          `json:"platforms"`
}

func (r *Router) unifiedContactsHandler(w http.ResponseWriter, req *http.Request) {
	if !r.requireContactLinking(w) {
		return
	}
	contacts, err := r.matrix.ListContacts()
	if err != nil {
		r.writeError(w, 502, err.Error())
		return
	}
	links, err := r.people.MatrixUserIdentities()
	if err != nil {
		r.writeContactLinkError(w, err)
		return
	}
	persons, err := r.people.Persons()
	if err != nil {
		r.writeContactLinkError(w, err)
		return
	}
	metaPlatforms := r.metaUserPlatforms()

	groups := map[string]*UnifiedContact{}
	var order []string
	for _, contact := range contacts {
		if matrix.SelfUserIDs[contact.UserID] {
			continue
		}
		link, linked := links[contact.UserID]
		if !linked && strings.TrimSpace(contact.DisplayName) == "" {
			continue
		}
		key := "identity:" + contact.UserID
		if linked {
			key = "person:" + link.PersonID
		}
		group, ok := groups[key]
		if !ok {
			group = &UnifiedContact{PersonID: link.PersonID, DisplayName: contact.DisplayName}
			if linked {
				person, known := persons[link.PersonID]
				if !known {
					r.writeError(w, 502, fmt.Sprintf("people-store lists %s on %s but not that person", contact.UserID, link.PersonID))
					return
				}
				group.DisplayName = person.DisplayName
				group.PersonDeletedAt = person.DeletedAt
			}
			groups[key] = group
			order = append(order, key)
		}
		platform := contact.Platform
		if resolved, ok := metaPlatforms[contact.UserID]; ok && platform == "meta" {
			platform = resolved
		}
		group.Identities = append(group.Identities, UnifiedIdentity{
			UserID: contact.UserID, Platform: platform, DisplayName: contact.DisplayName, Avatar: contact.AvatarURL,
			LinkedBy: link.LinkedBy, LinkReason: link.Reason,
		})
	}

	result := make([]UnifiedContact, 0, len(groups))
	for _, key := range order {
		group := groups[key]
		for _, identity := range group.Identities {
			if !slices.Contains(group.Platforms, identity.Platform) {
				group.Platforms = append(group.Platforms, identity.Platform)
			}
		}
		sort.Strings(group.Platforms)
		result = append(result, *group)
	}
	sort.Slice(result, func(i, j int) bool {
		if len(result[i].Platforms) != len(result[j].Platforms) {
			return len(result[i].Platforms) > len(result[j].Platforms)
		}
		return result[i].DisplayName < result[j].DisplayName
	})
	r.writeJSON(w, result)
}

// metaUserPlatforms maps each Meta puppet to "facebook" or "instagram" from
// the bridge database; puppets it cannot place keep "meta".
func (r *Router) metaUserPlatforms() map[string]string {
	result := map[string]string{}
	if r.metaDBPath == "" {
		return result
	}
	metaDB, err := sql.Open("sqlite3", r.metaDBPath+"?mode=ro")
	if err != nil {
		return result
	}
	defer metaDB.Close()
	loginPlatform := map[string]string{}
	if rows, _ := metaDB.Query(`SELECT id, json_extract(metadata, '$.platform') FROM user_login`); rows != nil {
		for rows.Next() {
			var id, platform string
			rows.Scan(&id, &platform)
			loginPlatform[id] = platform
		}
		rows.Close()
	}
	for _, query := range []string{
		`SELECT g.mxid, up.login_id FROM ghost g JOIN user_portal up ON 1=1 WHERE g.mxid IS NOT NULL`,
		`SELECT p.mxid, up.login_id FROM portal p JOIN user_portal up ON p.id = up.portal_id AND p.receiver = up.portal_receiver WHERE p.receiver != ''`,
	} {
		rows, _ := metaDB.Query(query)
		if rows == nil {
			continue
		}
		for rows.Next() {
			var mxid, loginID string
			rows.Scan(&mxid, &loginID)
			if platform, ok := loginPlatform[loginID]; ok {
				result[mxid] = platform
			}
		}
		rows.Close()
	}
	return result
}

// contactDisplayName is the name Synapse holds for one identity.
func (r *Router) contactDisplayName(userID string) (string, error) {
	contacts, err := r.matrix.ListContacts()
	if err != nil {
		return "", err
	}
	for _, contact := range contacts {
		if contact.UserID == userID {
			return contact.DisplayName, nil
		}
	}
	return "", fmt.Errorf("Synapse knows no user %s", userID)
}

func (r *Router) linkContactHandler(w http.ResponseWriter, req *http.Request) {
	if !r.requireContactLinking(w) {
		return
	}
	var body struct {
		UserID      string `json:"user_id"`
		WithUserID  string `json:"with_user_id"`
		PersonID    string `json:"person_id"`
		LinkedBy    string `json:"linked_by"`
		Reason      string `json:"reason"`
		DisplayName string `json:"display_name"`
	}
	if err := decodeStrictJSON(req, &body); err != nil {
		r.writeError(w, 400, "body must be JSON {user_id, with_user_id | person_id, linked_by, reason, display_name?}: "+err.Error())
		return
	}
	if err := checkContactUserID("user_id", body.UserID); err != nil {
		r.writeError(w, 400, err.Error())
		return
	}
	if (body.WithUserID == "") == (body.PersonID == "") {
		r.writeError(w, 400, "give exactly one of with_user_id (the same person's other account) or person_id (an existing people-store person)")
		return
	}
	if strings.TrimSpace(body.Reason) == "" {
		r.writeError(w, 400, "reason is required: say why these are the same person")
		return
	}
	linker := r.contactLinker()
	if body.PersonID != "" {
		if err := linker.LinkToPerson(body.UserID, body.PersonID, body.LinkedBy, body.Reason); err != nil {
			r.writeContactLinkError(w, err)
			return
		}
		r.writeJSON(w, map[string]string{"person_id": body.PersonID})
		return
	}
	if err := checkContactUserID("with_user_id", body.WithUserID); err != nil {
		r.writeError(w, 400, err.Error())
		return
	}
	newPersonDisplayName := body.DisplayName
	if strings.TrimSpace(newPersonDisplayName) == "" {
		name, err := r.contactDisplayName(body.UserID)
		if err != nil {
			r.writeError(w, 400, err.Error())
			return
		}
		newPersonDisplayName = name
	}
	personID, err := linker.LinkSamePerson(body.UserID, body.WithUserID, newPersonDisplayName, body.LinkedBy, body.Reason)
	if err != nil {
		r.writeContactLinkError(w, err)
		return
	}
	r.writeJSON(w, map[string]string{"person_id": personID})
}

// unlinkContactHandler takes an account off its person. Only the operator
// unlinks: people-store records the removal as the operator's refusal, which
// keeps every automatic source from linking it back.
func (r *Router) unlinkContactHandler(w http.ResponseWriter, req *http.Request) {
	if !r.requireContactLinking(w) {
		return
	}
	var body struct {
		UserID    string `json:"user_id"`
		RemovedBy string `json:"removed_by"`
	}
	if err := decodeStrictJSON(req, &body); err != nil {
		r.writeError(w, 400, "body must be JSON {user_id, removed_by}: "+err.Error())
		return
	}
	if err := checkContactUserID("user_id", body.UserID); err != nil {
		r.writeError(w, 400, err.Error())
		return
	}
	if body.RemovedBy != contactlinks.SourceOperator {
		r.writeError(w, 400, "removed_by must be operator: only the operator takes an account off a person")
		return
	}
	removed, err := r.contactLinker().Unlink(body.UserID)
	if err != nil {
		r.writeContactLinkError(w, err)
		return
	}
	r.writeJSON(w, map[string]any{"removed": removed})
}

func (r *Router) phoneMatchHandler(w http.ResponseWriter, req *http.Request) {
	if !r.requireContactLinking(w) {
		return
	}
	result, err := r.LinkByPhoneNumber()
	if err != nil {
		r.writeContactLinkError(w, err)
		return
	}
	r.writeJSON(w, result)
}

// peopleWeTalkWith returns every account that has sent a message into the
// log or is the other person in a one-to-one chat we have joined. Only these
// are worth a person of their own; the rest are strangers in group chats.
func (r *Router) peopleWeTalkWith() (map[string]bool, error) {
	talkedWith, err := db.ContactUserIDsWeHaveTalkedWith(r.db)
	if err != nil {
		return nil, err
	}
	var discordDirectMessageRoomIDs map[string]bool
	if r.discordDBPath != "" {
		bridge, err := discordbridge.Open(r.discordDBPath)
		if err != nil {
			return nil, err
		}
		discordDirectMessageRoomIDs, err = discordbridge.DirectMessageRoomIDs(bridge)
		bridge.Close()
		if err != nil {
			return nil, fmt.Errorf("read Discord DM rooms from DISCORD_DB: %w", err)
		}
	}
	conversations, err := r.matrix.ListConversations(r.loadMetaPlatforms(), discordDirectMessageRoomIDs)
	if err != nil {
		return nil, err
	}
	for _, conversation := range conversations {
		if conversation.DirectMessagePartnerUserID != "" {
			talkedWith[conversation.DirectMessagePartnerUserID] = true
		}
	}
	return talkedWith, nil
}

// LinkByPhoneNumber runs one phone-number matching pass over every contact
// Synapse knows.
func (r *Router) LinkByPhoneNumber() (contactlinks.PhoneNumberMatchResult, error) {
	ownUserID, err := r.matrix.WhoAmI()
	if err != nil {
		return contactlinks.PhoneNumberMatchResult{}, err
	}
	_, serverName, _ := strings.Cut(ownUserID, ":")
	contacts, err := r.matrix.ListContacts()
	if err != nil {
		return contactlinks.PhoneNumberMatchResult{}, err
	}
	talkedWith, err := r.peopleWeTalkWith()
	if err != nil {
		return contactlinks.PhoneNumberMatchResult{}, err
	}
	return r.contactLinker().LinkByPhoneNumber(r.bridgeDatabases, serverName, contacts, matrix.SelfUserIDs, talkedWith)
}
