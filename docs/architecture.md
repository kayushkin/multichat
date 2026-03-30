# Architecture

## Overview

MultiChat is a hub-and-spoke messaging aggregator. A Matrix homeserver (Synapse) acts as the central message store and relay, with protocol-specific bridges connecting each messaging platform. A Go API server provides the business logic layer, and a React SPA serves as the user-facing dashboard.

```
┌─────────────────────────────────────────────────────┐
│                    Users (Browser)                   │
│              React SPA — Dashboard, Chat,            │
│              Search, Alerts, Analytics               │
└──────────────────────┬──────────────────────────────┘
                       │ HTTPS
┌──────────────────────▼──────────────────────────────┐
│                  Nginx (TLS termination)             │
│         Rate limiting, IP allowlisting              │
└──────────────────────┬──────────────────────────────┘
                       │
┌──────────────────────▼──────────────────────────────┐
│               Go API Server (:8402)                  │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐            │
│  │ Auth     │ │ AI/LLM   │ │ Tags &   │            │
│  │ Middleware│ │ Analysis │ │ Contacts │            │
│  └──────────┘ └──────────┘ └──────────┘            │
│  ┌──────────────────────────────────────┐           │
│  │        Matrix Client Library          │           │
│  └──────────────────┬───────────────────┘           │
│                     │                                │
│  ┌──────────────────▼───────────────────┐           │
│  │         SQLite (local state)          │           │
│  │  Tags, alerts, local contacts, config │           │
│  └──────────────────────────────────────┘           │
└──────────────────────┬──────────────────────────────┘
                       │ Matrix Client-Server API
┌──────────────────────▼──────────────────────────────┐
│                  Synapse Homeserver                   │
│          (Matrix protocol — message store)            │
│                                                      │
│  ┌─────────┐ ┌─────────┐ ┌─────────┐ ┌─────────┐  │
│  │Telegram │ │WhatsApp │ │ Signal  │ │  Meta   │  │
│  │ Bridge  │ │ Bridge  │ │ Bridge  │ │ Bridge  │  │
│  │(mautrix)│ │(mautrix)│ │(mautrix)│ │(mautrix)│  │
│  └────┬────┘ └────┬────┘ └────┬────┘ └────┬────┘  │
└───────┼───────────┼───────────┼───────────┼─────────┘
        │           │           │           │
   Telegram      WhatsApp    Signal     Facebook/
   Bot API       Web API     Protocol   Instagram
```

## Components

### Go API Server

The central application server. Stateless (except SQLite), horizontally scalable behind a load balancer.

**Responsibilities:**
- Authentication and session management
- Proxying Matrix API calls with business logic
- Contact identity unification across platforms
- AI-powered conversation analysis (summarization, escalation detection, auto-tagging)
- Keyword-based alert scanning
- Cross-platform search aggregation
- Tag and contact management

**Key packages:**
- `cmd/server/` — Entry point, configuration, self-identity mapping
- `internal/api/` — HTTP router, all REST endpoints, AI integration
- `internal/matrix/` — Matrix Client-Server API client
- `internal/db/` — SQLite schema and initialization

### Synapse (Matrix Homeserver)

The persistent message store and real-time relay. All messages from all platforms flow through Synapse as Matrix events. This provides:

- **Unified message format** — every platform's messages become Matrix events
- **Message persistence** — full history with pagination
- **Room-based conversations** — each chat maps to a Matrix room
- **E2E encryption support** — via Matrix protocol (Megolm)
- **Federation** (optional) — can federate with other Matrix servers

### Protocol Bridges (mautrix)

Each messaging platform connects via a [mautrix](https://github.com/mautrix) bridge:

| Bridge | Protocol | Auth Method |
|--------|----------|-------------|
| mautrix-telegram | Telegram Bot API / MTProto | Bot token or user session |
| mautrix-whatsapp | WhatsApp Web multidevice | QR code linking |
| mautrix-signal | Signal protocol (signald) | Phone number + PIN |
| mautrix-meta | Meta Messenger + Instagram | Facebook login cookies |
| mautrix-imessage | iMessage (macOS only) | Apple ID on macOS host |

Bridges run as separate processes, each maintaining its own SQLite database for platform-specific state (puppet mappings, portal rooms, login sessions).

### React Frontend

Single-page application built with Vite + React + TypeScript + Tailwind CSS.

**Pages:**
- **Dashboard** — Platform connection status, conversation stats, AI scan trigger
- **Chat** — Conversation list with platform filters, real-time messaging, message history with pagination
- **Search** — Cross-platform full-text search grouped by conversation
- **Alerts** — Keyword-based alert monitoring with severity levels
- **Database** — Raw SQLite schema viewer (admin/debug)

### SQLite (Application State)

The Go server maintains a local SQLite database for application-specific state that doesn't belong in Matrix:

- `tags` — User-defined labels (e.g., "LTL", "Refrigerated", "Urgent")
- `contact_tags` — Many-to-many contact ↔ tag assignments
- `local_contacts` — Manually-added contacts not from Matrix
- `alert_keywords` — Custom alert trigger words with severity

## Data Flow

### Inbound Message (e.g., WhatsApp → Dashboard)

1. Customer sends WhatsApp message
2. mautrix-whatsapp receives via WhatsApp Web protocol
3. Bridge creates Matrix event in the corresponding room
4. Synapse stores the event
5. Go server queries Synapse API for room messages
6. Frontend displays message in conversation view

### Outbound Message (Dashboard → Platform)

1. User composes message in MultiChat UI
2. Frontend POSTs to `/api/conversations/{room}/send`
3. Go server sends Matrix event to Synapse
4. Synapse routes to the appropriate bridge
5. Bridge sends via native platform protocol
6. Customer receives on their platform

### Contact Identity Resolution

MultiChat unifies contacts across platforms:

1. Matrix provides ghost users for each platform contact (e.g., `@telegram_12345:chat.example.com`)
2. Go server enriches with bridge database lookups (phone numbers from Telegram DB, platform type from Meta DB)
3. Phone number matching merges WhatsApp + Telegram contacts
4. Display name matching groups remaining contacts
5. Frontend shows unified contact cards with platform badges

## Future Architecture

### Planned: Real-Time WebSocket Layer

Replace polling with WebSocket push:
- Go server maintains WebSocket connections to frontend clients
- Synapse `/sync` long-polling feeds the event stream
- New messages push instantly to all connected dashboards

### Planned: PostgreSQL Migration

For multi-user production deployments:
- Synapse on PostgreSQL (supported natively)
- Application state migrated from SQLite to PostgreSQL
- Connection pooling via PgBouncer
- Enables concurrent write access from multiple API server instances

### Planned: Redis Cache Layer

- Cache contact lists, conversation metadata
- Pub/sub for real-time event distribution across API server instances
- Session storage for horizontal scaling

### Planned: Queue-Based AI Processing

- AI analysis jobs (summarization, escalation) move to a background queue
- Prevents API timeout on large conversation analysis
- Enables batch processing and scheduling
