# BACKLOG.md — MultiChat

Pick the top unclaimed item, do it, push. Mark `[x]` when done, `[~]` in progress.

---

## Up Next — Demo Polish

- [x] **Cross-conversation search** — Search bar that searches message content across ALL platforms. "Find every mention of [customer name] or [rate]"
- [x] **Keyword/phrase alerts** — Flag conversations containing configurable keywords: "complaint", "late", "damaged", "competitor", dollar amounts. Management gets notified.
- [x] **AI escalation detection** — Auto-scan conversations, flag ones needing attention (angry customer, missed deadline, risky promise). "⚠️ 3 conversations need review"
- [ ] **Conversation export** — Export any conversation as PDF with timestamps. Compliance/audit trail.
- [ ] **Read-only mode** — Management can view everything but can't accidentally send messages as the broker.
- [ ] **Smart replies** — AI suggests 3 reply options based on conversation context
- [ ] **Daily digest** — "Here's what happened across all platforms today" summary
- [ ] **Response time analytics** — "Avg response time: 12min on Telegram, 45min on WhatsApp"
- [ ] **Volume heatmap** — When are messages busiest? Time-of-day grid.
- [ ] **Real-time WebSocket updates** — New messages push to frontend instantly

---

## Phase 1 — Foundation

- [ ] **Go project init** — `go mod init github.com/kayushkin/multichat`, basic main.go with HTTP server
- [ ] **React frontend init** — Vite + React + TypeScript in `frontend/`, basic chat layout
- [ ] **SQLite schema** — users, conversations, messages, platforms tables
- [ ] **WebSocket server** — Go gorilla/websocket or nhooyr.io/websocket for real-time message push
- [ ] **Auth system** — simple session-based auth (cookie), user registration/login
- [ ] **Static file serving** — Go serves built frontend from `frontend/dist/`

## Phase 2 — Platform Integrations

- [ ] **Telegram Bot API** — receive messages via webhook/polling, send messages, store in SQLite
- [ ] **WhatsApp via whatsmeow** — connect via QR code, receive/send messages, store in SQLite
- [ ] **Platform abstraction** — common interface for send/receive across platforms
- [ ] **Conversation routing** — map platform conversations to unified threads

## Phase 3 — UI

- [ ] **Chat UI** — message list, input box, conversation sidebar
- [ ] **Platform switcher** — tabs or dropdown to switch between Telegram/WhatsApp
- [ ] **User management** — login/register screens, user settings
- [ ] **Message history** — scroll back through conversations, search
- [ ] **Real-time updates** — WebSocket integration for live message delivery

## Phase 4 — Deploy

- [ ] **Dockerfile** — multi-stage build (Go + Node)
- [ ] **Systemd service** — `multichat.service` for bare metal deploy
- [ ] **Nginx config** — reverse proxy for multichat.kayushkin.com
- [ ] **Let's Encrypt** — SSL cert for subdomain
- [ ] **deploy.sh** — build locally, push, SSH pull + restart
- [ ] **GitHub repo** — push to kayushkin/multichat

## Done

- [x] **Dashboard stats** — real conversation counts, platform breakdown, recent activity
- [x] **AI conversation summary** — Claude/OpenAI-powered summaries with topics + sentiment
- [x] **Contact identity unification** — cross-platform grouping by display name
- [x] **Cross-conversation search** — search message bodies across all platforms
- [x] **Keyword/phrase alerts** — auto-scan for red-flag words with severity levels
- [x] **AI escalation detection** — scan active conversations for manager attention
- [x] **SVG platform icons** — proper Telegram/WhatsApp/Signal/Messenger/Instagram icons
- [x] **Top nav bar** — replaced sidebar with compact top navigation
- [x] **Platform filter pills** — filter conversation list by platform
- [x] **Members dropdown** — clickable member count shows room members
- [x] **Merge WSL + Linode branches** — unified codebase
