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

## Settings

### GET /settings

Every environment variable the server reads, as `llm-bridge/servicesettings` describes it: key, variable, kind, value type, the value in force and where it came from (`environment` or `default`), and a description of what changing it changes. A secret shows only whether it is set, never its value. Needs the bearer like every other route. The declarations are `internal/config/settings.go`; bridge-ui's `/service-settings` page draws them.

Nothing is editable, so `PUT /settings/{key}` is not mounted and answers 404.

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
      "msg_type": "m.text",
      "timestamp": 1711835400000,
      "is_me": false,
      "format": "org.matrix.custom.html",
      "formatted_body": "Hello! <img data-mx-emoticon src=\"mxc://chat.kayushkin.com/Q…\" alt=\":bearlaugh:\" title=\":bearlaugh:\" height=\"32\"/>",
      "media_url": "",
      "media_mimetype": "",
      "reply_to_event_id": "$earlier",
      "reactions": [{"key": "mxc://chat.kayushkin.com/adm…", "shortcode": ":this:", "count": 1, "sender_display_names": ["Jade"]}]
    }
  ],
  "end": "t1234-5678",
  "has_more": true
}
```

Use the `end` token as the `from` parameter to load older messages. `format`, `formatted_body` (the Matrix HTML, "" when absent), `media_url` (the `mxc://` URI of an image, file or video), `media_mimetype` (`info.mimetype`) and `reply_to_event_id` (`m.relates_to.m.in_reply_to.event_id`) are read from the event content. An edit is listed as its own event with `replaces_event_id` naming the message it edits; its `body`, `format`, `formatted_body` and media are the edited content (`m.new_content`), never the `* …` fallback, so a client folds it into the original. The conversation list's `last_message` previews the edited text the same way. On Discord a custom emoji is only in `formatted_body`, as `<img data-mx-emoticon src="mxc://…" alt=":name:">`; the plain `body` leaves it out. `reactions` is each message's current reactions from the reaction log, grouped by key — so only reactions made since the log began (`message_log_started_at`) — and `[]` when none.

### GET /media/{server_name}/{media_id}

The file behind `mxc://{server_name}/{media_id}` — a custom emoji, an image, an attachment — fetched from Synapse's authenticated media API (`/_matrix/client/v1/media/download/…`; Synapse 1.161 refuses the old unauthenticated one) with multichat's Matrix token, and streamed through. With `width` and `height` both given it is `/thumbnail/…?width&height&method=scale`. Synapse's status, `Content-Type`, `Content-Length`, cache headers (`Cache-Control`, `Expires`, `ETag`, `Last-Modified`) and its `Content-Disposition`, `Content-Security-Policy` and `X-Content-Type-Options` pass through unchanged; a 200 without a `Cache-Control` gets `private, max-age=86400`. A path that is not exactly two segments, or only one of `width` and `height`, is a 400.

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

### POST /conversations/{room_id}/reactions

`{"event_id": "$message", "key": "🔥"}` reacts to a message as our Matrix account (`m.reaction`, an `m.annotation`); the room's bridge relays it to the platform as our own account, so everyone in the room sees it. `key` is a unicode emoji, the `mxc://` URI of a custom emoji the bridge knows, or `discord-emoji:<id>` for any emoji in the custom emoji catalog (`GET /emoji`). For the last, multichat first teaches the Discord bridge the emoji if it has not seen it (see *Emoji*), then reacts with its `mxc://` URI. Answers `{"event_id": "<the reaction>", "key": "<the key sent>"}`; for a `discord-emoji:` key that is the `mxc://` URI, which is what the reaction log and every later read carry. A `discord-emoji:` id not in the catalog is a 400; a failure teaching the bridge is a 502. A missing field is a 400; Synapse refusing it (a second identical reaction is `M_DUPLICATE_ANNOTATION`) is a 502 carrying its answer. The reaction is logged at once, so the next read shows it.

### DELETE /conversations/{room_id}/reactions/{reaction_event_id}

Takes back a reaction our Matrix account posted, by redacting it. Answers `{"redaction_event_id": …}`. 404 when the reaction is not in the reaction log or not in that room, 409 when it was already taken back, 403 when someone else made it — including our own puppet, from the platform's own app, which multichat cannot redact.

Every reaction group (here, on conversation messages and in `/message-log`) carries `reacted_by_me` (our Matrix account or our puppet reacted with that key) and `my_reaction_event_id` (our Matrix account's reaction, the id this route takes back; "" otherwise).

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

### POST /conversations/tags

Put a tag on a room. Tagging a room twice is not an error. A `room_id` that is not a Matrix room id (`!…:server`) is a 400; a `tag_id` with no tag is a 404.

```json
{ "room_id": "!abc123:chat.example.com", "tag_id": 1 }
```

**Response:** the room's tags afterwards.

```json
{ "room_id": "!abc123:chat.example.com", "tags": [{ "id": 1, "name": "LTL", "color": "#f5d40c" }] }
```

### DELETE /conversations/tags?room={room_id}&tag={tag_id}

Take a tag off a room. Answers the room's tags afterwards, like `POST`.

### GET /conversations/tags/bulk

Every room's own tags, keyed by room id; a room with none is absent. Contact tags are separate (`GET /contacts/tags/bulk`). Deleting a tag with `DELETE /tags` takes it off every contact and every room.

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

### GET /message-log

Every message a bridge brought in since the log started, newest first, with deleted ones kept, plus any Discord history imported from before it. Query: `platform` (one of `/inbound-platforms`), `network_id` (on Discord the server id), `room_id`, `sender_user_id`, `q` (substring of the body, any case), `deleted=true` (deleted only), `include_edits=true` (list edits as rows too; by default they are only in `edits`), `before` (RFC 3339; the next page is `before=` the last row's `sent_at`), `limit` (1–1000, default 200). Answers `{"messages": [...], "edits": {"<event_id>": [...]}, "reactions": {"<key>": [...]}}`.

A message has `id` (the log's own row id — the stable key of a row), `event_id` ("" on an archive row), `source` (`matrix` for a row logged live, `discord-archive` for an imported one), `discord_message_id` (archive rows only), `room_id`, `room_name`, `platform`, `network_id`, `network_name`, `channel_id`, `channel_name`, `channel_external_url`, `sender_user_id`, `sender_display_name`, `message_type`, `body`, `format` (`org.matrix.custom.html` when `formatted_body` is Matrix HTML, `discord-markdown` on archive rows, whose `body` is raw Discord markdown with `<:name:id>` custom emoji, else ""), `formatted_body`, `media_url`, `media_mimetype`, `reply_to_event_id`, `content_json` (the event content, or on an archive row the whole Discord message object), `replaces_event_id` (set on an edit), `sent_at`, `logged_at`, and `redacted_at`, `redaction_event_id`, `redacted_by_user_id` once deleted. An edit's `body` and formatting come from its `m.new_content`.

A Discord sticker is a row with `message_type` `m.sticker`, `body` the sticker's name, and `media_url`/`media_mimetype` from the event's `url` and `info.mimetype`; a Lottie sticker has no `media_url` and mimetype `application/json`. `edits` holds the edits made to each listed message, oldest first. `reactions` holds the current reactions to each listed message that has any, as `[{key, shortcode, count, sender_display_names}]` grouped by key: keyed by `event_id` from the reaction log (a unicode emoji, or an `mxc://` URI with its `:name:` shortcode), or by `discord:<discord_message_id>` for an archive row, from the archived object's `reactions` (a custom emoji is `discord-emoji:<id>`; `sender_display_names` is empty because Discord gives only the count). A bad filter is a 400 naming it.

`cmd/multichat-import-discord-archive -db … -discord-db … -archive file.jsonl` adds a Discord history fetch (one raw Discord API message object per line) as archive rows: only messages sent before `message_log_started_at`, so nothing overlaps the live log, and a second run inserts nothing. The room, server and channel come from the bridge database's `portal` and `guild` rows for the message's `channel_id`; the sender is the bridge puppet `@discord_<author.id>:<homeserver>`, named `author.global_name`, else `author.username`. `channel_external_url` and `message_type` are "" on archive rows.

### GET /discord/status

What the Discord bridge is doing, read-only: it reads the mautrix-discord database at `DISCORD_DB` (opened `?mode=ro`), Synapse's `joined_rooms` for `@admin`, and the message log. It never calls Discord and never reads the Discord token — `logged_in` is a SQL `discord_token IS NOT NULL`.

```json
{
  "accounts": [{"matrix_user_id": "@admin:chat.kayushkin.com", "discord_user_id": "920000000000000006",
                "logged_in": true, "management_room_id": "!…:chat.kayushkin.com"}],
  "bridged_servers": [{
    "discord_server_id": "9300000000000000001", "name": "Riverside social group",
    "bridging_mode": 3, "bridging_mode_name": "everything", "matrix_space_id": "!…:chat.kayushkin.com",
    "channels": [{
      "discord_channel_id": "9300000000000000002", "name": "#general-chat", "channel_type": 0,
      "category_name": "Text Channels", "matrix_room_id": "!…:chat.kayushkin.com", "admin_joined": true,
      "last_bridged_message_at": "2026-09-28T…Z", "logged_message_count": 12, "deleted_message_count": 1,
      "last_logged_message_at": "2026-09-28T…Z"}]
  }],
  "unbridged_server_count": 132
}
```

A server is bridged when its `bridging_mode` is not 0 (mautrix-discord's `GuildBridgingMode`: 1 if-portal-exists, 2 create-on-message, 3 everything). Its `channels` are the message-carrying ones — text (`channel_type` 0) and announcement (5) — ordered by category, then name; categories, voice channels and forums are left out. `matrix_room_id` is empty for a channel the bridge has made no room for. `admin_joined` false means `@admin` is at most invited, so multichat can neither read nor log the channel until it joins. `last_bridged_message_at` is the newest `message.timestamp` the bridge recorded there, null when none. The three log fields count message-log rows with that `channel_id` on platform `discord`, edits excluded and archive rows included — the same rows `GET /message-log` lists for the channel. `unbridged_server_count` counts servers a logged-in account is in (`user_portal` type `guild`) whose mode is 0. `DISCORD_DB` unset is a 503 naming it, as is an unconfigured Matrix client; a Synapse failure is a 502.

## Emoji

The custom emoji of every Discord server the bridge bridges, and the operator's favourite emoji and emoji settings (`internal/api/emoji.go`, `internal/discordemoji`, `internal/db/emoji.go`). The records are rendered to TypeScript (`EmojiCatalog`, `DiscordCustomEmoji`, `EmojiSettings`).

### GET /emoji

Answers `{"discord_custom_emoji": [...], "discord_refreshes": [...], "favorite_keys": [...], "settings": {...}}`. Each custom emoji has `discord_emoji_id`, `name` (no colons), `animated`, `discord_server_id`, `discord_server_name`, `fetched_at`, `reaction_key` (`discord-emoji:<id>`) and `bridge_mxc`: the `mxc://` URI the bridge knows it by, read from the bridge's `discord_file` table, or "" when the bridge has not seen it yet. `discord_refreshes` says when each server's emoji were last fetched. `favorite_keys` are unicode emoji and `discord-emoji:<id>` keys in the operator's order. `settings` is `{skin_tone, quick_reaction_count, offer_other_servers_emoji}`, defaults `""`, `5`, `false` until first saved. Nothing here calls Discord. `DISCORD_DB` unset is a 503.

### POST /emoji/discord/refresh

Lists each bridged server's custom emoji from Discord (`GET /guilds/{id}/emojis`) with the bot token in auth-store credential `DISCORD_EMOJI_BOT_CREDENTIAL_ID`, and replaces that server's rows, so an emoji deleted on Discord leaves the catalog; an emoji Discord marks unavailable (a lost boost) is left out. Answers the catalog as `GET /emoji` does. A missing setting is a 503 naming it; auth-store or Discord refusing is a 502 carrying its answer (a 403 or 404 from Discord usually means the bot is not in that server). Scheduler job `multichat emoji refresh` runs it daily.

### PUT /emoji/favorites

`{"favorite_keys": ["🔥", "discord-emoji:123"]}` replaces the favourites, in that order. A key that is not a unicode emoji or a `discord-emoji:<id>` in the catalog, a repeated key, a missing `favorite_keys` or an unknown field is a 400 and changes nothing. Answers `{"favorite_keys": [...]}`.

### PUT /emoji/settings

`{"skin_tone": "🏽", "quick_reaction_count": 5, "offer_other_servers_emoji": false}`, every field required. `skin_tone` is "" or one of the five modifiers U+1F3FB–U+1F3FF; `quick_reaction_count` is how many favourites the quick-react bar under a message shows, 0–12; `offer_other_servers_emoji` offers a Discord room every bridged server's emoji, not only its own (Discord refuses another server's emoji without Nitro). Out of range is a 400. Answers the settings.

### Teaching the bridge an emoji

mautrix-discord relays a Matrix reaction with an `mxc://` key only when its `discord_file` table holds that URI with the emoji's id and name; it adds a row the first time it relays the emoji from Discord. For one it has not seen, multichat does the same: it fetches the image from Discord's CDN, uploads it to Synapse, and inserts the row under the address the bridge files it by (`https://cdn.discordapp.com/emojis/<id>.png`, or `.webp?animated=true` for an animated one). If the bridge added the same address in between, its row wins. This is multichat's only write into a bridge's database, so the service user needs write access to `DISCORD_DB`: `~/multichat/docker-compose.yaml` runs `mautrix-discord` with this user's `UID` and `GID`, and `deploy.sh` refuses to deploy when the file is not writable. The bridge cannot put a custom emoji inside a message: it escapes `<` in plain text and drops `<img>` from HTML, so custom emoji work in reactions only.

### GET /inbound-platforms

The platform vocabulary a rule may name: `["whatsapp","telegram","signal","meta"]`.

### GET /inbound-prompt-fields

The fields a `prompt_template` may reference, read off the struct the dispatcher renders with: `[{"name":"Body","type":"string"}, …]`. An editor asks this instead of carrying the list.

### TypeScript types

`./generate-ts.sh` renders `internal/db/inbound.go` (`InboundRule`, `InboundDispatch`, `InboundPromptField`) and `internal/db/message_log.go` (`LoggedMessage`, `MessageReactionGroup`, `MessageLogPage`, and the source and format constants) to `ts/db.ts`, and `internal/discordbridge/status.go` (`DiscordBridgeStatus` and its parts) to `ts/discord_status.ts`, published source-only as `@kayushkin/multichat-types` (`file:../multichat/ts`, entry `ts/index.ts`). bridge-ui's Inbound rules and Discord pages import them; multichat's own frontend does not.

⚠️ A session started this way runs with nobody at the keyboard. The `multichat` MCP tool's `send_message` is gated `ask` at global scope in permission-store, so such a session **cannot reply to the person** — by design. It can read, draft, and write to noteboard or kanban; a human sends.

