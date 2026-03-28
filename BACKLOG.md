# BACKLOG.md — MultiChat

Pick the top unclaimed item, do it, push. Mark `[x]` when done, `[~]` in progress.

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
