package matrix

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	homeserver  string
	accessToken string
	httpClient  *http.Client
}

func NewClient(homeserver, accessToken string) *Client {
	return &Client{
		homeserver:  strings.TrimRight(homeserver, "/"),
		accessToken: accessToken,
		httpClient:  &http.Client{},
	}
}

func (c *Client) request(method, path string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequest(method, c.homeserver+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("matrix %s %s: %d %s", method, path, resp.StatusCode, string(data))
	}
	return data, nil
}

// SendMessage sends a text message to a Matrix room
func (c *Client) SendMessage(roomID, body string) (string, error) {
	txnID := fmt.Sprintf("%d", time.Now().UnixNano())
	payload, _ := json.Marshal(map[string]string{
		"msgtype": "m.text",
		"body":    body,
	})
	path := fmt.Sprintf("/_matrix/client/v3/rooms/%s/send/m.room.message/%s",
		url.PathEscape(roomID), txnID)
	data, err := c.request("PUT", path, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	var resp struct {
		EventID string `json:"event_id"`
	}
	json.Unmarshal(data, &resp)
	return resp.EventID, nil
}

// CreateDM creates a direct message room with a user and returns the room ID
func (c *Client) CreateDM(userID string) (string, error) {
	payload, _ := json.Marshal(map[string]interface{}{
		"invite":    []string{userID},
		"is_direct": true,
		"preset":    "trusted_private_chat",
	})
	data, err := c.request("POST", "/_matrix/client/v3/createRoom", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	var resp struct {
		RoomID string `json:"room_id"`
	}
	json.Unmarshal(data, &resp)
	return resp.RoomID, nil
}

// Contact is a Telegram puppet user in Matrix
type Contact struct {
	UserID      string   `json:"user_id"`
	DisplayName string   `json:"display_name"`
	AvatarURL   string   `json:"avatar_url,omitempty"`
	TelegramID  string   `json:"telegram_id,omitempty"`
	Phone       string   `json:"phone,omitempty"`
	Platform    string   `json:"platform"`
	Platforms   []string `json:"platforms,omitempty"`
}

// Conversation is a Matrix room (portal)
type Conversation struct {
	RoomID      string `json:"room_id"`
	Name        string `json:"name"`
	Topic       string `json:"topic,omitempty"`
	AvatarURL   string `json:"avatar_url,omitempty"`
	MemberCount int    `json:"member_count"`
	Platform    string `json:"platform,omitempty"`
}

// Message is a Matrix room event
type Message struct {
	EventID   string `json:"event_id"`
	Sender    string `json:"sender"`
	SenderName string `json:"sender_name,omitempty"`
	Body      string `json:"body"`
	MsgType   string `json:"msg_type"`
	Timestamp int64  `json:"timestamp"`
}

// TelegramPhones fetches phone numbers from the mautrix-telegram bridge DB
// Returns a map of telegram user ID → phone number
type TelegramPhoneProvider interface {
	GetPhones() map[string]string // telegramID -> phone
}

// ListContacts returns all bridged contacts, merging Telegram+WhatsApp by phone
func (c *Client) ListContacts() ([]Contact, error) {
	data, err := c.request("GET", "/_synapse/admin/v2/users?from=0&limit=500&guests=false", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Users []struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayname"`
			Admin       bool   `json:"admin"`
			AvatarURL   string `json:"avatar_url"`
		} `json:"users"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}

	var contacts []Contact
	for _, u := range resp.Users {
		localpart := strings.Split(u.Name, ":")[0]
		var platform, platformID string

		switch {
		case strings.HasPrefix(localpart, "@telegram_") && !strings.HasPrefix(localpart, "@telegrambot"):
			platform = "telegram"
			platformID = strings.TrimPrefix(localpart, "@telegram_")
		case strings.HasPrefix(localpart, "@whatsapp_") && !strings.HasPrefix(localpart, "@whatsappbot"):
			platform = "whatsapp"
			platformID = strings.TrimPrefix(localpart, "@whatsapp_")
		case strings.HasPrefix(localpart, "@signal_") && !strings.HasPrefix(localpart, "@signalbot"):
			platform = "signal"
			platformID = strings.TrimPrefix(localpart, "@signal_")
		case strings.HasPrefix(localpart, "@meta_") && !strings.HasPrefix(localpart, "@metabot"):
			platform = "meta"
			platformID = strings.TrimPrefix(localpart, "@meta_")
		default:
			continue
		}

		name := u.DisplayName
		name = strings.TrimSuffix(name, " (Telegram)")
		name = strings.TrimSuffix(name, " (WA)")
		name = strings.TrimSuffix(name, " (Signal)")
		// Clean up phone-only names
		if (platform == "whatsapp" || platform == "signal") && strings.HasPrefix(name, "+") {
			name = strings.TrimPrefix(name, "+")
		}

		contacts = append(contacts, Contact{
			UserID:      u.Name,
			DisplayName: name,
			AvatarURL:   u.AvatarURL,
			TelegramID:  platformID,
			Platform:    platform,
		})
	}
	return contacts, nil
}

// ListConversations returns rooms the user has joined
func (c *Client) ListConversations() ([]Conversation, error) {
	data, err := c.request("GET", "/_matrix/client/v3/joined_rooms", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		JoinedRooms []string `json:"joined_rooms"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}

	var convos []Conversation
	for _, roomID := range resp.JoinedRooms {
		conv := Conversation{RoomID: roomID}

		// Get room name
		if nameData, err := c.request("GET", "/_matrix/client/v3/rooms/"+url.PathEscape(roomID)+"/state/m.room.name", nil); err == nil {
			var nameEvt struct{ Name string `json:"name"` }
			json.Unmarshal(nameData, &nameEvt)
			conv.Name = nameEvt.Name
		}

		// Get members
		if memberData, err := c.request("GET", "/_matrix/client/v3/rooms/"+url.PathEscape(roomID)+"/members?membership=join", nil); err == nil {
			var memberResp struct {
				Chunk []struct {
					StateKey string `json:"state_key"`
				} `json:"chunk"`
			}
			json.Unmarshal(memberData, &memberResp)
			conv.MemberCount = len(memberResp.Chunk)

			// Detect platform from member user IDs
			for _, m := range memberResp.Chunk {
				if strings.Contains(m.StateKey, "telegram_") {
					conv.Platform = "telegram"
					break
				} else if strings.Contains(m.StateKey, "whatsapp_") {
					conv.Platform = "whatsapp"
					break
				}
			}

			// Detect bridge bot management room
			if conv.Name == "" {
				for _, m := range memberResp.Chunk {
					if strings.HasPrefix(m.StateKey, "@telegrambot") {
						conv.Name = "Telegram Bridge"
					}
				}
			}

			// For DMs with no room name, use the other member's display name
			if conv.Name == "" && len(memberResp.Chunk) <= 3 {
				for _, m := range memberResp.Chunk {
					if m.StateKey != "@admin:chat.kayushkin.com" {
						if profile, err := c.request("GET", "/_matrix/client/v3/profile/"+url.PathEscape(m.StateKey)+"/displayname", nil); err == nil {
							var p struct{ DisplayName string `json:"displayname"` }
							json.Unmarshal(profile, &p)
							name := strings.TrimSuffix(p.DisplayName, " (Telegram)")
							name = strings.TrimSuffix(name, " (WhatsApp)")
							if name != "" && name != "Telegram Bridge Bot" {
								conv.Name = name
								break
							}
						}
					}
				}
			}
		}

		if conv.Name == "" {
			conv.Name = "Unnamed Room"
		}

		convos = append(convos, conv)
	}
	return convos, nil
}

// GetMessages returns messages from a room
func (c *Client) GetMessages(roomID string, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 50
	}
	path := fmt.Sprintf("/_matrix/client/v3/rooms/%s/messages?dir=b&limit=%d&filter=%s",
		url.PathEscape(roomID), limit,
		url.QueryEscape(`{"types":["m.room.message"]}`))

	data, err := c.request("GET", path, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Chunk []struct {
			EventID        string `json:"event_id"`
			Sender         string `json:"sender"`
			OriginServerTS int64  `json:"origin_server_ts"`
			Content        struct {
				Body    string `json:"body"`
				MsgType string `json:"msgtype"`
			} `json:"content"`
		} `json:"chunk"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}

	// Build sender display name cache
	names := map[string]string{}

	var messages []Message
	// Reverse to get chronological order
	for i := len(resp.Chunk) - 1; i >= 0; i-- {
		evt := resp.Chunk[i]
		if evt.Content.Body == "" {
			continue
		}

		senderName := names[evt.Sender]
		if senderName == "" {
			if profile, err := c.request("GET", "/_matrix/client/v3/profile/"+url.PathEscape(evt.Sender)+"/displayname", nil); err == nil {
				var p struct{ DisplayName string `json:"displayname"` }
				json.Unmarshal(profile, &p)
				senderName = strings.TrimSuffix(p.DisplayName, " (Telegram)")
				names[evt.Sender] = senderName
			}
		}

		messages = append(messages, Message{
			EventID:    evt.EventID,
			Sender:     evt.Sender,
			SenderName: senderName,
			Body:       evt.Content.Body,
			MsgType:    evt.Content.MsgType,
			Timestamp:  evt.OriginServerTS,
		})
	}
	return messages, nil
}
