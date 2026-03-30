# Deployment Guide

## Overview

MultiChat is designed for on-premise or private cloud deployment. Because it handles real messaging credentials and business communications, it should **never** be deployed on shared infrastructure or public PaaS without additional hardening.

## Deployment Models

### Single-Server (Small Team, < 20 users)

Everything runs on one machine. Suitable for a single brokerage office.

```
┌──────────────────────────────────┐
│          Single Server           │
│  (4 CPU, 8GB RAM, 100GB SSD)    │
│                                  │
│  nginx (TLS) → Go API (:8402)   │
│  Synapse (:8008)                 │
│  Bridges (telegram, whatsapp,    │
│           signal, meta, imessage)│
│  SQLite DBs                      │
│  Redis (optional cache)          │
└──────────────────────────────────┘
```

**Minimum requirements:**
- 4 vCPUs, 8 GB RAM, 80 GB SSD
- Ubuntu 22.04+ or Debian 12+
- Public IP with DNS record
- TLS certificate (Let's Encrypt)

### Docker Compose (Recommended for Production)

```yaml
version: '3.8'

services:
  nginx:
    image: nginx:alpine
    ports:
      - "443:443"
      - "80:80"
    volumes:
      - ./nginx/conf.d:/etc/nginx/conf.d
      - ./certs:/etc/letsencrypt
    depends_on:
      - multichat-api
    restart: unless-stopped

  multichat-api:
    build: .
    environment:
      - SYNAPSE_URL=http://synapse:8008
      - MATRIX_TOKEN=${MATRIX_TOKEN}
      - MULTICHAT_TOKEN=${MULTICHAT_TOKEN}
      - ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY}
      - TELEGRAM_DB=/bridges/telegram/mautrix-telegram.db
      - META_DB=/bridges/meta/mautrix-meta.db
    volumes:
      - ./data/multichat:/data
      - bridge_telegram:/bridges/telegram:ro
      - bridge_meta:/bridges/meta:ro
    depends_on:
      - synapse
    restart: unless-stopped

  synapse:
    image: matrixdotorg/synapse:latest
    volumes:
      - synapse_data:/data
      - ./synapse/homeserver.yaml:/data/homeserver.yaml
    environment:
      - SYNAPSE_SERVER_NAME=${DOMAIN}
    restart: unless-stopped

  mautrix-telegram:
    image: dock.mau.dev/mautrix/telegram:latest
    volumes:
      - bridge_telegram:/data
      - ./bridges/telegram/config.yaml:/data/config.yaml
    depends_on:
      - synapse
    restart: unless-stopped

  mautrix-whatsapp:
    image: dock.mau.dev/mautrix/whatsapp:latest
    volumes:
      - bridge_whatsapp:/data
      - ./bridges/whatsapp/config.yaml:/data/config.yaml
    depends_on:
      - synapse
    restart: unless-stopped

  mautrix-signal:
    image: dock.mau.dev/mautrix/signal:latest
    volumes:
      - bridge_signal:/data
      - ./bridges/signal/config.yaml:/data/config.yaml
    depends_on:
      - synapse
    restart: unless-stopped

  mautrix-meta:
    image: dock.mau.dev/mautrix/meta:latest
    volumes:
      - bridge_meta:/data
      - ./bridges/meta/config.yaml:/data/config.yaml
    depends_on:
      - synapse
    restart: unless-stopped

volumes:
  synapse_data:
  bridge_telegram:
  bridge_whatsapp:
  bridge_signal:
  bridge_meta:
```

### Multi-Server (Enterprise, 20+ users)

Separate concerns across machines for reliability and performance.

```
┌─────────────┐   ┌─────────────┐   ┌─────────────┐
│  Load       │   │  App Server  │   │  App Server  │
│  Balancer   │──▶│  Go API #1   │   │  Go API #2   │
│  (nginx)    │   │              │   │              │
└─────────────┘   └──────┬───────┘   └──────┬───────┘
                         │                   │
                    ┌────▼───────────────────▼────┐
                    │   PostgreSQL (Synapse DB)    │
                    │   + Redis (cache/sessions)   │
                    └─────────────────────────────┘
                    ┌─────────────────────────────┐
                    │     Bridge Server            │
                    │  telegram, whatsapp, signal, │
                    │  meta, imessage bridges      │
                    └─────────────────────────────┘
```

**Requirements:**
- Load balancer: 1 vCPU, 1 GB RAM
- App servers (x2): 2 vCPU, 4 GB RAM each
- Database: 4 vCPU, 16 GB RAM, fast SSD (PostgreSQL)
- Bridge server: 4 vCPU, 8 GB RAM
- Redis: 1 vCPU, 2 GB RAM

## Build & Install

### Prerequisites

- Go 1.24+
- Node.js 20+
- SQLite3
- Docker & Docker Compose (for bridge services)

### Build from Source

```bash
# Clone
git clone git@github.com:kayushkin/multichat.git
cd multichat

# Build frontend
cd frontend
npm install
npm run build
cd ..

# Build Go binary
CGO_ENABLED=1 go build -o multichat-server ./cmd/server/

# Deploy frontend assets
mkdir -p /opt/multichat/frontend
cp -r frontend/dist/* /opt/multichat/frontend/

# Deploy binary
cp multichat-server /opt/multichat/
```

### Dockerfile (Multi-Stage)

```dockerfile
# Stage 1: Build frontend
FROM node:20-alpine AS frontend
WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# Stage 2: Build Go binary
FROM golang:1.24-alpine AS backend
RUN apk add --no-cache gcc musl-dev sqlite-dev
WORKDIR /app
COPY go.* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o multichat-server ./cmd/server/

# Stage 3: Runtime
FROM alpine:3.19
RUN apk add --no-cache sqlite-libs ca-certificates
WORKDIR /app
COPY --from=backend /app/multichat-server .
COPY --from=frontend /app/frontend/dist ./frontend/
EXPOSE 8402
CMD ["./multichat-server"]
```

## Systemd Service

```ini
[Unit]
Description=MultiChat Server
After=network.target synapse.service
Wants=synapse.service

[Service]
Type=simple
User=multichat
Group=multichat
WorkingDirectory=/opt/multichat
ExecStart=/opt/multichat/multichat-server
Restart=always
RestartSec=5

# Environment
EnvironmentFile=/opt/multichat/.env

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/opt/multichat/data
PrivateTmp=true
ProtectKernelTunables=true
ProtectControlGroups=true

[Install]
WantedBy=multi-user.target
```

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `SYNAPSE_URL` | Yes | Matrix homeserver URL (e.g., `http://localhost:8008`) |
| `MATRIX_TOKEN` | Yes | Admin access token for Synapse |
| `MULTICHAT_TOKEN` | Yes | Shared secret for API authentication |
| `PORT` | No | HTTP listen port (default: `8402`) |
| `ANTHROPIC_API_KEY` | No | For AI features (summarization, escalation) |
| `OPENAI_API_KEY` | No | Alternative AI provider |
| `TELEGRAM_DB` | No | Path to mautrix-telegram SQLite DB (phone lookups) |
| `META_DB` | No | Path to mautrix-meta SQLite DB (FB vs IG) |

## Nginx Configuration

```nginx
server {
    listen 443 ssl http2;
    server_name chat.example.com;

    ssl_certificate /etc/letsencrypt/live/chat.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/chat.example.com/privkey.pem;

    # Security headers
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-Frame-Options "DENY" always;
    add_header X-XSS-Protection "1; mode=block" always;
    add_header Referrer-Policy "strict-origin-when-cross-origin" always;
    add_header Content-Security-Policy "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:;" always;

    # Rate limiting
    limit_req_zone $binary_remote_addr zone=api:10m rate=30r/s;

    # API
    location /api/ {
        limit_req zone=api burst=50 nodelay;
        proxy_pass http://127.0.0.1:8402;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # Frontend (static files)
    location / {
        root /opt/multichat/frontend;
        try_files $uri $uri/ /index.html;

        # Cache static assets
        location ~* \.(js|css|png|jpg|svg|ico|woff2?)$ {
            expires 30d;
            add_header Cache-Control "public, immutable";
        }
    }
}

# Redirect HTTP → HTTPS
server {
    listen 80;
    server_name chat.example.com;
    return 301 https://$host$request_uri;
}
```

## Backup Strategy

### What to Back Up

| Component | Location | Frequency | Method |
|-----------|----------|-----------|--------|
| Synapse DB | `/data/homeserver.db` or PostgreSQL | Daily | `sqlite3 .backup` or `pg_dump` |
| Application DB | `/opt/multichat/data/multichat.db` | Daily | `sqlite3 .backup` |
| Bridge databases | `/data/*.db` per bridge | Daily | `sqlite3 .backup` |
| Config files | `*.yaml`, `.env` | On change | Git or rsync |
| TLS certificates | `/etc/letsencrypt/` | Weekly | rsync |

### Automated Backup Script

```bash
#!/bin/bash
BACKUP_DIR=/backups/multichat/$(date +%Y-%m-%d)
mkdir -p "$BACKUP_DIR"

# Stop writes briefly for consistent backup
sqlite3 /opt/multichat/data/multichat.db ".backup $BACKUP_DIR/multichat.db"
sqlite3 /data/synapse/homeserver.db ".backup $BACKUP_DIR/synapse.db"

# Bridge databases
for bridge in telegram whatsapp signal meta; do
    if [ -f "/data/$bridge/mautrix-$bridge.db" ]; then
        sqlite3 "/data/$bridge/mautrix-$bridge.db" ".backup $BACKUP_DIR/$bridge.db"
    fi
done

# Encrypt and upload
tar czf - "$BACKUP_DIR" | gpg --encrypt -r backup@example.com > "$BACKUP_DIR.tar.gz.gpg"
aws s3 cp "$BACKUP_DIR.tar.gz.gpg" s3://multichat-backups/

# Retain 30 days locally
find /backups/multichat -maxdepth 1 -mtime +30 -exec rm -rf {} +
```

## Monitoring

### Health Check

```bash
curl -s http://localhost:8402/api/health | jq .
# {"status": "ok"}
```

### Recommended Monitoring

- **Uptime**: Monitor `/api/health` endpoint (Uptime Kuma, Pingdom)
- **Synapse**: `/_synapse/admin/v1/server_version` for homeserver health
- **Bridge status**: Check bridge process is running, log for reconnection events
- **Disk space**: SQLite databases grow over time, especially Synapse
- **Memory**: Synapse can be memory-hungry with large room counts
- **Certificate expiry**: Alert 14 days before TLS cert expires

## Upgrading

1. Pull latest code
2. Rebuild frontend: `cd frontend && npm ci && npm run build`
3. Rebuild binary: `CGO_ENABLED=1 go build -o multichat-server ./cmd/server/`
4. Stop service: `systemctl stop multichat`
5. Replace binary and frontend assets
6. Start service: `systemctl start multichat`
7. Verify health endpoint

For Docker deployments: `docker compose pull && docker compose up -d --build`
