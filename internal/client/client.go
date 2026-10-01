// Package client is the Go client for multichat's own HTTP API, shared by the
// multichat-send CLI and the multichat-mcp server so the two agree on every
// route and payload.
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	BaseURL    string // e.g. http://localhost:8402 — the /api prefix is added here
	Token      string
	HTTPClient *http.Client
}

func New(baseURL, token string) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Token:      token,
		HTTPClient: &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *Client) do(method, path string, body any, into any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.BaseURL+"/api"+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("multichat %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("multichat %s %s: %d %s", method, path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if into != nil {
		if err := json.Unmarshal(data, into); err != nil {
			return fmt.Errorf("multichat %s %s: decode: %w", method, path, err)
		}
	}
	return nil
}

// Identity is one platform account of a person, under the name that platform
// shows for it.
type Identity struct {
	UserID      string `json:"user_id"`
	Platform    string `json:"platform"`
	DisplayName string `json:"display_name"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	LinkedBy    string `json:"linked_by,omitempty"`
	LinkReason  string `json:"link_reason,omitempty"`
}

// UnifiedContact is one person across every platform they reach us on. A
// linked person carries people-store's id and name; an account linked to no
// one is a contact of its own with no person id.
type UnifiedContact struct {
	PersonID        string     `json:"person_id,omitempty"`
	PersonDeletedAt int64      `json:"person_deleted_at,omitempty"`
	DisplayName     string     `json:"display_name"`
	Identities      []Identity `json:"identities"`
	Platforms       []string   `json:"platforms"`
}

func (c *Client) UnifiedContacts() ([]UnifiedContact, error) {
	var out []UnifiedContact
	return out, c.do("GET", "/contacts/unified", nil, &out)
}

type Conversation struct {
	RoomID      string               `json:"room_id"`
	Name        string               `json:"name"`
	MemberCount int                  `json:"member_count"`
	Members     []ConversationMember `json:"members,omitempty"`
	// NamedAfterMembers is true when Name is the members' display names.
	NamedAfterMembers bool   `json:"named_after_members,omitempty"`
	Platform          string `json:"platform,omitempty"`
	LastActivity      int64  `json:"last_activity"`
	LastMessage       string `json:"last_message,omitempty"`
}

type ConversationMember struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name,omitempty"`
}

func (c *Client) Conversations() ([]Conversation, error) {
	var out []Conversation
	return out, c.do("GET", "/conversations", nil, &out)
}

type Message struct {
	EventID    string `json:"event_id"`
	Sender     string `json:"sender"`
	SenderName string `json:"sender_name,omitempty"`
	Body       string `json:"body"`
	MsgType    string `json:"msg_type"`
	Timestamp  int64  `json:"timestamp"`
	IsMe       bool   `json:"is_me,omitempty"`
}

type MessagePage struct {
	Messages []Message `json:"messages"`
	End      string    `json:"end,omitempty"`
	HasMore  bool      `json:"has_more"`
}

func (c *Client) Messages(roomID string, limit int, from string) (*MessagePage, error) {
	query := url.Values{}
	query.Set("limit", strconv.Itoa(limit))
	if from != "" {
		query.Set("from", from)
	}
	var out MessagePage
	return &out, c.do("GET", "/conversations/"+url.PathEscape(roomID)+"/messages?"+query.Encode(), nil, &out)
}

type SendResult struct {
	RoomID      string `json:"room_id"`
	EventID     string `json:"event_id"`
	CreatedRoom bool   `json:"created_room"`
}

// SendDirectMessage messages one person by puppet id, in the DM room shared
// with them (created only if none exists).
func (c *Client) SendDirectMessage(userID, body string) (*SendResult, error) {
	var out SendResult
	return &out, c.do("POST", "/messages/send", map[string]string{"user_id": userID, "body": body}, &out)
}

// SendToRoom posts to a known room (a group, or a DM whose id you already have).
func (c *Client) SendToRoom(roomID, body string) (*SendResult, error) {
	var out struct {
		EventID string `json:"event_id"`
	}
	if err := c.do("POST", "/conversations/"+url.PathEscape(roomID)+"/send", map[string]string{"body": body}, &out); err != nil {
		return nil, err
	}
	return &SendResult{RoomID: roomID, EventID: out.EventID}, nil
}

// ResolveContact finds the one identity on a platform answering to a name:
// either the person's name or that account's own name, case-insensitive and
// exact. None, or more than one, is an error naming what was found — never a
// guess.
// ResolvePersonContact returns a people-store person's one account on
// platform, joined on the person id. None, or more than one, is an error.
func ResolvePersonContact(contacts []UnifiedContact, personID, platform string) (Identity, error) {
	for _, contact := range contacts {
		if contact.PersonID != personID {
			continue
		}
		var onPlatform []Identity
		for _, identity := range contact.Identities {
			if identity.Platform == platform {
				onPlatform = append(onPlatform, identity)
			}
		}
		switch len(onPlatform) {
		case 0:
			return Identity{}, fmt.Errorf("%s (%s) has no %s account linked", personID, contact.DisplayName, platform)
		case 1:
			return onPlatform[0], nil
		default:
			ids := make([]string, len(onPlatform))
			for i, identity := range onPlatform {
				ids[i] = identity.UserID
			}
			return Identity{}, fmt.Errorf("%s has %d %s accounts (%s); pass the user id instead", personID, len(onPlatform), platform, strings.Join(ids, ", "))
		}
	}
	return Identity{}, fmt.Errorf("no contact has person id %s: either people-store has no such person or none of their accounts is linked", personID)
}

func ResolveContact(contacts []UnifiedContact, displayName, platform string) (Identity, error) {
	wanted := strings.TrimSpace(displayName)
	var named int
	var onPlatform []Identity
	for _, contact := range contacts {
		personMatches := strings.EqualFold(strings.TrimSpace(contact.DisplayName), wanted)
		for _, identity := range contact.Identities {
			if !personMatches && !strings.EqualFold(strings.TrimSpace(identity.DisplayName), wanted) {
				continue
			}
			named++
			if identity.Platform == platform {
				onPlatform = append(onPlatform, identity)
			}
		}
	}
	if named == 0 {
		return Identity{}, fmt.Errorf("no contact named %q", displayName)
	}
	if len(onPlatform) == 0 {
		return Identity{}, fmt.Errorf("no contact named %q has a %s identity", displayName, platform)
	}
	if len(onPlatform) > 1 {
		ids := make([]string, len(onPlatform))
		for i, identity := range onPlatform {
			ids[i] = identity.UserID
		}
		return Identity{}, fmt.Errorf("%d %s identities answer to %q (%s); pass the user id instead", len(onPlatform), platform, displayName, strings.Join(ids, ", "))
	}
	return onPlatform[0], nil
}
