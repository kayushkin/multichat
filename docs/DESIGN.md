# MultiChat Design Document

**Version**: 1.0  
**Date**: 2026-03-28  
**Author**: Claude Code  
**URL**: chat.kayushkin.com

## Overview

MultiChat is a unified chat interface demo application that bridges conversations across Telegram and WhatsApp platforms. It provides a single web UI where users can manage conversations from multiple messaging platforms, demonstrating modern multi-platform integration patterns for work presentations.

---

## 1. Architecture Overview

### High-Level System Diagram

```
┌─────────────────┐    ┌───────────────────┐    ┌─────────────────┐
│                 │    │                   │    │                 │
│   React Web UI  │    │    Go Backend     │    │  Telegram Bot   │
│   (TypeScript)  │    │                   │    │      API        │
│                 │    │  ┌─────────────┐  │    │                 │
│                 │◄───┤  │   HTTP API  │  ├────►                 │
│                 │    │  └─────────────┘  │    │                 │
│                 │    │                   │    └─────────────────┘
│                 │    │  ┌─────────────┐  │    
│                 │◄───┤  │ WebSocket   │  │    ┌─────────────────┐
│                 │    │  │ Server      │  │    │                 │
│                 │    │  └─────────────┘  │    │   WhatsApp      │
│                 │    │                   │    │   (whatsmeow)   │
│                 │    │  ┌─────────────┐  │    │                 │
│                 │    │  │  Platform   │  ├────►                 │
│                 │    │  │ Integrations│  │    │                 │
│                 │    │  └─────────────┘  │    │                 │
│                 │    │                   │    └─────────────────┘
│                 │    │  ┌─────────────┐  │    
└─────────────────┘    │  │   SQLite    │  │    
                       │  │  Database   │  │    
                       │  └─────────────┘  │    
                       │                   │    
                       └───────────────────┘    
```

### Component Responsibilities

**React Frontend** (`chat.kayushkin.com`)
- Chat UI with conversation list and message threads
- Platform switcher (Telegram/WhatsApp)
- Real-time message display via WebSocket
- User authentication and session management
- Platform connection management (QR codes, bot setup)

**Go Backend Server**
- HTTP REST API for CRUD operations
- WebSocket server for real-time message push
- Platform integrations (Telegram Bot API, WhatsApp whatsmeow)
- Message persistence and history
- User authentication and session management
- Cross-platform message format normalization

**SQLite Database**
- User accounts and sessions
- Platform connections (user ↔ Telegram/WhatsApp)
- Conversations and message history
- Platform-specific metadata

### WebSocket Architecture

The WebSocket connection provides real-time bidirectional communication:

1. **Client → Server**: Message sends, typing indicators, conversation switches
2. **Server → Client**: Incoming messages, delivery confirmations, connection status updates
3. **Authentication**: WebSocket connection authenticated via session token
4. **Message Flow**: Platform webhooks/polling → Go server → WebSocket → React UI

---

## 2. Database Schema

### Core Tables

```sql
-- Users table
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT UNIQUE NOT NULL,
    email TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    display_name TEXT,
    avatar_url TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- User sessions
CREATE TABLE sessions (
    id TEXT PRIMARY KEY, -- UUID
    user_id INTEGER NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL,
    last_active DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- Platform connections (user links their Telegram/WhatsApp)
CREATE TABLE platform_connections (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    platform TEXT NOT NULL, -- 'telegram', 'whatsapp'
    platform_user_id TEXT NOT NULL, -- Telegram user ID, WhatsApp phone number
    platform_username TEXT, -- @username for Telegram, display name for WhatsApp
    connection_data TEXT, -- JSON: bot token, QR session data, etc.
    status TEXT NOT NULL DEFAULT 'pending', -- 'pending', 'connected', 'disconnected'
    connected_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    UNIQUE(platform, platform_user_id)
);

-- Conversations (maps to Telegram chats, WhatsApp chats/groups)
CREATE TABLE conversations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL, -- Which MultiChat user owns this conversation
    platform TEXT NOT NULL, -- 'telegram', 'whatsapp'
    platform_chat_id TEXT NOT NULL, -- Telegram chat_id, WhatsApp JID
    conversation_type TEXT NOT NULL, -- 'private', 'group', 'channel'
    title TEXT, -- Chat/group name
    description TEXT,
    avatar_url TEXT,
    participant_count INTEGER DEFAULT 0,
    last_message_at DATETIME,
    is_archived BOOLEAN DEFAULT FALSE,
    metadata TEXT, -- JSON: platform-specific data
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    UNIQUE(user_id, platform, platform_chat_id)
);

-- Messages
CREATE TABLE messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_id INTEGER NOT NULL,
    platform_message_id TEXT NOT NULL, -- Telegram message_id, WhatsApp message ID
    sender_platform_id TEXT NOT NULL, -- Sender's platform user ID
    sender_name TEXT,
    sender_avatar_url TEXT,
    content TEXT, -- Message text content
    content_type TEXT DEFAULT 'text', -- 'text', 'image', 'file', 'sticker', 'voice'
    media_url TEXT, -- For images/files/voice messages
    media_metadata TEXT, -- JSON: file size, mime type, dimensions, etc.
    reply_to_message_id INTEGER, -- References another message in this conversation
    is_from_me BOOLEAN DEFAULT FALSE, -- Did the MultiChat user send this?
    timestamp DATETIME NOT NULL, -- Original message timestamp from platform
    received_at DATETIME DEFAULT CURRENT_TIMESTAMP, -- When we received it
    metadata TEXT, -- JSON: platform-specific message data
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE,
    FOREIGN KEY (reply_to_message_id) REFERENCES messages(id) ON DELETE SET NULL,
    UNIQUE(conversation_id, platform_message_id)
);
```

### Indexes for Performance

```sql
-- User lookups
CREATE INDEX idx_users_username ON users(username);
CREATE INDEX idx_users_email ON users(email);

-- Session management
CREATE INDEX idx_sessions_user_id ON sessions(user_id);
CREATE INDEX idx_sessions_expires_at ON sessions(expires_at);

-- Platform connections
CREATE INDEX idx_platform_connections_user_id ON platform_connections(user_id);
CREATE INDEX idx_platform_connections_platform ON platform_connections(platform);
CREATE INDEX idx_platform_connections_status ON platform_connections(status);

-- Conversation queries
CREATE INDEX idx_conversations_user_id ON conversations(user_id);
CREATE INDEX idx_conversations_user_platform ON conversations(user_id, platform);
CREATE INDEX idx_conversations_last_message ON conversations(last_message_at DESC);
CREATE INDEX idx_conversations_platform_chat ON conversations(platform, platform_chat_id);

-- Message queries
CREATE INDEX idx_messages_conversation_id ON messages(conversation_id);
CREATE INDEX idx_messages_timestamp ON messages(timestamp DESC);
CREATE INDEX idx_messages_conversation_timestamp ON messages(conversation_id, timestamp DESC);
CREATE INDEX idx_messages_platform_id ON messages(conversation_id, platform_message_id);
CREATE INDEX idx_messages_sender ON messages(sender_platform_id);
```

### Platform Mapping Strategy

**Telegram → Conversations**:
- `platform_chat_id` = Telegram `chat.id` (can be negative for groups)
- `conversation_type` = "private" (user DM), "group" (group chat), "channel" (broadcast channel)
- `title` = Telegram chat title or user's first_name + last_name
- Handle both incoming messages and sent messages

**WhatsApp → Conversations**:
- `platform_chat_id` = WhatsApp JID (e.g., "1234567890@s.whatsapp.net" for individuals, "group-id@g.us" for groups)
- `conversation_type` = "private" (individual), "group" (group chat)
- `title` = Contact name or group subject
- Parse JID format to determine individual vs group

---

## 3. API Endpoints

### Auth Endpoints

```go
// POST /api/auth/register
type RegisterRequest struct {
    Username    string `json:"username" binding:"required,min=3,max=32"`
    Email       string `json:"email" binding:"required,email"`
    Password    string `json:"password" binding:"required,min=6"`
    DisplayName string `json:"display_name"`
}

type RegisterResponse struct {
    User    UserResponse    `json:"user"`
    Session SessionResponse `json:"session"`
}

// POST /api/auth/login
type LoginRequest struct {
    Username string `json:"username" binding:"required"`
    Password string `json:"password" binding:"required"`
}

type LoginResponse struct {
    User    UserResponse    `json:"user"`
    Session SessionResponse `json:"session"`
}

// POST /api/auth/logout
// No request body
type LogoutResponse struct {
    Success bool `json:"success"`
}

// GET /api/auth/me
// No request body
type MeResponse struct {
    User UserResponse `json:"user"`
}
```

### User Endpoints

```go
// GET /api/users/me
// No request body
type GetUserResponse struct {
    User UserResponse `json:"user"`
}

// PUT /api/users/me
type UpdateUserRequest struct {
    DisplayName *string `json:"display_name,omitempty"`
    Email       *string `json:"email,omitempty" binding:"omitempty,email"`
}

type UpdateUserResponse struct {
    User UserResponse `json:"user"`
}
```

### Platform Connection Endpoints

```go
// GET /api/platforms/connections
// No request body
type GetPlatformConnectionsResponse struct {
    Connections []PlatformConnectionResponse `json:"connections"`
}

// POST /api/platforms/telegram/connect
type ConnectTelegramRequest struct {
    BotToken string `json:"bot_token" binding:"required"`
}

type ConnectTelegramResponse struct {
    Connection PlatformConnectionResponse `json:"connection"`
    BotInfo    TelegramBotInfo           `json:"bot_info"`
}

// POST /api/platforms/whatsapp/connect
// No request body
type ConnectWhatsAppResponse struct {
    Connection PlatformConnectionResponse `json:"connection"`
    QRCode     string                     `json:"qr_code"` // Base64 PNG
}

// GET /api/platforms/whatsapp/status/{connection_id}
// No request body
type WhatsAppStatusResponse struct {
    Status string `json:"status"` // 'connecting', 'connected', 'disconnected'
    QRCode string `json:"qr_code,omitempty"` // New QR if needed
}

// DELETE /api/platforms/connections/{connection_id}
// No request body
type DisconnectPlatformResponse struct {
    Success bool `json:"success"`
}
```

### Conversation Endpoints

```go
// GET /api/conversations
type GetConversationsRequest struct {
    Platform string `form:"platform,omitempty"` // Filter by platform
    Limit    int    `form:"limit,omitempty"`
    Offset   int    `form:"offset,omitempty"`
}

type GetConversationsResponse struct {
    Conversations []ConversationResponse `json:"conversations"`
    Total         int                    `json:"total"`
}

// GET /api/conversations/{conversation_id}
// No request body
type GetConversationResponse struct {
    Conversation ConversationResponse `json:"conversation"`
}

// PUT /api/conversations/{conversation_id}
type UpdateConversationRequest struct {
    IsArchived *bool `json:"is_archived,omitempty"`
}

type UpdateConversationResponse struct {
    Conversation ConversationResponse `json:"conversation"`
}
```

### Message Endpoints

```go
// GET /api/conversations/{conversation_id}/messages
type GetMessagesRequest struct {
    Limit     int       `form:"limit,omitempty"`
    Before    *int      `form:"before,omitempty"` // Message ID cursor
    After     *int      `form:"after,omitempty"`  // Message ID cursor
    Timestamp *time.Time `form:"timestamp,omitempty"`
}

type GetMessagesResponse struct {
    Messages []MessageResponse `json:"messages"`
    HasMore  bool              `json:"has_more"`
}

// POST /api/conversations/{conversation_id}/messages
type SendMessageRequest struct {
    Content     string `json:"content" binding:"required"`
    ContentType string `json:"content_type,omitempty"` // Default: 'text'
    ReplyToID   *int   `json:"reply_to_id,omitempty"`
}

type SendMessageResponse struct {
    Message MessageResponse `json:"message"`
}

// GET /api/messages/{message_id}
// No request body
type GetMessageResponse struct {
    Message MessageResponse `json:"message"`
}
```

### WebSocket Message Types

```go
// Client → Server
type WSClientMessage struct {
    Type string      `json:"type"`
    Data interface{} `json:"data"`
}

// Supported client message types:
// - "ping" (keepalive)
// - "subscribe_conversation" (data: {"conversation_id": 123})
// - "unsubscribe_conversation" (data: {"conversation_id": 123})
// - "typing_start" (data: {"conversation_id": 123})
// - "typing_stop" (data: {"conversation_id": 123})

// Server → Client
type WSServerMessage struct {
    Type string      `json:"type"`
    Data interface{} `json:"data"`
}

// Supported server message types:
// - "pong" (response to ping)
// - "new_message" (data: MessageResponse)
// - "message_updated" (data: MessageResponse)
// - "conversation_updated" (data: ConversationResponse)
// - "platform_status" (data: PlatformStatusUpdate)
// - "typing" (data: TypingIndicator)
// - "error" (data: ErrorResponse)
```

---

## 4. Type Definitions

### Core Data Models

```go
package models

import "time"

// User represents a MultiChat user account
type User struct {
    ID          int       `json:"id" db:"id"`
    Username    string    `json:"username" db:"username"`
    Email       string    `json:"email" db:"email"`
    PasswordHash string   `json:"-" db:"password_hash"` // Never serialize
    DisplayName *string   `json:"display_name" db:"display_name"`
    AvatarURL   *string   `json:"avatar_url" db:"avatar_url"`
    CreatedAt   time.Time `json:"created_at" db:"created_at"`
    UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// Session represents a user authentication session
type Session struct {
    ID         string    `json:"id" db:"id"`
    UserID     int       `json:"user_id" db:"user_id"`
    CreatedAt  time.Time `json:"created_at" db:"created_at"`
    ExpiresAt  time.Time `json:"expires_at" db:"expires_at"`
    LastActive time.Time `json:"last_active" db:"last_active"`
}

// PlatformConnection represents a user's connection to a messaging platform
type PlatformConnection struct {
    ID                int       `json:"id" db:"id"`
    UserID           int       `json:"user_id" db:"user_id"`
    Platform         string    `json:"platform" db:"platform"`
    PlatformUserID   string    `json:"platform_user_id" db:"platform_user_id"`
    PlatformUsername *string   `json:"platform_username" db:"platform_username"`
    ConnectionData   *string   `json:"-" db:"connection_data"` // JSON, sensitive
    Status           string    `json:"status" db:"status"`
    ConnectedAt      *time.Time `json:"connected_at" db:"connected_at"`
    CreatedAt        time.Time `json:"created_at" db:"created_at"`
    UpdatedAt        time.Time `json:"updated_at" db:"updated_at"`
}

// Conversation represents a chat thread from a messaging platform
type Conversation struct {
    ID               int       `json:"id" db:"id"`
    UserID          int       `json:"user_id" db:"user_id"`
    Platform        string    `json:"platform" db:"platform"`
    PlatformChatID  string    `json:"platform_chat_id" db:"platform_chat_id"`
    ConversationType string    `json:"conversation_type" db:"conversation_type"`
    Title           *string   `json:"title" db:"title"`
    Description     *string   `json:"description" db:"description"`
    AvatarURL       *string   `json:"avatar_url" db:"avatar_url"`
    ParticipantCount int       `json:"participant_count" db:"participant_count"`
    LastMessageAt   *time.Time `json:"last_message_at" db:"last_message_at"`
    IsArchived      bool      `json:"is_archived" db:"is_archived"`
    Metadata        *string   `json:"metadata" db:"metadata"` // JSON
    CreatedAt       time.Time `json:"created_at" db:"created_at"`
    UpdatedAt       time.Time `json:"updated_at" db:"updated_at"`
}

// Message represents a message within a conversation
type Message struct {
    ID                  int       `json:"id" db:"id"`
    ConversationID     int       `json:"conversation_id" db:"conversation_id"`
    PlatformMessageID  string    `json:"platform_message_id" db:"platform_message_id"`
    SenderPlatformID   string    `json:"sender_platform_id" db:"sender_platform_id"`
    SenderName         *string   `json:"sender_name" db:"sender_name"`
    SenderAvatarURL    *string   `json:"sender_avatar_url" db:"sender_avatar_url"`
    Content            *string   `json:"content" db:"content"`
    ContentType        string    `json:"content_type" db:"content_type"`
    MediaURL           *string   `json:"media_url" db:"media_url"`
    MediaMetadata      *string   `json:"media_metadata" db:"media_metadata"` // JSON
    ReplyToMessageID   *int      `json:"reply_to_message_id" db:"reply_to_message_id"`
    IsFromMe           bool      `json:"is_from_me" db:"is_from_me"`
    Timestamp          time.Time `json:"timestamp" db:"timestamp"`
    ReceivedAt         time.Time `json:"received_at" db:"received_at"`
    Metadata           *string   `json:"metadata" db:"metadata"` // JSON
}
```

### API Response Types

```go
// UserResponse is the public representation of a user
type UserResponse struct {
    ID          int       `json:"id"`
    Username    string    `json:"username"`
    Email       string    `json:"email"`
    DisplayName *string   `json:"display_name"`
    AvatarURL   *string   `json:"avatar_url"`
    CreatedAt   time.Time `json:"created_at"`
}

// SessionResponse contains session info for authenticated responses
type SessionResponse struct {
    ID        string    `json:"id"`
    ExpiresAt time.Time `json:"expires_at"`
}

// PlatformConnectionResponse is the public representation of a platform connection
type PlatformConnectionResponse struct {
    ID                int       `json:"id"`
    Platform         string    `json:"platform"`
    PlatformUserID   string    `json:"platform_user_id"`
    PlatformUsername *string   `json:"platform_username"`
    Status           string    `json:"status"`
    ConnectedAt      *time.Time `json:"connected_at"`
    CreatedAt        time.Time `json:"created_at"`
}

// ConversationResponse is the public representation of a conversation
type ConversationResponse struct {
    ID               int                        `json:"id"`
    Platform         string                     `json:"platform"`
    ConversationType string                     `json:"conversation_type"`
    Title            *string                    `json:"title"`
    Description      *string                    `json:"description"`
    AvatarURL        *string                    `json:"avatar_url"`
    ParticipantCount int                        `json:"participant_count"`
    LastMessageAt    *time.Time                 `json:"last_message_at"`
    LastMessage      *MessageResponse           `json:"last_message,omitempty"`
    IsArchived       bool                       `json:"is_archived"`
    UnreadCount      int                        `json:"unread_count"`
}

// MessageResponse is the public representation of a message
type MessageResponse struct {
    ID                  int       `json:"id"`
    ConversationID     int       `json:"conversation_id"`
    SenderPlatformID   string    `json:"sender_platform_id"`
    SenderName         *string   `json:"sender_name"`
    SenderAvatarURL    *string   `json:"sender_avatar_url"`
    Content            *string   `json:"content"`
    ContentType        string    `json:"content_type"`
    MediaURL           *string   `json:"media_url"`
    MediaMetadata      *MediaMetadata `json:"media_metadata,omitempty"`
    ReplyToMessageID   *int      `json:"reply_to_message_id"`
    ReplyToMessage     *MessageResponse `json:"reply_to_message,omitempty"`
    IsFromMe           bool      `json:"is_from_me"`
    Timestamp          time.Time `json:"timestamp"`
    DeliveryStatus     string    `json:"delivery_status"` // 'sent', 'delivered', 'read'
}

// MediaMetadata contains parsed media information
type MediaMetadata struct {
    FileName    string `json:"file_name,omitempty"`
    MimeType    string `json:"mime_type,omitempty"`
    FileSize    int64  `json:"file_size,omitempty"`
    Width       int    `json:"width,omitempty"`
    Height      int    `json:"height,omitempty"`
    Duration    int    `json:"duration,omitempty"` // For audio/video in seconds
    Thumbnail   string `json:"thumbnail,omitempty"` // URL
}
```

### WebSocket Types

```go
// WebSocket message types
type WSMessageType string

const (
    WSTypePing                    WSMessageType = "ping"
    WSTypePong                    WSMessageType = "pong"
    WSTypeSubscribeConversation   WSMessageType = "subscribe_conversation"
    WSTypeUnsubscribeConversation WSMessageType = "unsubscribe_conversation"
    WSTypeTypingStart            WSMessageType = "typing_start"
    WSTypeTypingStop             WSMessageType = "typing_stop"
    WSTypeNewMessage             WSMessageType = "new_message"
    WSTypeMessageUpdated         WSMessageType = "message_updated"
    WSTypeConversationUpdated    WSMessageType = "conversation_updated"
    WSTypePlatformStatus         WSMessageType = "platform_status"
    WSTypeTyping                 WSMessageType = "typing"
    WSTypeError                  WSMessageType = "error"
)

// WSSubscribeConversation is sent by client to subscribe to conversation updates
type WSSubscribeConversation struct {
    ConversationID int `json:"conversation_id"`
}

// WSTypingIndicator is sent by client/server for typing status
type WSTypingIndicator struct {
    ConversationID int `json:"conversation_id"`
    UserID         int `json:"user_id,omitempty"` // Server → Client only
}

// WSPlatformStatusUpdate notifies of platform connection changes
type WSPlatformStatusUpdate struct {
    Platform     string `json:"platform"`
    ConnectionID int    `json:"connection_id"`
    Status       string `json:"status"`
    Message      string `json:"message,omitempty"`
}

// WSErrorResponse contains error information
type WSErrorResponse struct {
    Code    string `json:"code"`
    Message string `json:"message"`
}
```

### Platform-Specific Types

```go
// Telegram types
type TelegramBotInfo struct {
    ID        int    `json:"id"`
    IsBot     bool   `json:"is_bot"`
    FirstName string `json:"first_name"`
    Username  string `json:"username,omitempty"`
}

type TelegramWebhookUpdate struct {
    UpdateID int             `json:"update_id"`
    Message  *TelegramMessage `json:"message,omitempty"`
    // Add other update types as needed
}

type TelegramMessage struct {
    MessageID int                `json:"message_id"`
    From      *TelegramUser      `json:"from,omitempty"`
    Chat      TelegramChat       `json:"chat"`
    Date      int                `json:"date"`
    Text      string             `json:"text,omitempty"`
    // Add other message fields as needed
}

type TelegramUser struct {
    ID        int    `json:"id"`
    IsBot     bool   `json:"is_bot"`
    FirstName string `json:"first_name"`
    LastName  string `json:"last_name,omitempty"`
    Username  string `json:"username,omitempty"`
}

type TelegramChat struct {
    ID   int64  `json:"id"`
    Type string `json:"type"`
    Title string `json:"title,omitempty"`
    Username string `json:"username,omitempty"`
    FirstName string `json:"first_name,omitempty"`
    LastName string `json:"last_name,omitempty"`
}

// WhatsApp types
type WhatsAppMessage struct {
    ID        string                 `json:"id"`
    From      string                 `json:"from"` // JID
    To        string                 `json:"to"`   // JID
    Timestamp time.Time              `json:"timestamp"`
    Body      string                 `json:"body,omitempty"`
    Type      string                 `json:"type"` // text, image, document, etc.
    Media     *WhatsAppMedia         `json:"media,omitempty"`
    Context   *WhatsAppContextInfo   `json:"context,omitempty"` // Reply context
}

type WhatsAppMedia struct {
    URL      string `json:"url"`
    MimeType string `json:"mime_type"`
    SHA256   string `json:"sha256"`
    FileSize int64  `json:"file_size"`
    FileName string `json:"file_name,omitempty"`
}

type WhatsAppContextInfo struct {
    QuotedMessageID string `json:"quoted_message_id"`
    QuotedMessage   string `json:"quoted_message,omitempty"`
}
```

---

## 5. Platform Integration Design

### Common Platform Interface

```go
// PlatformManager defines the interface for messaging platform integrations
type PlatformManager interface {
    // Connection management
    Connect(userID int, connectionData map[string]interface{}) error
    Disconnect(connectionID int) error
    GetStatus(connectionID int) (string, error)
    
    // Message operations
    SendMessage(connectionID int, chatID string, content string) (*PlatformMessage, error)
    GetChatHistory(connectionID int, chatID string, limit int) ([]*PlatformMessage, error)
    
    // Event handling
    SetMessageHandler(handler func(connectionID int, message *PlatformMessage))
    SetStatusHandler(handler func(connectionID int, status string))
    
    // Metadata
    GetPlatformName() string
}

// PlatformMessage is the normalized message format
type PlatformMessage struct {
    ID         string                 `json:"id"`
    ChatID     string                 `json:"chat_id"`
    SenderID   string                 `json:"sender_id"`
    SenderName string                 `json:"sender_name"`
    Content    string                 `json:"content"`
    Type       string                 `json:"type"` // text, image, file, etc.
    Timestamp  time.Time              `json:"timestamp"`
    ReplyTo    *string                `json:"reply_to,omitempty"`
    Media      *PlatformMediaInfo     `json:"media,omitempty"`
    RawData    map[string]interface{} `json:"raw_data"` // Platform-specific data
}

type PlatformMediaInfo struct {
    URL      string `json:"url"`
    Type     string `json:"type"` // image, document, audio, video
    MimeType string `json:"mime_type"`
    FileSize int64  `json:"file_size"`
    FileName string `json:"file_name,omitempty"`
}
```

### Telegram Integration

**Connection Flow**:
1. User provides Telegram Bot Token (obtained from @BotFather)
2. Server validates token via `getMe` API call
3. Store connection data (token, bot info) in `platform_connections`
4. Set webhook URL: `https://chat.kayushkin.com/api/webhooks/telegram/{connection_id}`
5. Start receiving updates via webhooks

**Message Handling**:
- **Incoming**: Webhook → Parse update → Normalize to `PlatformMessage` → Store in DB → Push via WebSocket
- **Outgoing**: API request → Telegram `sendMessage` API → Update delivery status

**Implementation Notes**:
```go
type TelegramManager struct {
    baseURL string // https://api.telegram.org
    client  *http.Client
    db      *sql.DB
}

func (tm *TelegramManager) Connect(userID int, connectionData map[string]interface{}) error {
    botToken := connectionData["bot_token"].(string)
    
    // Validate bot token
    botInfo, err := tm.getBotInfo(botToken)
    if err != nil {
        return fmt.Errorf("invalid bot token: %w", err)
    }
    
    // Store connection
    connection := &PlatformConnection{
        UserID: userID,
        Platform: "telegram",
        PlatformUserID: fmt.Sprintf("%d", botInfo.ID),
        PlatformUsername: &botInfo.Username,
        ConnectionData: marshalJSON(map[string]interface{}{
            "bot_token": botToken,
            "bot_info": botInfo,
        }),
        Status: "connected",
        ConnectedAt: timeNow(),
    }
    
    err = tm.db.CreateConnection(connection)
    if err != nil {
        return err
    }
    
    // Set webhook
    return tm.setWebhook(connection.ID, botToken)
}
```

**Webhook Format**:
- URL: `/api/webhooks/telegram/{connection_id}`
- Method: POST
- Body: Telegram Update JSON
- Security: Validate webhook secret, rate limiting

### WhatsApp Integration (whatsmeow)

**Connection Flow**:
1. User initiates WhatsApp connection
2. Server generates QR code using whatsmeow
3. Return QR code to frontend as base64 image
4. User scans QR with WhatsApp mobile app
5. whatsmeow establishes session, store session data
6. Update connection status to "connected"

**Message Handling**:
- **Incoming**: whatsmeow event → Normalize to `PlatformMessage` → Store in DB → Push via WebSocket  
- **Outgoing**: API request → whatsmeow `SendMessage` → Update delivery status

**Implementation Notes**:
```go
type WhatsAppManager struct {
    client   *whatsmeow.Client
    eventCh  chan whatsmeow.Events
    db       *sql.DB
    sessions map[int]*whatsmeow.Client // connectionID → client
}

func (wm *WhatsAppManager) Connect(userID int, connectionData map[string]interface{}) error {
    // Generate new session
    deviceStore := container.NewSQLiteContainer()
    client := whatsmeow.NewClient(deviceStore, waLog.Stdout("whatsapp", "INFO", true))
    
    // Generate QR code
    qrChan, err := client.GetQRChannel(context.Background())
    if err != nil {
        return err
    }
    
    go func() {
        for evt := range qrChan {
            if evt.Event == "code" {
                // Store QR code for frontend retrieval
                wm.storeQRCode(userID, evt.Code)
            } else if evt.Event == "success" {
                // QR scanned successfully, session established
                phoneNumber := client.Store.ID.User
                wm.updateConnectionStatus(userID, "connected", phoneNumber)
            }
        }
    }()
    
    return client.Connect()
}

func (wm *WhatsAppManager) handleMessage(evt *events.Message) {
    // Normalize WhatsApp message to PlatformMessage
    platformMsg := &PlatformMessage{
        ID:         evt.Info.ID,
        ChatID:     evt.Info.Chat.String(),
        SenderID:   evt.Info.Sender.User,
        SenderName: evt.Info.PushName,
        Content:    evt.Message.GetConversation(),
        Type:       wm.getMessageType(evt.Message),
        Timestamp:  evt.Info.Timestamp,
        RawData:    map[string]interface{}{"whatsapp_info": evt.Info},
    }
    
    // Store and broadcast
    wm.messageHandler(wm.getConnectionID(evt.Info.Chat), platformMsg)
}
```

**WhatsApp JID Handling**:
- Individual: `{phone_number}@s.whatsapp.net`
- Group: `{group_id}@g.us`
- Parse JID to determine conversation type
- Store full JID as `platform_chat_id`

### Message Normalization

Both platforms implement the common `PlatformMessage` format:

```go
func NormalizeTelegramMessage(tgMsg *TelegramMessage) *PlatformMessage {
    return &PlatformMessage{
        ID:        fmt.Sprintf("%d", tgMsg.MessageID),
        ChatID:    fmt.Sprintf("%d", tgMsg.Chat.ID),
        SenderID:  fmt.Sprintf("%d", tgMsg.From.ID),
        SenderName: tgMsg.From.FirstName,
        Content:   tgMsg.Text,
        Type:      "text", // Determine from message content
        Timestamp: time.Unix(int64(tgMsg.Date), 0),
        RawData:   map[string]interface{}{"telegram": tgMsg},
    }
}

func NormalizeWhatsAppMessage(waMsg *WhatsAppMessage) *PlatformMessage {
    return &PlatformMessage{
        ID:        waMsg.ID,
        ChatID:    waMsg.From,
        SenderID:  extractPhoneNumber(waMsg.From),
        SenderName: "", // Extract from contact info or group participant
        Content:   waMsg.Body,
        Type:      waMsg.Type,
        Timestamp: waMsg.Timestamp,
        RawData:   map[string]interface{}{"whatsapp": waMsg},
    }
}
```

---

## 6. Auth Flow

### Registration/Login Flow

```
1. User Registration:
   POST /api/auth/register
   ├── Validate username/email uniqueness
   ├── Hash password (bcrypt)
   ├── Create user record
   ├── Generate session UUID
   ├── Store session with 24h expiry
   └── Return user info + session token

2. User Login:
   POST /api/auth/login
   ├── Validate username/password
   ├── Generate new session UUID
   ├── Store session with 24h expiry
   └── Return user info + session token

3. Session Validation:
   All API requests (except auth endpoints)
   ├── Extract session token from Authorization header
   ├── Validate session exists and not expired
   ├── Update last_active timestamp
   └── Load user context
```

### Session Management

**Session Token Format**: 
- UUID v4 (e.g., `550e8400-e29b-41d4-a716-446655440000`)
- Sent as Bearer token: `Authorization: Bearer <session_id>`
- 24-hour expiry, extended on activity
- Stored in `sessions` table with foreign key to `users`

**Frontend Storage**:
- Session token stored in localStorage
- Automatic logout on 401 responses
- Session renewal before expiry

**Security Measures**:
- Password hashing: bcrypt with cost 12
- Session cleanup: Remove expired sessions daily
- Rate limiting: Login attempts, API requests
- CORS: Only allow chat.kayushkin.com origin

### Platform Connection Management

```
1. Telegram Connection:
   POST /api/platforms/telegram/connect
   ├── User provides bot token
   ├── Validate token with Telegram API
   ├── Store connection with status 'connected'
   ├── Set webhook URL
   └── Return connection info + bot details

2. WhatsApp Connection:
   POST /api/platforms/whatsapp/connect
   ├── Generate whatsmeow session
   ├── Create connection with status 'pending'
   ├── Generate QR code
   ├── Return connection ID + QR code
   └── Wait for QR scan (async status updates via WebSocket)

3. Connection Status Check:
   GET /api/platforms/whatsapp/status/{connection_id}
   ├── Check whatsmeow client status
   ├── Return current status + new QR if needed
   └── Used by frontend for polling during QR flow

4. Disconnect Platform:
   DELETE /api/platforms/connections/{connection_id}
   ├── Validate user owns connection
   ├── Clean up webhooks/sessions
   ├── Mark conversations as disconnected
   └── Update connection status to 'disconnected'
```

**Platform Authentication Security**:
- Telegram bot tokens stored encrypted in `connection_data`
- WhatsApp session data encrypted and stored per-connection
- Platform credentials isolated per user (no cross-user access)
- Webhook URLs include connection-specific secrets

### Authorization Model

**Resource Ownership**:
- Users can only access their own data
- Platform connections are user-scoped
- Conversations are user-scoped (one user per conversation)
- Messages belong to conversations (transitive user ownership)

**Permission Checking**:
```go
func (s *Server) requireUserConversation(userID, conversationID int) (*Conversation, error) {
    conv, err := s.db.GetConversation(conversationID)
    if err != nil {
        return nil, err
    }
    if conv.UserID != userID {
        return nil, ErrForbidden
    }
    return conv, nil
}

func (s *Server) requireUserPlatformConnection(userID, connectionID int) (*PlatformConnection, error) {
    conn, err := s.db.GetPlatformConnection(connectionID)
    if err != nil {
        return nil, err
    }
    if conn.UserID != userID {
        return nil, ErrForbidden
    }
    return conn, nil
}
```

**WebSocket Authentication**:
- Session token passed as query parameter: `/ws?token=<session_id>`
- Validate session before WebSocket upgrade
- Associate WebSocket connection with user context
- Only send updates for user's conversations

---

## Implementation Roadmap

### Phase 1: Foundation
1. **Go project setup**: Module init, basic HTTP server with routing
2. **Database layer**: SQLite schema, migration system, CRUD operations  
3. **Authentication**: Registration/login endpoints, session middleware
4. **Basic API**: User management, platform connections (stub)
5. **Frontend scaffold**: React app, auth forms, basic routing

### Phase 2: Platform Integration
1. **Telegram integration**: Bot API wrapper, webhook handling, message normalization
2. **WhatsApp integration**: whatsmeow client, QR auth flow, message handling
3. **WebSocket server**: Real-time message broadcasting, typing indicators
4. **Message persistence**: Store/retrieve conversation history

### Phase 3: Chat Interface
1. **Conversation list**: Platform-grouped conversations, real-time updates
2. **Message thread**: Chat interface, message sending, media support
3. **Platform switching**: UI to connect/disconnect platforms
4. **Real-time features**: Message delivery status, typing indicators

### Phase 4: Production
1. **Deployment**: Build scripts, systemd service, Nginx config
2. **Security**: Rate limiting, input validation, SSL setup
3. **Monitoring**: Logging, health checks, error reporting
4. **Performance**: Connection pooling, message pagination, caching

---

## Technical Considerations

### Scalability Notes
- SQLite suitable for demo purposes (single-user, low volume)
- WebSocket connections stored in memory (restart loses connections)
- Platform integrations are stateful (WhatsApp sessions)
- Consider Redis for session storage and WebSocket scaling in production

### Error Handling Strategy
- Structured error responses with consistent format
- Platform integration errors don't crash the server
- Graceful degradation when platforms are unavailable
- Comprehensive logging for debugging platform issues

### Security Considerations
- All user inputs validated and sanitized
- Platform credentials encrypted at rest
- Webhook URLs include connection-specific secrets
- CORS configured for production domain only
- Rate limiting on all endpoints

This design document provides a comprehensive blueprint for MultiChat implementation. All components are designed to work together seamlessly while maintaining clean separation of concerns and scalability for future enhancements.