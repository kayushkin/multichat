package matrix

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

	// conversations is what ListConversations answers from.
	conversations conversationCache
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
	respData, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("upload: %w", err)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("upload failed: %d %s", resp.StatusCode, string(respData))
	}
	var result struct {
		ContentURI string `json:"content_uri"`
	}
	if err := json.Unmarshal(respData, &result); err != nil {
		return "", fmt.Errorf("upload: %w", err)
	}
	if !strings.HasPrefix(result.ContentURI, "mxc://") {
		return "", fmt.Errorf("upload: no mxc:// content_uri in %s", respData)
	}
	return result.ContentURI, nil
}

// MediaUploadSizeLimit is the largest file Synapse takes, in bytes, from its
// media config (m.upload.size); 0 when it sets none.
func (c *Client) MediaUploadSizeLimit() (int64, error) {
	data, err := c.request("GET", "/_matrix/client/v1/media/config", nil)
	if err != nil {
		return 0, fmt.Errorf("media config: %w", err)
	}
	var config struct {
		UploadSize int64 `json:"m.upload.size"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return 0, fmt.Errorf("media config: %w", err)
	}
	return config.UploadSize, nil
}

// SentFile describes a file to send as a Matrix message.
type SentFile struct {
	MXCURI   string
	FileName string
	// Caption is the text sent with the file, "" for none. Per MSC2530 it
	// goes in body with the name in filename; bridges send it as the text of
	// the same message (mautrix-discord does).
	Caption  string
	MimeType string
	Size     int
	// Width and Height are an image's size in pixels, 0 when not known.
	Width, Height int
}

// FileMessageType is the Matrix msgtype for a file of this MIME type.
func FileMessageType(mimeType string) string {
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return "m.image"
	case strings.HasPrefix(mimeType, "video/"):
		return "m.video"
	case strings.HasPrefix(mimeType, "audio/"):
		return "m.audio"
	default:
		return "m.file"
	}
}

// SendFile sends an uploaded file to a room as an image, video, audio or
// file message, with its caption, and returns the event id.
func (c *Client) SendFile(roomID string, file SentFile) (string, error) {
	info := map[string]any{"mimetype": file.MimeType, "size": file.Size}
	if file.Width > 0 && file.Height > 0 {
		info["w"], info["h"] = file.Width, file.Height
	}
	content := map[string]any{
		"msgtype":  FileMessageType(file.MimeType),
		"body":     file.FileName,
		"filename": file.FileName,
		"url":      file.MXCURI,
		"info":     info,
	}
	if file.Caption != "" {
		content["body"] = file.Caption
	}
	payload, err := json.Marshal(content)
	if err != nil {
		return "", err
	}
	path := fmt.Sprintf("/_matrix/client/v3/rooms/%s/send/m.room.message/%d", url.PathEscape(roomID), time.Now().UnixNano())
	data, err := c.request("PUT", path, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	var resp struct {
		EventID string `json:"event_id"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("send file: %w", err)
	}
	if resp.EventID == "" {
		return "", fmt.Errorf("send file: no event_id in %s", data)
	}
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
	// Members is every joined member but us and the bridge bots.
	Members []ConversationMember `json:"members,omitempty"`
	// NamedAfterMembers is true when the room has no name of its own and Name
	// is its members' display names joined with ", ".
	NamedAfterMembers bool   `json:"named_after_members,omitempty"`
	Platform          string `json:"platform,omitempty"`
	LastActivity      int64  `json:"last_activity"`
	LastMessage       string `json:"last_message,omitempty"`
	// DirectMessagePartnerUserID is the other person in a one-to-one direct
	// message room, and "" in any other room: a group, a server channel, or a
	// DM whose bridge does not say it is one (Telegram).
	DirectMessagePartnerUserID string `json:"direct_message_partner_user_id,omitempty"`
	// Visibility is the operator's choice for the room — "hidden", "shown",
	// or "auto" to follow the auto-hide rule — and Hidden the result. The
	// API fills both from multichat's own database; the Matrix read leaves
	// them empty.
	Visibility string `json:"visibility"`
	Hidden     bool   `json:"hidden"`
}

// ConversationMember is one person in a room: their Matrix user id and the
// display name their member event carries in that room, with the bridge's
// suffix taken off. DisplayName is "" when the event has none.
type ConversationMember struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name,omitempty"`
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
	Format        string `json:"format"`
	FormattedBody string `json:"formatted_body"`
	MediaURL      string `json:"media_url"`
	MediaMimetype string `json:"media_mimetype"`
	// FileName is a media message's file name when the sender set one apart
	// from Body; then Body is the caption sent with the file. "" otherwise.
	FileName       string `json:"file_name,omitempty"`
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
// roomNameFromMembers joins the display names of members, in order, with
// ", ". A member with no display name is left out.
func roomNameFromMembers(members []ConversationMember) string {
	var names []string
	for _, member := range members {
		if member.DisplayName != "" {
			names = append(names, member.DisplayName)
		}
	}
	return strings.Join(names, ", ")
}

func stripBridgeSuffix(name string) string {
	for _, suffix := range []string{" (Telegram)", " (WhatsApp)", " (WA)", " (Signal)", " (Meta)", " (Facebook)", " (Instagram)", " (Messenger)"} {
		name = strings.TrimSuffix(name, suffix)
	}
	return name
}

// ListContacts returns every bridge puppet Synapse knows, one per identity.
// It reads every page of the admin user list: one page of 500 once hid three
// quarters of them.
func (c *Client) ListContacts() ([]Contact, error) {
	type synapseUser struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayname"`
		Admin       bool   `json:"admin"`
		AvatarURL   string `json:"avatar_url"`
	}
	var users []synapseUser
	from := "0"
	for {
		data, err := c.request("GET", "/_synapse/admin/v2/users?from="+url.QueryEscape(from)+"&limit=1000&guests=false", nil)
		if err != nil {
			return nil, err
		}
		var page struct {
			Users     []synapseUser   `json:"users"`
			NextToken json.RawMessage `json:"next_token"`
		}
		if err := json.Unmarshal(data, &page); err != nil {
			return nil, err
		}
		users = append(users, page.Users...)
		next := strings.Trim(string(page.NextToken), `"`)
		if next == "" || next == "null" || len(page.Users) == 0 {
			break
		}
		from = next
	}
	var contacts []Contact
	for _, u := range users {
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

// listedRoom is what conversationOf learns about one room from Synapse,
// before the per-request facts (which Meta app, which Discord rooms are DMs)
// are applied by finish.
type listedRoom struct {
	conversation Conversation
	// bridgeRoomType is m.bridge's com.beeper.room_type, read only for rooms
	// with one other member, where it decides whether the room is a DM.
	bridgeRoomType string
}

// finish applies what the bridge databases say now: directMessageRoomIDs
// names DM rooms whose bridge does not mark them in m.bridge (Discord); the
// mautrix-go bridges mark theirs with com.beeper.room_type "dm".
func (room listedRoom) finish(metaPlatforms MetaPlatformMap, directMessageRoomIDs map[string]bool) Conversation {
	conv := room.conversation
	if len(conv.Members) == 1 && (directMessageRoomIDs[conv.RoomID] || room.bridgeRoomType == "dm") {
		conv.DirectMessagePartnerUserID = conv.Members[0].UserID
	}
	// Resolve meta → facebook/instagram
	if conv.Platform == "meta" && metaPlatforms != nil {
		if p, ok := metaPlatforms[conv.RoomID]; ok {
			conv.Platform = p
		}
	}
	return conv
}

// conversationOf reads one joined room from Synapse. listed is false for a
// room the conversation list leaves out: a bridge's management room, or a
// room no bridge carries.
func (c *Client) conversationOf(roomID string) (room listedRoom, listed bool, err error) {
	conv := Conversation{RoomID: roomID}

	nameData, err := c.request("GET", "/_matrix/client/v3/rooms/"+url.PathEscape(roomID)+"/state/m.room.name", nil)
	switch {
	case isMatrixNotFound(err):
		// The room has no name of its own.
	case err != nil:
		return listedRoom{}, false, err
	default:
		var nameEvt struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(nameData, &nameEvt); err != nil {
			return listedRoom{}, false, fmt.Errorf("m.room.name of %s: %w", roomID, err)
		}
		conv.Name = nameEvt.Name
	}

	memberData, err := c.request("GET", "/_matrix/client/v3/rooms/"+url.PathEscape(roomID)+"/members?membership=join", nil)
	if err != nil {
		return listedRoom{}, false, err
	}
	var memberResp struct {
		Chunk []struct {
			StateKey string `json:"state_key"`
			Content  struct {
				DisplayName string `json:"displayname"`
			} `json:"content"`
		} `json:"chunk"`
	}
	if err := json.Unmarshal(memberData, &memberResp); err != nil {
		return listedRoom{}, false, fmt.Errorf("members of %s: %w", roomID, err)
	}
	// Collect non-bot, non-self member IDs
	for _, m := range memberResp.Chunk {
		local := strings.Split(m.StateKey, ":")[0]
		if !strings.HasSuffix(local, "bot") && m.StateKey != "@admin:chat.kayushkin.com" && !SelfUserIDs[m.StateKey] {
			conv.Members = append(conv.Members, ConversationMember{UserID: m.StateKey, DisplayName: stripBridgeSuffix(m.Content.DisplayName)})
		}
	}
	// Real member count = other people + you
	conv.MemberCount = len(conv.Members) + 1

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

	// With no room name, name the room after the other people in it, as
	// Messenger does. Members already leaves out bridge bots and our own
	// puppets, which a bridged chat carries alongside the real members.
	if conv.Name == "" {
		conv.Name = roomNameFromMembers(conv.Members)
		conv.NamedAfterMembers = conv.Name != ""
	}
	if conv.Name == "" {
		conv.Name = "Unnamed Room"
	}

	// Skip bridge bot/management rooms and unidentified rooms
	if strings.Contains(strings.ToLower(conv.Name), "bridge") || conv.Platform == "" {
		return listedRoom{}, false, nil
	}

	room = listedRoom{conversation: conv}
	if len(conv.Members) == 1 {
		room.bridgeRoomType, err = c.bridgeRoomType(roomID)
		if err != nil {
			return listedRoom{}, false, fmt.Errorf("m.bridge state of %s: %w", roomID, err)
		}
	}

	// Fetch latest message for sorting + preview
	msgPath := fmt.Sprintf("/_matrix/client/v3/rooms/%s/messages?dir=b&limit=1&filter=%s",
		url.PathEscape(roomID),
		url.QueryEscape(`{"types":["m.room.message"]}`))
	msgData, err := c.request("GET", msgPath, nil)
	if err != nil {
		return listedRoom{}, false, err
	}
	var msgResp struct {
		Chunk []struct {
			OriginServerTS int64           `json:"origin_server_ts"`
			Content        json.RawMessage `json:"content"`
		} `json:"chunk"`
	}
	if err := json.Unmarshal(msgData, &msgResp); err != nil {
		return listedRoom{}, false, fmt.Errorf("last message of %s: %w", roomID, err)
	}
	if len(msgResp.Chunk) > 0 {
		room.conversation.LastActivity = msgResp.Chunk[0].OriginServerTS
		content, err := ParseMessageContent(msgResp.Chunk[0].Content)
		if err != nil {
			return listedRoom{}, false, fmt.Errorf("last message of %s: %w", roomID, err)
		}
		if content.NewContent != nil {
			// An edit: preview the edited text, not the "* …" fallback.
			content = *content.NewContent
		}
		room.conversation.LastMessage = lastMessagePreview(content.Body)
	}
	return room, true, nil
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
			FileName:        content.FileName,
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

// ExcludedFromTimeline says whether the room's bridge marks it as no chat of
// its own: the mautrix bridges set com.beeper.exclude_from_timeline on the
// m.room.topic of a room such as WhatsApp's status@broadcast, which carries
// contacts' status updates rather than a conversation. A room with no topic
// is not marked.
func (c *Client) ExcludedFromTimeline(roomID string) (bool, error) {
	path := "/_matrix/client/v3/rooms/" + url.PathEscape(roomID) + "/state/m.room.topic"
	status, data, err := c.requestWithStatus("GET", path, nil)
	if err != nil {
		return false, err
	}
	if status == http.StatusNotFound {
		return false, nil
	}
	if status >= 400 {
		return false, fmt.Errorf("matrix GET %s: %d %s", path, status, string(data))
	}
	var topic struct {
		ExcludeFromTimeline bool `json:"com.beeper.exclude_from_timeline"`
	}
	if err := json.Unmarshal(data, &topic); err != nil {
		return false, fmt.Errorf("m.room.topic of %s: %w", roomID, err)
	}
	return topic.ExcludeFromTimeline, nil
}

// bridgeRoomType is com.beeper.room_type from the room's m.bridge state ("dm"
// for a direct message on the mautrix-go bridges), or "" when no m.bridge
// event sets it. Each bridge writes m.bridge under a state key of its own
// ("chat.kayushkin.com/whatsapp", "fi.mau.discord://…"), so the whole state is
// read rather than one key guessed.
func (c *Client) bridgeRoomType(roomID string) (string, error) {
	data, err := c.request("GET", "/_matrix/client/v3/rooms/"+url.PathEscape(roomID)+"/state", nil)
	if err != nil {
		return "", err
	}
	var events []struct {
		Type    string `json:"type"`
		Content struct {
			RoomType string `json:"com.beeper.room_type"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &events); err != nil {
		return "", err
	}
	for _, event := range events {
		if event.Type == "m.bridge" && event.Content.RoomType != "" {
			return event.Content.RoomType, nil
		}
	}
	return "", nil
}
