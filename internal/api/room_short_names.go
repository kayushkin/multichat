package api

// The Messages pages' typeable chat names (messages/kat), through multichat.
// short-name-store owns the names (namespace messages, entity type
// multichat_room, the room id as entity id); multichat already chooses them
// (short_names.go), and these routes are how the web front end reads them and
// renames a room, so it calls multichat alone:
//
//	GET /conversations/short-names                  every current room name
//	GET /conversations/short-names/resolve?path=kat what a path names
//	PUT /conversations/{room_id}/short-name {"path": "kat"}
//
// The rows are short-name-store's, passed back unchanged.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/kayushkin/multichat/internal/shortnames"
)

// shortNameRenamedByTheOperator is the created_by of a rename made through
// PUT /conversations/{room_id}/short-name, the Messages page's rename control.
const shortNameRenamedByTheOperator = "operator"

// shortNameRenameReason is the reason that rename records.
const shortNameRenameReason = "renamed by the operator on the Messages page"

// shortNameListPageSize is the most rows short-name-store answers in one page.
const shortNameListPageSize = 1000

// roomShortNamesHandler serves GET /conversations/short-names: every current
// name in namespace messages that points at a room, read page by page and
// answered as one JSON array of short-name-store's rows.
func (r *Router) roomShortNamesHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	if r.shortNameStoreURL == "" {
		r.writeError(w, 503, "multichat does not reach short-name-store: SHORT_NAME_STORE_URL is not set")
		return
	}
	all := []json.RawMessage{}
	for {
		query := url.Values{
			"namespace":   {shortnames.NamespaceMessages},
			"entity_type": {shortnames.EntityTypeMultichatRoom},
			"limit":       {strconv.Itoa(shortNameListPageSize)},
			"offset":      {strconv.Itoa(len(all))},
		}
		response, err := r.httpClient.Get(r.shortNameStoreURL + "/short-names?" + query.Encode())
		if err != nil {
			r.writeError(w, 502, "short-name-store: "+err.Error())
			return
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, 32<<20))
		response.Body.Close()
		if err != nil {
			r.writeError(w, 502, "short-name-store: reading the list: "+err.Error())
			return
		}
		if response.StatusCode != http.StatusOK {
			r.writeError(w, 502, fmt.Sprintf("short-name-store answered %d listing names: %s", response.StatusCode, strings.TrimSpace(string(data))))
			return
		}
		var page []json.RawMessage
		if err := json.Unmarshal(data, &page); err != nil {
			r.writeError(w, 502, "short-name-store's list does not parse: "+err.Error())
			return
		}
		all = append(all, page...)
		if len(page) < shortNameListPageSize {
			break
		}
	}
	r.writeJSON(w, all)
}

// resolveRoomShortNameHandler serves GET /conversations/short-names/resolve?path=:
// short-name-store's answer for that path in namespace messages, status and
// body unchanged, so a 404 still means no room has that name.
func (r *Router) resolveRoomShortNameHandler(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodGet {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	if r.shortNameStoreURL == "" {
		r.writeError(w, 503, "multichat does not reach short-name-store: SHORT_NAME_STORE_URL is not set")
		return
	}
	path := req.URL.Query().Get("path")
	if path == "" {
		r.writeError(w, 400, "path is required, such as kat or 20s30s/general")
		return
	}
	query := url.Values{"namespace": {shortnames.NamespaceMessages}, "path": {path}}
	response, err := r.httpClient.Get(r.shortNameStoreURL + "/short-names/resolve?" + query.Encode())
	if err != nil {
		r.writeError(w, 502, "short-name-store: "+err.Error())
		return
	}
	r.passAnswerThrough(w, response, "short-name-store resolve")
}

// roomShortNameHandler serves PUT /conversations/{room_id}/short-name
// {"path"}: names the room. A typed `messages/` before the path is dropped,
// since the page shows names that way. short-name-store judges the path and
// its answer comes back unchanged: 201 for a new name, 200 when the room held
// it already, 409 when another record holds it.
func (r *Router) roomShortNameHandler(w http.ResponseWriter, req *http.Request, roomID string) {
	if req.Method != http.MethodPut {
		r.writeError(w, 405, "Method not allowed")
		return
	}
	if r.shortNameStoreURL == "" {
		r.writeError(w, 503, "multichat does not reach short-name-store: SHORT_NAME_STORE_URL is not set")
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	decoder := json.NewDecoder(io.LimitReader(req.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		r.writeError(w, 400, "body must be {\"path\": \"…\"}: "+err.Error())
		return
	}
	path := strings.TrimLeft(strings.TrimSpace(body.Path), "/")
	path = strings.TrimPrefix(path, shortnames.NamespaceMessages+"/")
	if path == "" {
		r.writeError(w, 400, "Type the new name, such as kat or 20s30s/general.")
		return
	}
	encoded, err := json.Marshal(map[string]string{
		"namespace": shortnames.NamespaceMessages, "path": path,
		"entity_type": shortnames.EntityTypeMultichatRoom, "entity_id": roomID,
		"created_by": shortNameRenamedByTheOperator, "reason": shortNameRenameReason,
	})
	if err != nil {
		r.writeError(w, 500, err.Error())
		return
	}
	response, err := r.httpClient.Post(r.shortNameStoreURL+"/short-names", "application/json", bytes.NewReader(encoded))
	if err != nil {
		r.writeError(w, 502, "short-name-store: "+err.Error())
		return
	}
	r.passAnswerThrough(w, response, "short-name-store rename")
}

// passAnswerThrough writes a store's answer back unchanged: status, content
// type and body. It closes the response.
func (r *Router) passAnswerThrough(w http.ResponseWriter, response *http.Response, what string) {
	defer response.Body.Close()
	if contentType := response.Header.Get("Content-Type"); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	w.WriteHeader(response.StatusCode)
	if _, err := io.Copy(w, response.Body); err != nil {
		fmt.Printf("%s: copying the answer: %v\n", what, err)
	}
}
