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

// Identity is one platform account of a person.
type Identity struct {
	UserID    string `json:"user_id"`
	Platform  string `json:"platform"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

// UnifiedContact is one person across every platform they reach us on.
type UnifiedContact struct {
	DisplayName string     `json:"display_name"`
	Identities  []Identity `json:"identities"`
	Platforms   []string   `json:"platforms"`
}

func (c *Client) UnifiedContacts() ([]UnifiedContact, error) {
	var out []UnifiedContact
	return out, c.do("GET", "/contacts/unified", nil, &out)
}

type Conversation struct {
	RoomID       string   `json:"room_id"`
	Name         string   `json:"name"`
	MemberCount  int      `json:"member_count"`
	MemberIDs    []string `json:"member_ids,omitempty"`
	Platform     string   `json:"platform,omitempty"`
	LastActivity int64    `json:"last_activity"`
	LastMessage  string   `json:"last_message,omitempty"`
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

// ResolveContact finds the one identity for a person on a platform. The
// display name match is case-insensitive and exact; more than one person
// with that name, or none, or no identity on that platform, is an error
// naming what was found — never a guess.
func ResolveContact(contacts []UnifiedContact, displayName, platform string) (Identity, error) {
	var matches []UnifiedContact
	for _, contact := range contacts {
		if strings.EqualFold(strings.TrimSpace(contact.DisplayName), strings.TrimSpace(displayName)) {
			matches = append(matches, contact)
		}
	}
	if len(matches) == 0 {
		return Identity{}, fmt.Errorf("no contact named %q", displayName)
	}
	if len(matches) > 1 {
		return Identity{}, fmt.Errorf("%d contacts named %q; pass the user id instead", len(matches), displayName)
	}
	var onPlatform []Identity
	for _, identity := range matches[0].Identities {
		if identity.Platform == platform {
			onPlatform = append(onPlatform, identity)
		}
	}
	if len(onPlatform) == 0 {
		return Identity{}, fmt.Errorf("%q has no %s identity (has: %s)", matches[0].DisplayName, platform, strings.Join(matches[0].Platforms, ", "))
	}
	if len(onPlatform) > 1 {
		ids := make([]string, len(onPlatform))
		for i, identity := range onPlatform {
			ids[i] = identity.UserID
		}
		return Identity{}, fmt.Errorf("%q has %d %s identities (%s); pass the user id instead", matches[0].DisplayName, len(onPlatform), platform, strings.Join(ids, ", "))
	}
	return onPlatform[0], nil
}
