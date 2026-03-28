# MultiChat Design Document

**Version**: 3.0
**Date**: 2026-03-28
**URL**: chat.kayushkin.com

## Overview

MultiChat is a unified communications platform for work. It connects to **real** company-owned Telegram and WhatsApp accounts (not bots) via Matrix bridges, giving management visibility into all conversations across platforms with AI-powered analysis.

Built for a C-level demo at a 3PL (logistics company).

## Architecture

```
┌─────────────┐      ┌─────────────────┐      ┌──────────────┐
│  Telegram    │◄────►│ mautrix-telegram│◄────►│              │
│  Account(s)  │      │  (TDLib)        │      │              │
└─────────────┘      └─────────────────┘      │              │
                                               │   Synapse    │
┌─────────────┐      ┌─────────────────┐      │   (Matrix)   │
│  WhatsApp    │◄────►│ mautrix-whatsapp│◄────►│              │
│  Account(s)  │      │  (whatsmeow)    │      │   Port 8008  │
└─────────────┘      └─────────────────┘      └──────┬───────┘
                                                      │
                                               Matrix C-S API
                                                      │
                                               ┌──────▼───────┐
                                               │  Go API       │
                                               │  Server       │
                                               │  Port 8402    │
                                               │               │
                                               │  - REST API   │
                                               │  - WebSocket  │
                                               │  - AI hooks   │
                                               │  - SQLite     │
                                               └──────┬───────┘
                                                      │
                                                REST + WS
                                                      │
                                               ┌──────▼───────┐
                                               │ React Frontend│
                                               │ (Vite + TS)  │
                                               │               │
                                               │ - Inbox       │
                                               │ - Dashboard   │
                                               │ - AI Analysis │
                                               └──────┬───────┘
                                                      │
                                               Claude/OpenAI
```

### Components

#### 1. Synapse (Matrix Homeserver)
The messaging backbone. All messages from all platforms flow through Matrix.

- Docker container on Linode
- PostgreSQL backend (shared or dedicated)
- Handles rooms, events, message storage
- Federation disabled (internal use only)
- Port 8008 (internal, not exposed)

#### 2. mautrix-telegram (Telegram Bridge)
Connects to **real Telegram user accounts** via TDLib — not the Bot API.

- Logs in as the actual company Telegram account
- Pulls full message history on first bridge
- Real-time bidirectional message sync
- Each company Telegram account = one bridge login
- Requires: Telegram API ID + API hash (from my.telegram.org)
- Docker container

#### 3. mautrix-whatsapp (WhatsApp Bridge)
Connects to **real WhatsApp accounts** via whatsmeow.

- QR code pairing (like WhatsApp Web)
- Syncs recent message history
- Real-time bidirectional message sync
- Each company WhatsApp number = one bridge login
- Docker container

#### 4. Go API Server (Port 8402)
The application layer between Matrix and the frontend.

- **Matrix client**: Talks to Synapse via Client-Server API
  - Syncs rooms/messages, listens for new events
  - Maps Matrix rooms → conversations, Matrix users → contacts
- **REST API**: Serves the frontend
- **WebSocket**: Real-time push to frontend
- **AI integration**: Claude/OpenAI for conversation analysis
- **SQLite**: App-specific data (users, settings, AI cache, contact identity mapping)
- **Auth**: Simple session-based (JWT or cookie)

#### 5. React Frontend (Vite + TypeScript + Tailwind)
The demo star — what C-levels see.

- Unified inbox (all platforms, one view)
- Conversation threads with real-time updates
- Management dashboard (volume, response times, platform breakdown)
- AI analysis panel (sentiment, summaries, topics, escalation flags)
- Contact management with cross-platform identity linking
- Dark theme, professional, embeddable in company portal

## Database Schema

The Go API server maintains its own SQLite database for app-level data.
Matrix/Synapse handles raw message storage; we sync what we need.

```sql
-- App users (management, agents who use the dashboard)
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'viewer',  -- 'admin', 'manager', 'viewer'
    matrix_user_id TEXT,                  -- @user:chat.kayushkin.com
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- People we communicate with across platforms
CREATE TABLE contacts (
    id INTEGER PRIMARY KEY,
    display_name TEXT NOT NULL,
    notes TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Links a contact to their identity on each platform
-- Same person on Telegram + WhatsApp = 2 rows, 1 contact
CREATE TABLE contact_identities (
    id INTEGER PRIMARY KEY,
    contact_id INTEGER NOT NULL REFERENCES contacts(id),
    platform TEXT NOT NULL,               -- 'telegram', 'whatsapp', 'matrix'
    platform_user_id TEXT NOT NULL,       -- telegram user id, phone number, @mxid
    display_name TEXT,                    -- platform-specific display name
    metadata TEXT,                        -- JSON: avatar_url, username, etc
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(platform, platform_user_id)
);

-- Conversation threads (mapped from Matrix rooms)
CREATE TABLE conversations (
    id INTEGER PRIMARY KEY,
    matrix_room_id TEXT NOT NULL UNIQUE,  -- !room:server
    platform TEXT NOT NULL,               -- 'telegram', 'whatsapp'
    title TEXT,
    status TEXT NOT NULL DEFAULT 'active', -- 'active', 'archived'
    last_message_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Which contacts are in which conversations
CREATE TABLE contacts_conversations (
    contact_id INTEGER NOT NULL REFERENCES contacts(id),
    conversation_id INTEGER NOT NULL REFERENCES conversations(id),
    joined_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (contact_id, conversation_id)
);

-- Which app users have access to which conversations
CREATE TABLE users_conversations (
    user_id INTEGER NOT NULL REFERENCES users(id),
    conversation_id INTEGER NOT NULL REFERENCES conversations(id),
    last_read_at DATETIME,
    pinned BOOLEAN DEFAULT FALSE,
    PRIMARY KEY (user_id, conversation_id)
);

-- Messages (synced from Matrix, cached locally for search + AI)
CREATE TABLE messages (
    id INTEGER PRIMARY KEY,
    conversation_id INTEGER NOT NULL REFERENCES conversations(id),
    matrix_event_id TEXT NOT NULL UNIQUE, -- $event:server
    sender_contact_id INTEGER REFERENCES contacts(id),
    sender_user_id INTEGER REFERENCES users(id), -- if sent by an app user
    content TEXT,
    content_type TEXT DEFAULT 'text',     -- 'text', 'image', 'file', 'location'
    platform_message_id TEXT,             -- original platform msg id
    metadata TEXT,                        -- JSON: attachments, reactions, etc
    created_at DATETIME NOT NULL,
    INDEX idx_messages_conversation (conversation_id, created_at)
);

-- Connected platform accounts and bridge status
CREATE TABLE platform_connections (
    id INTEGER PRIMARY KEY,
    platform TEXT NOT NULL,               -- 'telegram', 'whatsapp'
    account_name TEXT NOT NULL,           -- display name / phone number
    bridge_status TEXT DEFAULT 'disconnected', -- 'connected', 'disconnected', 'syncing', 'error'
    connection_data TEXT,                 -- JSON: bridge-specific config
    matrix_bridge_user TEXT,              -- @telegrambot:server or @whatsappbot:server
    connected_at DATETIME,
    last_sync_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- User login sessions
CREATE TABLE sessions (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id),
    token TEXT NOT NULL UNIQUE,
    expires_at DATETIME NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- AI analysis cache
CREATE TABLE ai_analyses (
    id INTEGER PRIMARY KEY,
    conversation_id INTEGER NOT NULL REFERENCES conversations(id),
    analysis_type TEXT NOT NULL,          -- 'summary', 'sentiment', 'topics', 'full'
    result TEXT NOT NULL,                 -- JSON
    model TEXT,                           -- which AI model was used
    message_range TEXT,                   -- JSON: {from_id, to_id} — what messages were analyzed
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

## API Reference

Base URL: `https://chat.kayushkin.com/api`

### Auth
```
POST /api/auth/login
  Body: { email, password }
  Response: { token, user: { id, name, email, role } }

POST /api/auth/logout
  Header: Authorization: Bearer <token>
  Response: 204
```

### Conversations
```
GET /api/conversations
  Query: ?platform=telegram|whatsapp&status=active|archived&page=1&limit=50
  Response: { conversations: [...], total: int }

GET /api/conversations/:id
  Response: { conversation, contacts: [...], messages_count: int }

GET /api/conversations/:id/messages
  Query: ?before=<msg_id>&limit=50
  Response: { messages: [...], has_more: bool }

POST /api/conversations/:id/messages
  Body: { content, content_type? }
  Response: { message }
```

### Contacts
```
GET /api/contacts
  Query: ?q=search&platform=telegram|whatsapp&page=1&limit=50
  Response: { contacts: [...], total: int }

GET /api/contacts/:id
  Response: { contact, identities: [...], conversations: [...] }

PUT /api/contacts/:id
  Body: { display_name?, notes? }
  Response: { contact }

POST /api/contacts/:id/link
  Body: { target_contact_id }
  Response: { contact }  -- merges identities under one contact
```

### AI Analysis
```
POST /api/ai/analyze
  Body: { conversation_id, type: "summary"|"sentiment"|"topics"|"all" }
  Response: {
    summary?: string,
    sentiment?: { score: float, label: string },
    topics?: string[],
    flags?: string[],    -- escalation-worthy items
    cached: bool
  }

POST /api/ai/ask
  Body: { conversation_id, question }
  Response: { answer: string }
```

### Dashboard
```
GET /api/dashboard/stats
  Query: ?period=day|week|month
  Response: {
    conversations_count: int,
    messages_count: int,
    active_contacts: int,
    platform_breakdown: { telegram: int, whatsapp: int },
    avg_response_time_seconds: int,
    top_contacts: [...]
  }
```

### Platforms
```
GET /api/platforms
  Response: { connections: [...] }

POST /api/platforms/telegram/connect
  Body: { phone_number, api_id, api_hash }
  Response: { connection, auth_step: "code"|"2fa"|"done" }

POST /api/platforms/telegram/verify
  Body: { connection_id, code }
  Response: { connection }

POST /api/platforms/whatsapp/connect
  Response: { connection, qr_code: string }  -- base64 QR

GET /api/platforms/:id/status
  Response: { connection }

DELETE /api/platforms/:id
  Response: 204
```

### WebSocket
```
WS /ws
  Auth: ?token=<session_token>
  
  Server → Client events:
    { type: "message", conversation_id, message }
    { type: "conversation_update", conversation }
    { type: "typing", conversation_id, contact_id }
    { type: "presence", contact_id, online: bool }
    { type: "bridge_status", platform, status }
```

## Deploy

All on kayushkin.com Linode (45.33.63.37).

### Docker Compose (Matrix + Bridges)

```yaml
# deploy/docker-compose.yaml
services:
  synapse:
    image: matrixdotorg/synapse:latest
    volumes:
      - synapse_data:/data
    environment:
      - SYNAPSE_SERVER_NAME=chat.kayushkin.com
      - SYNAPSE_REPORT_STATS=no
    ports:
      - "127.0.0.1:8008:8008"
    restart: always

  mautrix-telegram:
    image: dock.mau.dev/mautrix/telegram:latest
    volumes:
      - telegram_data:/data
    depends_on:
      - synapse
    restart: always

  mautrix-whatsapp:
    image: dock.mau.dev/mautrix/whatsapp:latest
    volumes:
      - whatsapp_data:/data
    depends_on:
      - synapse
    restart: always

volumes:
  synapse_data:
  telegram_data:
  whatsapp_data:
```

### Go API Server
- Binary: `~/bin/multichat` (or `~/repos/multichat/multichat`)
- Systemd: `multichat.service`
- Port: 8402
- SQLite DB: `~/data/multichat/multichat.db`
- Config: `~/data/multichat/config.yaml`

### Nginx
```
chat.kayushkin.com/         → static frontend files
chat.kayushkin.com/api/     → Go API server :8402
chat.kayushkin.com/ws       → Go API server :8402 (WebSocket upgrade)
```
SSL via Let's Encrypt (already configured).

### Frontend
- `npm run build` → `frontend/dist/`
- Deployed to `~/multichat-frontend/` on server
- Served by nginx as static files

## Phases

### Phase 1: Foundation
- [ ] Deploy Synapse in Docker
- [ ] Configure mautrix-telegram bridge
- [ ] Go API skeleton (HTTP server, SQLite, Matrix client)
- [ ] React scaffold with routing and auth

### Phase 2: Core Messaging
- [ ] Connect a test Telegram account via bridge
- [ ] Sync Matrix rooms → conversations in Go API
- [ ] Sync Matrix events → messages
- [ ] Conversation list + message thread in frontend
- [ ] Real-time updates via WebSocket

### Phase 3: AI & Dashboard
- [ ] AI analysis endpoints (Claude API)
- [ ] Conversation summary, sentiment, topic extraction
- [ ] Management dashboard with stats
- [ ] AI insights panel in conversation view

### Phase 4: WhatsApp + Identity
- [ ] Configure mautrix-whatsapp bridge
- [ ] QR code pairing flow in frontend
- [ ] Contact identity linking across platforms
- [ ] Cross-platform contact view

### Phase 5: Polish & Demo
- [ ] Professional dark theme styling
- [ ] Loading states, error handling, empty states
- [ ] Search across conversations
- [ ] Export/report generation
- [ ] Deploy to production, seed demo data
- [ ] Demo rehearsal

## Key Decisions

- **Matrix as backbone**: Proven protocol, mautrix bridges are mature and actively maintained. Handles the hard part (platform protocol bridging) so we focus on UX and AI.
- **Real accounts, not bots**: Company owns the Telegram accounts. Bot API can't access existing conversations. TDLib via mautrix-telegram connects as the actual user.
- **Go API as intermediary**: Frontend doesn't talk to Matrix directly. Go server translates Matrix concepts (rooms, events) into our domain model (conversations, messages, contacts). Also handles auth, AI, and caching.
- **SQLite over PostgreSQL for app DB**: Simple, single-file, no extra service. Synapse can use PostgreSQL if needed for scale, but our app data is lightweight.
- **Contact identity linking**: Same person across Telegram + WhatsApp = multiple `contact_identities` rows pointing to one `contacts` row. Inspired by Beeper's model.
- **Federation disabled**: This is an internal tool, not a public Matrix server.
