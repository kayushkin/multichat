# API Reference

Base URL: `https://<your-domain>/api`

All endpoints (except `/auth/login` and `/health`) require authentication via:
- `Authorization: Bearer <token>` header, or
- `multichat_token` cookie

## Authentication

### POST /auth/login

Validate token and set session cookie.

**Request:**
```json
{ "token": "your-secret-token" }
```

**Response:**
```json
{ "status": "ok" }
```

Sets `multichat_token` cookie (30-day expiry, Secure, SameSite=Lax).

**Errors:** `401` if token is invalid.

---

## Health

### GET /health

Health check endpoint. No authentication required.

**Response:**
```json
{ "status": "ok" }
```

---

## Contacts

### GET /contacts

List all contacts across all connected platforms. Performs cross-platform identity merging (phone number matching for WhatsApp/Telegram, Meta platform resolution for Facebook/Instagram).

**Response:**
```json
[
  {
    "user_id": "@telegram_12345:chat.example.com",
    "display_name": "John Doe",
    "platform": "telegram",
    "telegram_id": "12345",
    "phone": "15551234567",
    "platforms": ["telegram", "whatsapp"]
  }
]
```

### POST /contacts/add

Add a local contact (not from Matrix).

**Request:**
```json
{
  "display_name": "Jane Smith",
  "platform": "phone",
  "identifier": "+15559876543"
}
```

**Response:**
```json
{
  "id": 1,
  "display_name": "Jane Smith",
  "platform": "phone",
  "identifier": "+15559876543",
  "user_id": "local_1"
}
```

### GET /contacts/unified

List contacts grouped by identity across platforms. Contacts with matching display names are merged.

**Response:**
```json
[
  {
    "display_name": "John Doe",
    "identities": [
      { "user_id": "@telegram_12345:chat.example.com", "platform": "telegram", "avatar_url": "mxc://..." },
      { "user_id": "@whatsapp_15551234567:chat.example.com", "platform": "whatsapp" }
    ],
    "platforms": ["telegram", "whatsapp"]
  }
]
```

Sorted by number of platforms (descending), then alphabetically.

---

## Conversations

### GET /conversations

List all conversations with metadata.

**Response:**
```json
[
  {
    "room_id": "!abc123:chat.example.com",
    "name": "John Doe",
    "platform": "telegram",
    "last_message": "Thanks for the update",
    "last_activity": 1711835400000,
    "is_direct": true
  }
]
```

Sorted by last activity (most recent first).

### POST /conversations/new

Create a new direct message conversation.

**Request:**
```json
{ "user_id": "@telegram_12345:chat.example.com" }
```

**Response:**
```json
{ "room_id": "!newroom:chat.example.com" }
```

### GET /conversations/{room_id}/messages

Fetch messages for a conversation with pagination.

**Query parameters:**
- `limit` (int, 1–200, default 50) — Number of messages
- `from` (string, optional) — Pagination token for older messages

**Response:**
```json
{
  "messages": [
    {
      "event_id": "$event123",
      "sender": "@telegram_12345:chat.example.com",
      "sender_name": "John Doe",
      "body": "Hello!",
      "timestamp": 1711835400000,
      "is_self": false
    }
  ],
  "end": "t1234-5678",
  "has_more": true
}
```

Use the `end` token as the `from` parameter to load older messages.

### GET /conversations/{room_id}/members

List members of a conversation.

**Response:**
```json
[
  {
    "user_id": "@telegram_12345:chat.example.com",
    "display_name": "John Doe",
    "avatar_url": "mxc://..."
  }
]
```

### POST /conversations/{room_id}/send

Send a message to a conversation. The message is delivered through the appropriate platform bridge.

**Request:**
```json
{ "body": "Got it, I'll send the rate confirmation." }
```

**Response:**
```json
{ "event_id": "$newevent456" }
```

---

## Search

### GET /search?q={query}

Search message content across all conversations and platforms.

**Query parameters:**
- `q` (string, required) — Search term (case-insensitive)

**Response:**
```json
{
  "query": "rate confirmation",
  "total": 5,
  "groups": [
    {
      "room_id": "!abc123:chat.example.com",
      "room_name": "John Doe",
      "platform": "telegram",
      "results": [
        {
          "event_id": "$event789",
          "sender_name": "John Doe",
          "body": "Can you send the rate confirmation?",
          "timestamp": 1711835400000,
          "platform": "telegram"
        }
      ]
    }
  ]
}
```

Results are grouped by conversation.

---

## Alerts

### GET /alerts

Scan recent messages for keyword matches.

**Query parameters:**
- `hours` (int, default 24) — How far back to scan

**Response:**
```json
{
  "total": 3,
  "hours_scanned": 24,
  "severity_counts": { "high": 1, "medium": 1, "low": 1 },
  "alerts": [
    {
      "room_id": "!abc:chat.example.com",
      "room_name": "Customer X",
      "platform": "whatsapp",
      "keyword": "urgent",
      "severity": "high",
      "body": "This is urgent, the shipment is late",
      "sender_name": "Customer X",
      "timestamp": 1711835400000
    }
  ],
  "keywords_used": ["complaint", "urgent", "late", ...],
  "keywords_detail": [
    { "keyword": "urgent", "severity": "high" },
    { "keyword": "complaint", "severity": "medium" }
  ]
}
```

### POST /alerts

Manage custom alert keywords.

**Add keyword:**
```json
{ "action": "add", "keyword": "detention", "severity": "medium" }
```

**Remove keyword:**
```json
{ "action": "remove", "keyword": "detention" }
```

**Change severity:**
```json
{ "action": "set-severity", "keyword": "detention", "severity": "high" }
```

---

## Tags

### GET /tags

List all tags.

**Response:**
```json
[
  { "id": 1, "name": "LTL", "color": "#f5d40c" },
  { "id": 2, "name": "Refrigerated", "color": "#3b82f6" }
]
```

### POST /tags

Create a tag.

```json
{ "name": "Flatbed", "color": "#ef4444" }
```

### DELETE /tags?id={id}

Delete a tag and all its contact associations.

### GET /contacts/tags?contact={user_id}

Get tags assigned to a contact.

### POST /contacts/tags

Assign a tag to a contact.

```json
{ "contact_user_id": "@telegram_12345:chat.example.com", "tag_id": 1 }
```

### DELETE /contacts/tags?contact={user_id}&tag={tag_id}

Remove a tag from a contact.

### GET /contacts/tags/bulk

Get all contact→tag mappings in one call. Useful for rendering tag badges on the contact list without N+1 queries.

**Response:**
```json
{
  "@telegram_12345:chat.example.com": [
    { "id": 1, "name": "LTL", "color": "#f5d40c" }
  ]
}
```

---

## AI Features

### POST /ai/summarize

AI-powered conversation summary.

**Request:**
```json
{ "room_id": "!abc123:chat.example.com" }
```

**Response:**
```json
{
  "summary": "John requested a rate for a 40ft refrigerated container from Miami to Atlanta. Discussed pricing and confirmed pickup for Thursday.",
  "topics": ["refrigerated", "rate quote", "Miami-Atlanta"],
  "sentiment": "positive"
}
```

Requires `ANTHROPIC_API_KEY` or `OPENAI_API_KEY`.

### POST /ai/escalation-scan

Scan the 10 most recent conversations for issues requiring manager attention.

**Response:**
```json
{
  "total": 2,
  "rooms_scanned": 10,
  "escalations": [
    {
      "room_id": "!abc:chat.example.com",
      "room_name": "Customer X",
      "platform": "whatsapp",
      "needs_attention": true,
      "reason": "Customer is frustrated about delayed shipment and threatening to switch carriers",
      "severity": "high"
    }
  ]
}
```

### POST /ai/auto-tag

Analyze a conversation and suggest relevant tags.

**Request:**
```json
{ "room_id": "!abc123:chat.example.com" }
```

**Response:**
```json
{
  "existing_tags": ["LTL", "Refrigerated"],
  "new_tags": ["Miami-Atlanta Lane", "Time-Critical"]
}
```

---

## Statistics

### GET /stats

Dashboard statistics.

**Response:**
```json
{
  "total_conversations": 47,
  "active_today": 12,
  "platforms_connected": 4,
  "platform_breakdown": [
    { "platform": "telegram", "count": 20, "percentage": 42 },
    { "platform": "whatsapp", "count": 15, "percentage": 31 }
  ],
  "most_active": [
    { "name": "John Doe", "platform": "telegram", "room_id": "!abc:..." }
  ]
}
```

---

## Database (Debug)

### GET /schema

Returns the raw SQLite schema. Admin/debug use only.

**Response:**
```json
[
  { "name": "tags", "type": "table", "sql": "CREATE TABLE tags (...)" }
]
```
