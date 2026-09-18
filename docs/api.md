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

---

## Messaging a person

### POST /messages/send

Send a text message to **one person**, identified by their bridge puppet id, in the direct-message room we already share with them. A room is created only when none exists — unlike `POST /conversations/new`, which creates a fresh room on every call.

**Request:**
```json
{ "user_id": "@whatsapp_15551234567:chat.kayushkin.com", "body": "running 10 min late" }
```

**Response:**
```json
{ "room_id": "!abc:chat.kayushkin.com", "event_id": "$xyz", "created_room": false }
```

**Errors:** `400` missing `user_id` or `body`; `502` Synapse refused (message in `error`); `503` Matrix client not configured.

The `user_id` comes from `GET /contacts/unified` (`identities[].user_id`). `~/bin/multichat-send` wraps this route for the shell and the scheduler, resolving a display name + platform to that id and refusing anything ambiguous.

---

## Inbound rules

What happens when a person messages us. The server long-polls Synapse `/sync` (when `NATS_URL` is set), publishes every message a person sent as `messaging.inbound` on the NATS JetStream stream `MESSAGING` (payload `messages.MessagingInbound` in `github.com/kayushkin/bus/messages`), and applies these rules to that stream as durable consumer `multichat-inbound-dispatcher`. A match starts an **autonomous** session on an llm-bridge-server instance by publishing `bridge.inbound` (`messages.BridgeInbound`), the same path the scheduler's agent jobs use. Our own sends and our own platform puppets are never published.

With no rules, nothing happens — there is no default.

### GET /inbound-rules

All rules, enabled or not.

### POST /inbound-rules

```json
{
  "name": "Alex on WhatsApp mentioning the invoice",
  "platform": "whatsapp",
  "sender_user_id": "@whatsapp_15551234567:chat.kayushkin.com",
  "body_pattern": "(?i)invoice",
  "direct_messages_only": true,
  "instance_id": "inst-cc-local",
  "agent_id": "",
  "prompt_template": "{{.SenderDisplayName}} wrote on {{.Platform}}: {{.Body}}\n\nDraft a reply and save it to noteboard; do not send anything."
}
```

Every filter is optional and an empty one matches anything: `platform` (one of `GET /inbound-platforms`), `sender_user_id` (exact puppet id), `body_pattern` (RE2 against the body), `direct_messages_only` (default **true**). `instance_id` is required and is **checked with llm-bridge-server on write** — `400` when it knows no such instance, `502` when it cannot answer, nothing stored either way. `prompt_template` is required, a Go `text/template` over `Body`, `SenderUserID`, `SenderDisplayName`, `Platform`, `RoomID`, `RoomName`, `EventID`, `MessageType`, `IsDirectMessage`, `Timestamp`; a template that does not parse, or that names a field outside that list, is `400` — it is rendered against an empty message on write, so it cannot fail for the first time when a person writes in. Unknown JSON fields are `400`. Responds `201` with the stored rule; new rules are enabled.

### GET /inbound-rules/{id}, PATCH /inbound-rules/{id}, DELETE /inbound-rules/{id}

PATCH merges the fields sent onto the stored row and re-validates the whole rule (`{"enabled": false}` is the off switch). DELETE answers `204`.

### GET /inbound-dispatches?limit=100

What the rules did, newest first: `rule_id`, `event_id`, `room_id`, `sender_user_id`, `bus_session_id` (the id llm-bridge-adapter maps to a bridge session), and `error` when the session could not be started (instance disabled, bus down). One row per rule per message — the `(rule_id, event_id)` pair is unique, which is how a JetStream redelivery is absorbed.

### GET /inbound-platforms

The platform vocabulary a rule may name: `["whatsapp","telegram","signal","meta"]`.

### GET /inbound-prompt-fields

The fields a `prompt_template` may reference, read off the struct the dispatcher renders with: `[{"name":"Body","type":"string"}, …]`. An editor asks this instead of carrying the list.

### TypeScript types

`./generate-ts.sh` renders `internal/db/inbound.go` (`InboundRule`, `InboundDispatch`, `InboundPromptField`) to `ts/inbound.ts`, published source-only as `@kayushkin/multichat-types` (`file:../multichat/ts`). bridge-ui's Inbound rules page imports them; multichat's own frontend does not.

⚠️ A session started this way runs with nobody at the keyboard. The `multichat` MCP tool's `send_message` is gated `ask` at global scope in permission-store, so such a session **cannot reply to the person** — by design. It can read, draft, and write to noteboard or kanban; a human sends.

