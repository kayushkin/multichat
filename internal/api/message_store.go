package api

// message-store's records, through multichat, for the Messages pages: what our
// bot accounts sent and received, and a person's older chats that no live
// Matrix room carries. multichat is the one messaging API the web front end
// calls; message-store stays the owner of these records, and the reads pass
// through unchanged. The one write sends as a bot account into a conversation
// message-store holds:
//
//	GET  /message-store/accounts, /accounts/{id}
//	GET  /message-store/conversations, /conversations/{id}
//	GET  /message-store/messages
//	POST /message-store/accounts/{account_id}/conversations/{conversation_id}/messages {"body": "…"}
//
// The send posts through the bot's own connection (internal/botaccounts),
// files the sent message with message-store at once so the page shows it, and
// answers message-store's row. The bot importer reads the same message again
// later under the same key and finds it unchanged.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/kayushkin/multichat/internal/botaccounts"
)

// messageStoreMount is where these routes hang, under the /api prefix main strips.
const messageStoreMount = "/message-store/"

// messageStoreReadPatterns are the message-store reads the Messages pages
// make; a "*" is exactly one path segment. Anything else under the mount is a 404.
var messageStoreReadPatterns = []string{"accounts", "accounts/*", "conversations", "conversations/*", "messages"}

// messageStoreSendPattern is the one write: a send as a bot account.
const messageStoreSendPattern = "accounts/*/conversations/*/messages"

// errMessageStoreNotFound is message-store's 404: no such account or conversation.
var errMessageStoreNotFound = errors.New("not found")

// errAccountSendRefused is a send this route will not make.
var errAccountSendRefused = errors.New("refused")

func (r *Router) messageStoreHandler(w http.ResponseWriter, req *http.Request) {
	if r.messageStoreURL == "" {
		r.writeError(w, 503, "multichat does not reach message-store: MESSAGE_STORE_URL is not set")
		return
	}
	rest := strings.Trim(strings.TrimPrefix(req.URL.Path, messageStoreMount), "/")
	segments := strings.Split(rest, "/")
	switch {
	case req.Method == http.MethodPost && pathSegmentsMatch(messageStoreSendPattern, segments):
		r.sendAsAccountHandler(w, req, segments[1], segments[3])
	case req.Method == http.MethodGet && pathMatchesAny(messageStoreReadPatterns, segments):
		r.forwardMessageStoreRead(w, req, rest)
	default:
		r.writeError(w, 404, fmt.Sprintf("%s %s%s is not a message-store route multichat carries", req.Method, messageStoreMount, rest))
	}
}

// pathMatchesAny reports whether the segments match one of the patterns.
func pathMatchesAny(patterns []string, segments []string) bool {
	for _, pattern := range patterns {
		if pathSegmentsMatch(pattern, segments) {
			return true
		}
	}
	return false
}

// pathSegmentsMatch reports whether the segments match the pattern, where a
// "*" matches any one non-empty segment.
func pathSegmentsMatch(pattern string, segments []string) bool {
	parts := strings.Split(pattern, "/")
	if len(parts) != len(segments) {
		return false
	}
	for index, part := range parts {
		if segments[index] == "" || (part != "*" && part != segments[index]) {
			return false
		}
	}
	return true
}

// forwardMessageStoreRead passes one read to message-store and its answer back
// unchanged: status, content type and body. message-store answers a merged or
// superseded id with a 301 to the row that holds it now, which the client
// follows, so the caller gets that row.
func (r *Router) forwardMessageStoreRead(w http.ResponseWriter, req *http.Request, rest string) {
	address := r.messageStoreURL + "/" + rest
	if req.URL.RawQuery != "" {
		address += "?" + req.URL.RawQuery
	}
	response, err := r.httpClient.Get(address)
	if err != nil {
		r.writeError(w, 502, "message-store: "+err.Error())
		return
	}
	r.passAnswerThrough(w, response, "message-store GET /"+rest)
}

// messageStoreAccount is the part of message-store's account a send reads.
type messageStoreAccount struct {
	ID          string `json:"id"`
	Platform    string `json:"platform"`
	Handle      string `json:"handle"`
	DisplayName string `json:"display_name"`
}

// messageStoreConversation is the part of message-store's conversation a send reads.
type messageStoreConversation struct {
	ID           string `json:"id"`
	Platform     string `json:"platform"`
	Source       string `json:"source"`
	SourceRef    string `json:"source_ref"`
	IsGroup      bool   `json:"is_group"`
	Participants []struct {
		Handle string `json:"handle"`
	} `json:"participants"`
}

func (r *Router) sendAsAccountHandler(w http.ResponseWriter, req *http.Request, accountID, conversationID string) {
	if r.bots == nil {
		r.writeError(w, 503, "multichat holds no bot accounts: MESSAGE_STORE_URL is not set")
		return
	}
	var request struct {
		Body string `json:"body"`
	}
	decoder := json.NewDecoder(io.LimitReader(req.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		r.writeError(w, 400, "body must be {\"body\": \"…\"}: "+err.Error())
		return
	}
	if strings.TrimSpace(request.Body) == "" {
		r.writeError(w, 400, "the message is empty")
		return
	}
	// One message, one row to answer: a body Discord would split is refused
	// rather than sent in parts.
	if length := len([]rune(request.Body)); length > botaccounts.DiscordMessageLimit {
		r.writeError(w, 400, fmt.Sprintf("Discord takes at most %d characters in a message; this is %d", botaccounts.DiscordMessageLimit, length))
		return
	}
	row, err := r.sendAsAccount(accountID, conversationID, request.Body)
	switch {
	case errors.Is(err, errAccountSendRefused):
		r.writeError(w, 403, err.Error())
	case errors.Is(err, errMessageStoreNotFound), errors.Is(err, botaccounts.ErrUnknownAccount):
		r.writeError(w, 404, err.Error())
	case errors.Is(err, botaccounts.ErrNotConnected):
		r.writeError(w, 503, err.Error())
	case err != nil:
		r.writeError(w, 502, err.Error())
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write(row)
	}
}

// sendAsAccount sends body into the conversation as the account and answers
// message-store's row for the sent message.
func (r *Router) sendAsAccount(accountID, conversationID, body string) (json.RawMessage, error) {
	var account messageStoreAccount
	if err := r.messageStoreJSON(http.MethodGet, "/accounts/"+url.PathEscape(accountID), nil, &account); err != nil {
		return nil, err
	}
	if account.Platform != botaccounts.PlatformDiscord {
		return nil, fmt.Errorf("%w: account %s is on %s; only Discord accounts can send from here", errAccountSendRefused, account.ID, account.Platform)
	}
	var conversation messageStoreConversation
	if err := r.messageStoreJSON(http.MethodGet, "/conversations/"+url.PathEscape(conversationID), nil, &conversation); err != nil {
		return nil, err
	}
	if conversation.Source != botaccounts.PlatformDiscord || conversation.Platform != account.Platform {
		return nil, fmt.Errorf("%w: conversation %s is not a Discord channel", errAccountSendRefused, conversation.ID)
	}
	takesPart := false
	for _, participant := range conversation.Participants {
		takesPart = takesPart || participant.Handle == account.Handle
	}
	if !takesPart {
		return nil, fmt.Errorf("%w: account %s is not in conversation %s", errAccountSendRefused, account.ID, conversation.ID)
	}
	result, err := r.bots.Send(account.ID, "", conversation.SourceRef, body, nil)
	if err != nil {
		return nil, err
	}
	if len(result.Messages) != 1 {
		return nil, fmt.Errorf("sent to Discord as %d messages %v, want 1", len(result.Messages), result.MessageIDs)
	}
	sent := result.Messages[0]
	if err := r.fileSentMessages(account, conversation, nil, []botaccounts.SentMessage{sent}); err != nil {
		return nil, fmt.Errorf("sent to Discord as message %s, but message-store did not take it: %w", sent.ID, err)
	}
	var page struct {
		Messages []json.RawMessage `json:"messages"`
	}
	query := url.Values{"source": {botaccounts.PlatformDiscord}, "source_ref": {sent.ID}}
	if err := r.messageStoreJSON(http.MethodGet, "/messages?"+query.Encode(), nil, &page); err != nil {
		return nil, err
	}
	if len(page.Messages) != 1 {
		return nil, fmt.Errorf("sent to Discord as message %s, and message-store holds %d rows for it, want 1", sent.ID, len(page.Messages))
	}
	return page.Messages[0], nil
}

// fileSentMessages files what a bot sent with message-store, in one import:
// the conversation, with the bot and otherParticipantHandles as its
// participants, and each sent message.
func (r *Router) fileSentMessages(account messageStoreAccount, conversation messageStoreConversation, otherParticipantHandles []string, sent []botaccounts.SentMessage) error {
	var run struct {
		ID string `json:"id"`
	}
	if err := r.messageStoreJSON(http.MethodPost, "/imports", map[string]string{"importer": "multichat", "description": "a message sent through multichat as " + account.ID}, &run); err != nil {
		return err
	}
	participants := []map[string]any{{"handle": account.Handle, "display_name": account.DisplayName}}
	for _, handle := range otherParticipantHandles {
		participants = append(participants, map[string]any{"handle": handle})
	}
	messages := make([]map[string]any, 0, len(sent))
	for _, message := range sent {
		messages = append(messages, map[string]any{"conversation_source": conversation.Source, "conversation_source_ref": conversation.SourceRef,
			"sent_at": message.SentAt.UnixMilli(), "sender_handle": message.AuthorID, "kind": "text", "body": message.Content, "body_source": "platform",
			"attachments": []any{}, "reply_to_source_ref": "", "call_seconds": 0, "edited": false, "deleted_on_platform": false,
			"source": botaccounts.PlatformDiscord, "source_ref": message.ID, "source_hash": ""})
	}
	batch := map[string]any{
		"conversations": []map[string]any{{"platform": conversation.Platform, "source": conversation.Source, "source_ref": conversation.SourceRef,
			"title": "", "is_group": conversation.IsGroup, "participants": participants}},
		"messages":          messages,
		"conversation_days": []any{},
	}
	if err := r.messageStoreJSON(http.MethodPost, "/imports/"+url.PathEscape(run.ID)+"/batches", batch, nil); err != nil {
		return err
	}
	return r.messageStoreJSON(http.MethodPost, "/imports/"+url.PathEscape(run.ID)+"/finish", map[string]any{}, nil)
}

// fileDirectMessageSend files a send a bot made to a person by their user id:
// Discord lists no bot's DMs, so the DM channel the send opened is filed here
// as a direct conversation of the bot and the person, with what was sent, or
// message-store's bot importer never reads it.
func (r *Router) fileDirectMessageSend(accountID, userID string, result botaccounts.SendResult) error {
	var account messageStoreAccount
	if err := r.messageStoreJSON(http.MethodGet, "/accounts/"+url.PathEscape(accountID), nil, &account); err != nil {
		return err
	}
	conversation := messageStoreConversation{Platform: account.Platform, Source: botaccounts.PlatformDiscord, SourceRef: result.ChannelID, IsGroup: false}
	return r.fileSentMessages(account, conversation, []string{userID}, result.Messages)
}

// messageStoreJSON calls message-store at path and decodes its answer into out.
func (r *Router) messageStoreJSON(method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, r.messageStoreURL+path, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := r.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("message-store: %w", err)
	}
	defer response.Body.Close()
	answer, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("message-store %s %s: reading the answer: %w", method, path, err)
	}
	routePath := strings.SplitN(path, "?", 2)[0]
	if response.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w: message-store %s %s: %s", errMessageStoreNotFound, method, routePath, strings.TrimSpace(string(answer)))
	}
	if response.StatusCode/100 != 2 {
		return fmt.Errorf("message-store %s %s answered %d: %s", method, routePath, response.StatusCode, strings.TrimSpace(string(answer)))
	}
	if out != nil {
		return json.Unmarshal(answer, out)
	}
	return nil
}
