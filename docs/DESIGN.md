# MultiChat Design Document

**Version**: 2.0
**Date**: 2026-03-28
**URL**: chat.kayushkin.com

## Overview

MultiChat is a unified communications platform for work — a custom React frontend on top of Chatwoot, with AI-powered conversation analysis. It bridges Telegram + WhatsApp into a single interface with management visibility and analytics.

## Architecture

```
┌──────────────────────┐     ┌──────────────────────┐
│   Custom React UI    │     │   Chatwoot (Docker)   │
│   (chat.kayushkin.com)│────▶│   Rails + Sidekiq     │
│                      │     │   Port 3100            │
│  - Unified inbox     │     │                        │
│  - Management dash   │     │  ┌─────────┐ ┌──────┐ │
│  - AI analysis panel │     │  │Postgres │ │Redis │ │
│                      │     │  └─────────┘ └──────┘ │
└──────────┬───────────┘     └──────────┬─────────────┘
           │                            │
           │ Chatwoot REST API          │ Webhooks/Bridges
           │ /api/v1/*                  │
           ▼                            ▼
┌──────────────────────┐     ┌──────────────────────┐
│  Go API Server       │     │  Telegram Bot API     │
│  Port 8402           │     │  WhatsApp (whatsmeow) │
│                      │     │  (via Chatwoot)       │
│  - AI analysis hooks │     └──────────────────────┘
│  - Custom endpoints  │
│  - Claude/OpenAI API │
└──────────────────────┘
```

### Components

**Chatwoot** (backend, Docker) — handles all platform integration:
- Telegram Bot API webhooks
- WhatsApp via Chatwoot's built-in integration
- Message storage in PostgreSQL
- Contact management with cross-platform identity
- Conversation threading
- REST API for everything

**Custom React Frontend** — the demo layer:
- Unified inbox view (all platforms in one sidebar)
- Conversation thread view with real-time messages
- Management dashboard (volume, response times, platform breakdown)
- AI analysis panel (conversation summaries, sentiment, topic extraction)
- Clean, brandable UI suitable for embedding in a company portal

**Go API Server** (port 8402) — AI + custom logic:
- Proxies some Chatwoot API calls with enrichment
- AI endpoints: POST /api/v2/analyze (conversation summary, sentiment, topics)
- Management stats aggregation
- Claude/OpenAI API integration

### Key Decisions

- **Chatwoot as backend**: Saves weeks of platform integration work. Full API, webhooks, contact dedup, message storage all built-in.
- **Custom frontend**: C-level demo needs to look polished, not like an open source admin panel. Also needs to be embeddable.
- **Separate Go server for AI**: Keeps AI logic isolated. Frontend calls Go server, Go server calls Chatwoot API + Claude API.

## Chatwoot API Reference

Base URL: `https://chat.kayushkin.com/api/v1`
Auth: `api_access_token` header (user or bot token)

Key endpoints we'll use:
- `GET /conversations` — list conversations with filters
- `GET /conversations/{id}/messages` — message history
- `GET /contacts` — contact list with search
- `GET /contacts/{id}/conversations` — all convos for a contact
- `POST /conversations/{id}/messages` — send a message
- `GET /reports/agents` — agent performance stats
- `GET /reports/summary` — conversation volume stats

## Custom Frontend Pages

### 1. Inbox View
- Left sidebar: conversation list grouped by platform (Telegram/WhatsApp)
- Center: message thread with real-time updates via Chatwoot ActionCable
- Right panel: contact info, conversation metadata, AI insights
- Unread counts per conversation
- Search across all conversations

### 2. Management Dashboard
- Conversation volume over time (by platform)
- Active conversations count
- Average response time
- Contact growth
- Platform connection status

### 3. AI Analysis Panel
- Per-conversation: AI summary, sentiment score, key topics
- Bulk analysis: trends across conversations, flagged items
- "Ask about this conversation" — chat with AI about a specific thread

## Go API Server Endpoints

```
POST /api/v2/analyze
  Body: { conversation_id: int, type: "summary"|"sentiment"|"topics"|"all" }
  Response: { summary: string, sentiment: float, topics: string[], flags: string[] }

GET /api/v2/dashboard/stats
  Response: { conversations_today: int, messages_today: int, avg_response_time: int, ... }

POST /api/v2/ai/ask
  Body: { conversation_id: int, question: string }
  Response: { answer: string }
```

## Deploy

- **Chatwoot**: Docker Compose on kayushkin.com Linode, port 3100
- **Custom frontend**: Vite build → static files served by Go server or nginx
- **Go API server**: port 8402, systemd service
- **Nginx**: chat.kayushkin.com → Chatwoot (/) + Go server (/api/v2/)
- **SSL**: Let's Encrypt (already configured)
