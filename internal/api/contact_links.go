package api

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/kayushkin/multichat/internal/contactlinks"
	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/discordbridge"
	"github.com/kayushkin/multichat/internal/matrix"
	"github.com/kayushkin/multichat/internal/principals"
)

func (r *Router) setupContactLinkRoutes() {
	r.mux.HandleFunc("GET /contacts/unified", r.unifiedContactsHandler)
	r.mux.HandleFunc("GET /contacts/links/sources", r.contactLinkSourcesHandler)
	r.mux.HandleFunc("POST /contacts/links", r.linkContactHandler)
	r.mux.HandleFunc("POST /contacts/links/remove", r.unlinkContactHandler)
	r.mux.HandleFunc("POST /contacts/links/phone-match", r.phoneMatchHandler)
	r.mux.HandleFunc("GET /contacts/links/suggestions", r.listContactLinkSuggestionsHandler)
	r.mux.HandleFunc("POST /contacts/links/suggestions", r.addContactLinkSuggestionHandler)
	r.mux.HandleFunc("POST /contacts/links/suggestions/{id}/{decision}", r.decideContactLinkSuggestionHandler)
	r.mux.HandleFunc("GET /contacts/reviews/pending", r.pendingContactReviewsHandler)
	r.mux.HandleFunc("POST /contacts/reviews/claim", r.claimContactReviewsHandler)
	r.mux.HandleFunc("POST /contacts/reviews", r.markContactsReviewedHandler)
}

func (r *Router) contactLinker() contactlinks.Linker {
	return contactlinks.Linker{Database: r.db, Principals: r.principals}
}

// requireContactLinking answers 503 naming what is missing and returns false
// when linking cannot work.
func (r *Router) requireContactLinking(w http.ResponseWriter) bool {
	if !r.principals.Configured() {
		r.writeError(w, 503, "PRINCIPAL_STORE_URL is not set; people and their links live there")
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
	case errors.Is(err, db.ErrUnknownContactLinkSource), errors.Is(err, principals.ErrUnknownPrincipal):
		r.writeError(w, 400, err.Error())
	case errors.Is(err, db.ErrContactLinkRefused), errors.Is(err, db.ErrContactLinkConflict):
		r.writeError(w, 409, err.Error())
	case errors.Is(err, db.ErrUnknownSuggestion):
		r.writeError(w, 404, err.Error())
	case errors.Is(err, principals.ErrPrincipalStoreFailed):
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

func (r *Router) contactLinkSourcesHandler(w http.ResponseWriter, req *http.Request) {
	r.writeJSON(w, db.ContactLinkSources)
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

// UnifiedContact is one person: every identity linked to one principal-store
// id, or a single identity that is linked to no one yet.
type UnifiedContact struct {
	PrincipalID string            `json:"principal_id,omitempty"`
	DisplayName string            `json:"display_name"`
	Identities  []UnifiedIdentity `json:"identities"`
	Platforms   []string          `json:"platforms"`
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
	links, err := db.ListContactIdentityLinks(r.db)
	if err != nil {
		r.writeError(w, 500, err.Error())
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
			key = "principal:" + link.PrincipalID
		}
		group, ok := groups[key]
		if !ok {
			group = &UnifiedContact{PrincipalID: link.PrincipalID, DisplayName: contact.DisplayName}
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
		if group.PrincipalID != "" {
			principal, err := r.principals.Get(group.PrincipalID)
			if err != nil {
				r.writeError(w, 502, fmt.Sprintf("read person %s: %v", group.PrincipalID, err))
				return
			}
			group.DisplayName = principal.DisplayName
		}
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
		PrincipalID string `json:"principal_id"`
		LinkedBy    string `json:"linked_by"`
		Reason      string `json:"reason"`
		DisplayName string `json:"display_name"`
	}
	if err := decodeStrictJSON(req, &body); err != nil {
		r.writeError(w, 400, "body must be JSON {user_id, with_user_id | principal_id, linked_by, reason, display_name?}: "+err.Error())
		return
	}
	if err := checkContactUserID("user_id", body.UserID); err != nil {
		r.writeError(w, 400, err.Error())
		return
	}
	if (body.WithUserID == "") == (body.PrincipalID == "") {
		r.writeError(w, 400, "give exactly one of with_user_id (the same person's other identity) or principal_id (an existing person)")
		return
	}
	if strings.TrimSpace(body.Reason) == "" {
		r.writeError(w, 400, "reason is required: say why these are the same person")
		return
	}
	linker := r.contactLinker()
	if body.PrincipalID != "" {
		if err := linker.LinkToPrincipal(body.UserID, body.PrincipalID, body.LinkedBy, body.Reason); err != nil {
			r.writeContactLinkError(w, err)
			return
		}
		r.writeJSON(w, map[string]string{"principal_id": body.PrincipalID})
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
	principalID, err := linker.LinkSamePerson(body.UserID, body.WithUserID, newPersonDisplayName, body.LinkedBy, body.Reason)
	if err != nil {
		r.writeContactLinkError(w, err)
		return
	}
	r.writeJSON(w, map[string]string{"principal_id": principalID})
}

func (r *Router) unlinkContactHandler(w http.ResponseWriter, req *http.Request) {
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
	removed, err := db.RemoveContactIdentityLink(r.db, body.UserID, body.RemovedBy)
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

// peopleWeTalkWith returns every identity that has sent a message into the
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

func (r *Router) listContactLinkSuggestionsHandler(w http.ResponseWriter, req *http.Request) {
	suggestions, err := db.ListOpenContactLinkSuggestions(r.db)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	r.writeJSON(w, suggestions)
}

func (r *Router) addContactLinkSuggestionHandler(w http.ResponseWriter, req *http.Request) {
	var body struct {
		UserID      string `json:"user_id"`
		WithUserID  string `json:"with_user_id"`
		Reason      string `json:"reason"`
		SuggestedBy string `json:"suggested_by"`
	}
	if err := decodeStrictJSON(req, &body); err != nil {
		r.writeError(w, 400, "body must be JSON {user_id, with_user_id, reason, suggested_by}: "+err.Error())
		return
	}
	for field, value := range map[string]string{"user_id": body.UserID, "with_user_id": body.WithUserID} {
		if err := checkContactUserID(field, value); err != nil {
			r.writeError(w, 400, err.Error())
			return
		}
	}
	if body.UserID == body.WithUserID {
		r.writeError(w, 400, "user_id and with_user_id are the same identity")
		return
	}
	if strings.TrimSpace(body.Reason) == "" || !slices.Contains(db.ContactLinkSources, body.SuggestedBy) {
		r.writeError(w, 400, fmt.Sprintf("reason is required and suggested_by must be one of %v", db.ContactLinkSources))
		return
	}
	suggestion, err := db.AddContactLinkSuggestion(r.db, body.UserID, body.WithUserID, body.Reason, body.SuggestedBy)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	r.writeJSON(w, suggestion)
}

func (r *Router) decideContactLinkSuggestionHandler(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.ParseInt(req.PathValue("id"), 10, 64)
	if err != nil {
		r.writeError(w, 404, "no suggestion "+req.PathValue("id"))
		return
	}
	decision := req.PathValue("decision")
	if decision != "accept" && decision != "reject" {
		r.writeError(w, 404, "a suggestion is decided with /accept or /reject")
		return
	}
	var body struct {
		DecidedBy string `json:"decided_by"`
	}
	if err := decodeStrictJSON(req, &body); err != nil {
		r.writeError(w, 400, "body must be JSON {decided_by}: "+err.Error())
		return
	}
	if !slices.Contains(db.ContactLinkSources, body.DecidedBy) {
		r.writeError(w, 400, db.ErrUnknownContactLinkSource.Error())
		return
	}
	suggestion, err := db.OpenContactLinkSuggestion(r.db, id)
	if err != nil {
		r.writeContactLinkError(w, err)
		return
	}
	if decision == "reject" {
		if err := db.DecideContactLinkSuggestion(r.db, id, db.ContactLinkSuggestionRejected, body.DecidedBy); err != nil {
			r.writeContactLinkError(w, err)
			return
		}
		r.writeJSON(w, map[string]string{"status": db.ContactLinkSuggestionRejected})
		return
	}
	if !r.requireContactLinking(w) {
		return
	}
	newPersonDisplayName, err := r.contactDisplayName(suggestion.ContactUserID)
	if err != nil {
		r.writeError(w, 400, err.Error())
		return
	}
	principalID, err := r.contactLinker().LinkSamePerson(suggestion.ContactUserID, suggestion.OtherContactUserID, newPersonDisplayName, body.DecidedBy, suggestion.Reason)
	if err != nil {
		r.writeContactLinkError(w, err)
		return
	}
	if err := db.DecideContactLinkSuggestion(r.db, id, db.ContactLinkSuggestionAccepted, body.DecidedBy); err != nil {
		r.writeContactLinkError(w, err)
		return
	}
	r.writeJSON(w, map[string]string{"status": db.ContactLinkSuggestionAccepted, "principal_id": principalID})
}

// PendingContactReview is an identity the agent pass has not looked at.
type PendingContactReview struct {
	UserID      string `json:"user_id"`
	Platform    string `json:"platform"`
	DisplayName string `json:"display_name"`
	PrincipalID string `json:"principal_id,omitempty"`
}

// pendingContactReviews returns the people we talk with whom the agent pass
// has neither reviewed nor claimed in the last half hour, sorted by id, and
// how many identities are claimed right now.
func (r *Router) pendingContactReviews() ([]PendingContactReview, int, error) {
	talkedWith, err := r.peopleWeTalkWith()
	if err != nil {
		return nil, 0, err
	}
	reviewed, err := db.ReviewedContactUserIDs(r.db)
	if err != nil {
		return nil, 0, err
	}
	claimed, err := db.ActiveContactReviewClaims(r.db)
	if err != nil {
		return nil, 0, err
	}
	links, err := db.ListContactIdentityLinks(r.db)
	if err != nil {
		return nil, 0, err
	}
	contacts, err := r.matrix.ListContacts()
	if err != nil {
		return nil, 0, err
	}
	pending := []PendingContactReview{}
	for _, contact := range contacts {
		if !talkedWith[contact.UserID] || reviewed[contact.UserID] || claimed[contact.UserID] || matrix.SelfUserIDs[contact.UserID] {
			continue
		}
		pending = append(pending, PendingContactReview{
			UserID: contact.UserID, Platform: contact.Platform, DisplayName: contact.DisplayName,
			PrincipalID: links[contact.UserID].PrincipalID,
		})
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].UserID < pending[j].UserID })
	return pending, len(claimed), nil
}

func parseReviewLimit(raw string) (int, error) {
	if raw == "" {
		return 25, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > 1000 {
		return 0, errors.New("limit must be a whole number from 1 to 1000")
	}
	return limit, nil
}

// pendingContactReviewsHandler answers {total, active_claims, pending}, so a
// scheduler check starts the agent only when there is work and no session is
// already on it.
func (r *Router) pendingContactReviewsHandler(w http.ResponseWriter, req *http.Request) {
	if r.matrix == nil {
		r.writeError(w, 503, "the Matrix client is not configured (SYNAPSE_URL and MATRIX_TOKEN)")
		return
	}
	limit, err := parseReviewLimit(req.URL.Query().Get("limit"))
	if err != nil {
		r.writeError(w, 400, err.Error())
		return
	}
	pending, activeClaims, err := r.pendingContactReviews()
	if err != nil {
		r.writeError(w, 502, err.Error())
		return
	}
	total := len(pending)
	pending = pending[:min(limit, total)]
	r.writeJSON(w, map[string]any{"total": total, "active_claims": activeClaims, "pending": pending})
}

// claimContactReviewsHandler hands a reviewer the next batch and claims it
// for half an hour, so two sessions never review the same people.
func (r *Router) claimContactReviewsHandler(w http.ResponseWriter, req *http.Request) {
	if r.matrix == nil {
		r.writeError(w, 503, "the Matrix client is not configured (SYNAPSE_URL and MATRIX_TOKEN)")
		return
	}
	var body struct {
		Limit     int    `json:"limit"`
		ClaimedBy string `json:"claimed_by"`
	}
	if err := decodeStrictJSON(req, &body); err != nil {
		r.writeError(w, 400, "body must be JSON {limit, claimed_by}: "+err.Error())
		return
	}
	if body.Limit < 1 || body.Limit > 100 || !slices.Contains(db.ContactLinkSources, body.ClaimedBy) {
		r.writeError(w, 400, fmt.Sprintf("limit must be 1 to 100 and claimed_by one of %v", db.ContactLinkSources))
		return
	}
	pending, _, err := r.pendingContactReviews()
	if err != nil {
		r.writeError(w, 502, err.Error())
		return
	}
	batch := pending[:min(body.Limit, len(pending))]
	ids := make([]string, len(batch))
	for i, item := range batch {
		ids[i] = item.UserID
	}
	if err := db.ClaimContactsForReview(r.db, ids, body.ClaimedBy); err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	r.writeJSON(w, map[string]any{"claimed": batch, "remaining": len(pending) - len(batch)})
}

func (r *Router) markContactsReviewedHandler(w http.ResponseWriter, req *http.Request) {
	var body struct {
		UserIDs    []string `json:"user_ids"`
		ReviewedBy string   `json:"reviewed_by"`
	}
	if err := decodeStrictJSON(req, &body); err != nil {
		r.writeError(w, 400, "body must be JSON {user_ids, reviewed_by}: "+err.Error())
		return
	}
	if len(body.UserIDs) == 0 || !slices.Contains(db.ContactLinkSources, body.ReviewedBy) {
		r.writeError(w, 400, fmt.Sprintf("user_ids must not be empty and reviewed_by must be one of %v", db.ContactLinkSources))
		return
	}
	for _, userID := range body.UserIDs {
		if err := checkContactUserID("user_ids", userID); err != nil {
			r.writeError(w, 400, err.Error())
			return
		}
	}
	if err := db.MarkContactsReviewed(r.db, body.UserIDs, body.ReviewedBy); err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	r.writeJSON(w, map[string]int{"reviewed": len(body.UserIDs)})
}
