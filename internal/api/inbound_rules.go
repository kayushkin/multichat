package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/kayushkin/multichat/internal/db"
	"github.com/kayushkin/multichat/internal/inbound"
	"github.com/kayushkin/multichat/internal/matrix"
)

// Inbound rules: what happens when a person messages us.
//
//	GET    /inbound-rules            list
//	POST   /inbound-rules            create (every field validated; instance_id checked with llm-bridge-server)
//	GET    /inbound-rules/{id}
//	PATCH  /inbound-rules/{id}       partial update, same validation on the merged row
//	DELETE /inbound-rules/{id}
//	GET    /inbound-dispatches?limit= what the rules did, newest first
//	GET    /inbound-platforms        the platform vocabulary a rule may name
//	GET    /inbound-prompt-fields    the fields a prompt_template may reference

func (r *Router) inboundRulesHandler(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case "GET":
		rules, err := db.ListInboundRules(r.db, false)
		if err != nil {
			r.writeError(w, 500, err.Error())
			return
		}
		r.writeJSON(w, rules)
	case "POST":
		// Defaults a caller may leave out. direct_messages_only defaults to
		// true: a rule that fires on group chatter is the surprising one, so
		// it must be asked for.
		rule := db.InboundRule{DirectMessagesOnly: true}
		if err := decodeStrict(req, &rule); err != nil {
			r.writeError(w, 400, err.Error())
			return
		}
		rule.Enabled = true
		if code, err := r.validateInboundRule(rule); err != nil {
			r.writeError(w, code, err.Error())
			return
		}
		created, err := db.CreateInboundRule(r.db, rule)
		if err != nil {
			r.writeError(w, 500, err.Error())
			return
		}
		w.WriteHeader(201)
		r.writeJSON(w, created)
	default:
		r.writeError(w, 405, "Method not allowed")
	}
}

func (r *Router) inboundRuleHandler(w http.ResponseWriter, req *http.Request) {
	id, err := strconv.ParseInt(strings.TrimPrefix(req.URL.Path, "/inbound-rules/"), 10, 64)
	if err != nil {
		r.writeError(w, 400, "rule id must be an integer")
		return
	}
	switch req.Method {
	case "GET":
		rule, err := db.GetInboundRule(r.db, id)
		if r.writeLookupError(w, err) {
			return
		}
		r.writeJSON(w, rule)
	case "PATCH":
		stored, err := db.GetInboundRule(r.db, id)
		if r.writeLookupError(w, err) {
			return
		}
		// Merge onto the stored row: decode into a copy so absent fields keep
		// their stored values and present ones overwrite.
		merged := stored
		if err := decodeStrict(req, &merged); err != nil {
			r.writeError(w, 400, err.Error())
			return
		}
		merged.ID = stored.ID
		if code, err := r.validateInboundRule(merged); err != nil {
			r.writeError(w, code, err.Error())
			return
		}
		updated, err := db.UpdateInboundRule(r.db, merged)
		if r.writeLookupError(w, err) {
			return
		}
		r.writeJSON(w, updated)
	case "DELETE":
		if r.writeLookupError(w, db.DeleteInboundRule(r.db, id)) {
			return
		}
		w.WriteHeader(204)
	default:
		r.writeError(w, 405, "Method not allowed")
	}
}

func (r *Router) inboundDispatchesHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != "GET" {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	limit := 100
	if l := req.URL.Query().Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n <= 0 {
			r.writeError(w, 400, "limit must be a positive integer")
			return
		}
		limit = n
	}
	dispatches, err := db.ListInboundDispatches(r.db, limit)
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	r.writeJSON(w, dispatches)
}

func (r *Router) inboundPlatformsHandler(w http.ResponseWriter, req *http.Request) {
	r.writeJSON(w, matrix.KnownPlatforms)
}

func (r *Router) inboundPromptFieldsHandler(w http.ResponseWriter, req *http.Request) {
	r.writeJSON(w, inbound.PromptFieldList())
}

// validateInboundRule refuses a rule that could not run. The instance is
// checked with its owner, llm-bridge-server: 400 when it says the id is
// unknown, 502 when it cannot answer — never stored unverified.
func (r *Router) validateInboundRule(rule db.InboundRule) (int, error) {
	if strings.TrimSpace(rule.Name) == "" {
		return 400, errors.New("name is required")
	}
	if rule.Platform != "" && !slices.Contains(matrix.KnownPlatforms, rule.Platform) {
		return 400, fmt.Errorf("platform %q is not one of %s (or empty for any)", rule.Platform, strings.Join(matrix.KnownPlatforms, ", "))
	}
	if rule.SenderUserID != "" && !strings.HasPrefix(rule.SenderUserID, "@") {
		return 400, fmt.Errorf("sender_user_id %q is not a Matrix user id (expected @platform_id:server)", rule.SenderUserID)
	}
	if rule.BodyPattern != "" {
		if _, err := regexp.Compile(rule.BodyPattern); err != nil {
			return 400, fmt.Errorf("body_pattern: %v", err)
		}
	}
	if strings.TrimSpace(rule.PromptTemplate) == "" {
		return 400, errors.New("prompt_template is required")
	}
	if err := inbound.CheckPromptTemplate(rule.PromptTemplate); err != nil {
		return 400, fmt.Errorf("prompt_template: %v", err)
	}
	if rule.InstanceID == "" {
		return 400, errors.New("instance_id is required (an llm-bridge-server instance id)")
	}
	_, err := inbound.LookupInstance(r.httpClient, r.llmBridgeURL, rule.InstanceID)
	var lookupErr *inbound.InstanceLookupError
	if errors.As(err, &lookupErr) {
		if lookupErr.Unknown {
			return 400, err
		}
		return 502, err
	}
	if err != nil {
		return 502, err
	}
	return 0, nil
}

func (r *Router) writeLookupError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, db.ErrNotFound) {
		r.writeError(w, 404, "no such inbound rule")
	} else {
		r.writeError(w, 500, err.Error())
	}
	return true
}

func decodeStrict(req *http.Request, into any) error {
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return fmt.Errorf("invalid JSON: %v", err)
	}
	return nil
}
