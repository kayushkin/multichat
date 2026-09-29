package matrix

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Client struct {
	homeserver  string
	accessToken string
	httpClient  *http.Client

	// selfUserID is WhoAmI's answer once it has one.
	selfUserIDMutex sync.Mutex
	selfUserID      string
}

func NewClient(homeserver, accessToken string) *Client {
	return &Client{
		homeserver:  strings.TrimRight(homeserver, "/"),
		accessToken: accessToken,
		httpClient:  &http.Client{},
	}
}

func (c *Client) request(method, path string, body io.Reader) ([]byte, error) {
	status, data, err := c.requestWithStatus(method, path, body)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, fmt.Errorf("matrix %s %s: %d %s", method, path, status, string(data))
	}
	return data, nil
}

// requestWithStatus is request for callers that must tell one error status
// from another: it returns the status and body instead of failing on >= 400.
func (c *Client) requestWithStatus(method, path string, body io.Reader) (int, []byte, error) {
	req, err := http.NewRequest(method, c.homeserver+path, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, data, nil
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

// UploadMedia uploads a file to Matrix and returns the mxc:// URI
func (c *Client) UploadMedia(data []byte, contentType, filename string) (string, error) {
	path := fmt.Sprintf("/_matrix/media/v3/upload?filename=%s", url.QueryEscape(filename))
	req, err := http.NewRequest("POST", c.homeserver+path, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	req.Header.Set("Content-Type", contentType)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respData, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("upload failed: %d %s", resp.StatusCode, string(respData))
	}
	var result struct {
		ContentURI string `json:"content_uri"`
	}
	json.Unmarshal(respData, &result)
	return result.ContentURI, nil
}

// SendImage sends an image message to a Matrix room
func (c *Client) SendImage(roomID, mxcURI, filename, contentType string, size int) (string, error) {
	txnID := fmt.Sprintf("%d", time.Now().UnixNano())
	msg := map[string]interface{}{
		"msgtype": "m.image",
		"body":    filename,
		"url":     mxcURI,
		"info": map[string]interface{}{
			"mimetype": contentType,
			"size":     size,
		},
	}
	payload, _ := json.Marshal(msg)
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
	RoomID       string   `json:"room_id"`
	Name         string   `json:"name"`
	Topic        string   `json:"topic,omitempty"`
	AvatarURL    string   `json:"avatar_url,omitempty"`
	MemberCount  int      `json:"member_count"`
	MemberIDs    []string `json:"member_ids,omitempty"`
	Platform     string   `json:"platform,omitempty"`
	LastActivity int64    `json:"last_activity"`
	LastMessage  string   `json:"last_message,omitempty"`
}

// Message is a Matrix room event. GetMessagePage fills the formatting,
// media and reply fields from the event content; GetMessages leaves them "".
type Message struct {
	EventID    string `json:"event_id"`
	Sender     string `json:"sender"`
	SenderName string `json:"sender_name,omitempty"`
	Body       string `json:"body"`
	MsgType    string `json:"msg_type"`
	Timestamp  int64  `json:"timestamp"`
	IsMe       bool   `json:"is_me,omitempty"`
	// Format is "org.matrix.custom.html" when FormattedBody is Matrix HTML,
	// else "".
	Format         string `json:"format"`
	FormattedBody  string `json:"formatted_body"`
	MediaURL       string `json:"media_url"`
	MediaMimetype  string `json:"media_mimetype"`
	ReplyToEventID string `json:"reply_to_event_id"`
	// ReplacesEventID is set on an edit: the message it edits. An edit's
	// body, format, formatted body and media are the edited message's new
	// content (m.new_content), not the "* …" fallback Matrix puts at the top
	// level for clients that do not understand edits.
	ReplacesEventID string `json:"replaces_event_id"`
}

// SelfUserIDs are Matrix user IDs that represent the multichat owner
var SelfUserIDs = map[string]bool{}

// TelegramPhones fetches phone numbers from the mautrix-telegram bridge DB
// Returns a map of telegram user ID → phone number
type TelegramPhoneProvider interface {
	GetPhones() map[string]string // telegramID -> phone
}

// stripBridgeSuffix removes bridge-appended platform labels from display names
func stripBridgeSuffix(name string) string {
	for _, suffix := range []string{" (Telegram)", " (WhatsApp)", " (WA)", " (Signal)", " (Meta)", " (Facebook)", " (Instagram)", " (Messenger)"} {
		name = strings.TrimSuffix(name, suffix)
	}
	return name
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

		platform = PlatformOfUserID(u.Name)
		if platform == "" {
			continue
		}
		platformID = strings.TrimPrefix(localpart, "@"+platform+"_")

		name := stripBridgeSuffix(u.DisplayName)
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

// RoomMember is a member of a Matrix room
type RoomMember struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
	Platform    string `json:"platform,omitempty"`
	IsMe        bool   `json:"is_me,omitempty"`
}

// GetRoomMembers returns joined members of a room with display names
func (c *Client) GetRoomMembers(roomID string) ([]RoomMember, error) {
	data, err := c.request("GET", "/_matrix/client/v3/rooms/"+url.PathEscape(roomID)+"/members?membership=join", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Chunk []struct {
			StateKey string `json:"state_key"`
			Content  struct {
				DisplayName string `json:"displayname"`
			} `json:"content"`
		} `json:"chunk"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}

	var members []RoomMember
	for _, m := range resp.Chunk {
		// Skip bridge bots and the admin user
		local := strings.Split(m.StateKey, ":")[0]
		if strings.HasSuffix(local, "bot") || m.StateKey == "@admin:chat.kayushkin.com" {
			continue
		}

		name := stripBridgeSuffix(m.Content.DisplayName)
		if name == "" {
			name = m.StateKey
		}

		platform := PlatformOfUserID(m.StateKey)

		members = append(members, RoomMember{
			UserID:      m.StateKey,
			DisplayName: name,
			Platform:    platform,
			IsMe:        SelfUserIDs[m.StateKey],
		})
	}
	return members, nil
}

// MetaPlatformMap maps room IDs to facebook/instagram platform
type MetaPlatformMap map[string]string

// ListConversations returns rooms the user has joined
func (c *Client) ListConversations(metaPlatforms MetaPlatformMap) ([]Conversation, error) {
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
			var nameEvt struct {
				Name string `json:"name"`
			}
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
			// Collect non-bot, non-self member IDs
			for _, m := range memberResp.Chunk {
				local := strings.Split(m.StateKey, ":")[0]
				if !strings.HasSuffix(local, "bot") && m.StateKey != "@admin:chat.kayushkin.com" && !SelfUserIDs[m.StateKey] {
					conv.MemberIDs = append(conv.MemberIDs, m.StateKey)
				}
			}
			// Real member count = other people + you
			conv.MemberCount = len(conv.MemberIDs) + 1

			// Detect platform from member user IDs
			for _, m := range memberResp.Chunk {
				switch {
				case PlatformOfUserID(m.StateKey) != "":
					conv.Platform = PlatformOfUserID(m.StateKey)
				case strings.Contains(m.StateKey, "facebook_"):
					conv.Platform = "facebook"
				case strings.Contains(m.StateKey, "instagram_"):
					conv.Platform = "instagram"
				default:
					continue
				}
				break
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
							var p struct {
								DisplayName string `json:"displayname"`
							}
							json.Unmarshal(profile, &p)
							name := stripBridgeSuffix(p.DisplayName)
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

		// Skip bridge bot/management rooms and unidentified rooms
		nameLower := strings.ToLower(conv.Name)
		if strings.Contains(nameLower, "bridge") {
			continue
		}
		if conv.Platform == "" {
			continue
		}

		// Resolve meta → facebook/instagram
		if conv.Platform == "meta" && metaPlatforms != nil {
			if p, ok := metaPlatforms[conv.RoomID]; ok {
				conv.Platform = p
			}
		}

		// Fetch latest message for sorting + preview
		msgPath := fmt.Sprintf("/_matrix/client/v3/rooms/%s/messages?dir=b&limit=1&filter=%s",
			url.PathEscape(roomID),
			url.QueryEscape(`{"types":["m.room.message"]}`))
		if msgData, err := c.request("GET", msgPath, nil); err == nil {
			var msgResp struct {
				Chunk []struct {
					OriginServerTS int64           `json:"origin_server_ts"`
					Content        json.RawMessage `json:"content"`
				} `json:"chunk"`
			}
			if json.Unmarshal(msgData, &msgResp) == nil && len(msgResp.Chunk) > 0 {
				conv.LastActivity = msgResp.Chunk[0].OriginServerTS
				content, err := ParseMessageContent(msgResp.Chunk[0].Content)
				if err != nil {
					return nil, fmt.Errorf("last message of %s: %w", roomID, err)
				}
				if content.NewContent != nil {
					// An edit: preview the edited text, not the "* …" fallback.
					content = *content.NewContent
				}
				conv.LastMessage = lastMessagePreview(content.Body)
			}
		}

		convos = append(convos, conv)
	}
	// Sort by most recent activity
	sort.Slice(convos, func(i, j int) bool {
		return convos[i].LastActivity > convos[j].LastActivity
	})
	return convos, nil
}

// GetMessages returns messages from a room
// MessagePage is a page of messages with pagination info
type MessagePage struct {
	Messages []Message `json:"messages"`
	End      string    `json:"end,omitempty"`
	HasMore  bool      `json:"has_more"`
}

func (c *Client) GetMessagePage(roomID string, limit int, from string) (*MessagePage, error) {
	if limit <= 0 {
		limit = 50
	}
	path := fmt.Sprintf("/_matrix/client/v3/rooms/%s/messages?dir=b&limit=%d&filter=%s",
		url.PathEscape(roomID), limit,
		url.QueryEscape(`{"types":["m.room.message"]}`))
	if from != "" {
		path += "&from=" + url.QueryEscape(from)
	}

	data, err := c.request("GET", path, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Chunk []struct {
			EventID        string          `json:"event_id"`
			Sender         string          `json:"sender"`
			OriginServerTS int64           `json:"origin_server_ts"`
			Content        json.RawMessage `json:"content"`
		} `json:"chunk"`
		End string `json:"end"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}

	names := map[string]string{}
	var messages []Message
	for i := len(resp.Chunk) - 1; i >= 0; i-- {
		evt := resp.Chunk[i]
		content, err := ParseMessageContent(evt.Content)
		if err != nil {
			return nil, fmt.Errorf("event %s: %w", evt.EventID, err)
		}
		replacesEventID := content.ReplacesEventID
		if content.NewContent != nil {
			content = *content.NewContent
		}
		if content.Body == "" {
			continue
		}
		senderName := names[evt.Sender]
		if senderName == "" {
			if profile, err := c.request("GET", "/_matrix/client/v3/profile/"+url.PathEscape(evt.Sender)+"/displayname", nil); err == nil {
				var p struct {
					DisplayName string `json:"displayname"`
				}
				json.Unmarshal(profile, &p)
				senderName = stripBridgeSuffix(p.DisplayName)
				names[evt.Sender] = senderName
			}
		}
		messages = append(messages, Message{
			EventID:         evt.EventID,
			Sender:          evt.Sender,
			SenderName:      senderName,
			Body:            content.Body,
			MsgType:         content.MsgType,
			Timestamp:       evt.OriginServerTS,
			IsMe:            SelfUserIDs[evt.Sender],
			Format:          content.Format,
			FormattedBody:   content.FormattedBody,
			MediaURL:        content.MediaURL,
			MediaMimetype:   content.MediaMimetype,
			ReplyToEventID:  content.ReplyToEventID,
			ReplacesEventID: replacesEventID,
		})
	}

	hasMore := len(resp.Chunk) >= limit && resp.End != ""
	return &MessagePage{Messages: messages, End: resp.End, HasMore: hasMore}, nil
}

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
				var p struct {
					DisplayName string `json:"displayname"`
				}
				json.Unmarshal(profile, &p)
				senderName = stripBridgeSuffix(p.DisplayName)
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
			IsMe:       SelfUserIDs[evt.Sender],
		})
	}
	return messages, nil
}

// OpenMedia asks Synapse's authenticated media API for a file — the whole
// file, or with width and height both above zero a thumbnail scaled to fit
// them — and returns the response unread, whatever its status. The caller
// streams the body and must close it. serverName and mediaID are the two
// parts of an mxc://serverName/mediaID URI.
func (c *Client) OpenMedia(ctx context.Context, serverName, mediaID string, width, height int) (*http.Response, error) {
	path := "/_matrix/client/v1/media/download/" + url.PathEscape(serverName) + "/" + url.PathEscape(mediaID)
	if width > 0 && height > 0 {
		path = "/_matrix/client/v1/media/thumbnail/" + url.PathEscape(serverName) + "/" + url.PathEscape(mediaID) +
			"?width=" + strconv.Itoa(width) + "&height=" + strconv.Itoa(height) + "&method=scale"
	}
	req, err := http.NewRequestWithContext(ctx, "GET", c.homeserver+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("matrix media %s/%s: %w", serverName, mediaID, err)
	}
	return resp, nil
}

// SendReaction reacts to a message as this account (m.reaction, an
// m.annotation with the given key: a unicode emoji, or an mxc:// URI for a
// custom emoji) and returns the reaction's event id. A bridge relays it to the
// platform as our own account.
func (c *Client) SendReaction(roomID, targetEventID, key string) (string, error) {
	payload, err := json.Marshal(map[string]any{
		"m.relates_to": map[string]string{"rel_type": "m.annotation", "event_id": targetEventID, "key": key},
	})
	if err != nil {
		return "", err
	}
	path := fmt.Sprintf("/_matrix/client/v3/rooms/%s/send/m.reaction/%d", url.PathEscape(roomID), time.Now().UnixNano())
	data, err := c.request("PUT", path, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	var resp struct {
		EventID string `json:"event_id"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("send reaction: %w", err)
	}
	if resp.EventID == "" {
		return "", fmt.Errorf("send reaction: no event_id in %s", data)
	}
	return resp.EventID, nil
}

// Redact redacts an event this account may redact and returns the
// redaction's event id. Redacting a reaction takes it back.
func (c *Client) Redact(roomID, eventID string) (string, error) {
	path := fmt.Sprintf("/_matrix/client/v3/rooms/%s/redact/%s/%d", url.PathEscape(roomID), url.PathEscape(eventID), time.Now().UnixNano())
	data, err := c.request("PUT", path, bytes.NewReader([]byte(`{}`)))
	if err != nil {
		return "", err
	}
	var resp struct {
		EventID string `json:"event_id"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("redact: %w", err)
	}
	if resp.EventID == "" {
		return "", fmt.Errorf("redact: no event_id in %s", data)
	}
	return resp.EventID, nil
}
