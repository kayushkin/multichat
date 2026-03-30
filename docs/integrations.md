# Platform Integrations

## How Bridges Work

MultiChat connects to messaging platforms through [mautrix](https://github.com/mautrix) bridges. Each bridge:

1. **Authenticates** with the platform using native credentials
2. **Syncs** contacts and conversations into Matrix rooms on Synapse
3. **Relays** messages bidirectionally between Matrix and the platform
4. **Maintains** a local SQLite database for platform-specific state

The Go API server never talks to platforms directly — it only interacts with Synapse via the Matrix Client-Server API.

## Telegram

### Setup

**Option A: Bot Mode** (recommended for receiving)

1. Create a bot via [@BotFather](https://t.me/BotFather)
2. Get the bot token
3. Configure mautrix-telegram:

```yaml
# mautrix-telegram/config.yaml
appservice:
  address: http://localhost:29317
  hostname: 0.0.0.0
  port: 29317

homeserver:
  address: http://synapse:8008
  domain: chat.example.com

bridge:
  permissions:
    '*': relaybot
    '@admin:chat.example.com': admin

telegram:
  api_id: YOUR_API_ID      # from my.telegram.org
  api_hash: YOUR_API_HASH
```

**Option B: User Mode** (full conversation access)

Login as a real Telegram user for access to all personal and group chats:

```
# In the bridge admin room
!tg login
# Follow phone number + code flow
```

### What Gets Bridged

- Text messages, photos, documents, stickers, voice messages
- Group chats and channels
- Contact names and profile photos
- Phone numbers (via bridge database for identity merging)
- Read receipts
- Message edits and deletions

### Limitations

- Bot mode: only sees messages sent to the bot or in groups where it's a member
- User mode: may trigger Telegram's "suspicious activity" detection
- Large media files may be slow to bridge
- Telegram Premium features (custom emoji) may not bridge

## WhatsApp

### Setup

mautrix-whatsapp uses the WhatsApp Web multi-device protocol. No phone number API needed.

```yaml
# mautrix-whatsapp/config.yaml
appservice:
  address: http://localhost:29318
  hostname: 0.0.0.0
  port: 29318

homeserver:
  address: http://synapse:8008
  domain: chat.example.com

bridge:
  permissions:
    '@admin:chat.example.com': admin
  history_sync:
    backfill: true
    max_initial_conversations: -1  # All conversations
```

### Linking

1. Start the bridge
2. In the Matrix admin room: `!wa login`
3. Bridge generates a QR code
4. Scan with WhatsApp on phone → Linked Devices → Link a Device

### What Gets Bridged

- Text, images, documents, audio, video, contacts, location
- Group chats
- Status/story messages (optional)
- Read receipts
- Reactions
- Reply threading

### Limitations

- Phone must have internet at least once every 14 days or session expires
- Business API accounts have different rate limits
- Broadcast lists are not supported
- End-to-end encryption is handled by the bridge (decrypted before Matrix)

## Signal

### Setup

```yaml
# mautrix-signal/config.yaml
appservice:
  address: http://localhost:29328
  hostname: 0.0.0.0
  port: 29328

homeserver:
  address: http://synapse:8008
  domain: chat.example.com

signal:
  socket_path: /var/run/signald/signald.sock
```

### Registration

1. Start signald and mautrix-signal
2. In admin room: `!signal register +15551234567`
3. Enter verification code received via SMS
4. Complete CAPTCHA if required

### What Gets Bridged

- Text, images, documents, voice notes
- Group chats
- Disappearing messages (respected — deleted after timer)
- Reactions
- Read receipts

### Limitations

- Can only be linked to one device at a time (Signal protocol limitation)
- Disappearing messages are deleted from Matrix too
- Signal's anti-spam may block registration from servers
- Media quality may be reduced

## Meta (Facebook Messenger + Instagram)

### Setup

One bridge handles both Facebook Messenger and Instagram DMs.

```yaml
# mautrix-meta/config.yaml
appservice:
  address: http://localhost:29319
  hostname: 0.0.0.0
  port: 29319

homeserver:
  address: http://synapse:8008
  domain: chat.example.com

meta:
  mode: facebook  # or "instagram" or both via separate logins
```

### Authentication

Meta bridges use browser cookie authentication:

1. Log into Facebook/Instagram in a browser
2. Extract cookies (c_user, xs for Facebook)
3. In admin room: `!meta login <cookies>`

**Important:** Cookie sessions expire. You'll need to re-authenticate periodically (typically every 1–4 weeks).

### What Gets Bridged

- Text, images, GIFs, stickers, voice clips
- Group chats (Messenger)
- Instagram DMs
- Reactions
- Read receipts

### Limitations

- Cookie auth is fragile — Meta may flag automated access
- Facebook may require 2FA re-verification
- Instagram: only DMs, not story replies
- Business pages have separate authentication

### Platform Identification

MultiChat resolves whether a Meta contact is from Facebook or Instagram by querying the bridge database:
- Each `user_login` has a `platform` field in metadata
- Portal rooms are mapped to login IDs to determine platform
- Unresolved contacts default to "facebook"

## iMessage

### Setup

**Requirements:**
- macOS host (physical Mac or VM with macOS)
- Apple ID signed into Messages app
- mautrix-imessage installed on the Mac

```yaml
# mautrix-imessage/config.yaml
homeserver:
  address: http://synapse-host:8008
  domain: chat.example.com

imessage:
  platform: mac
  imessage_rest_path: /usr/local/bin/mautrix-imessage
```

### Authentication

iMessage auth uses the macOS system's Apple ID session. No additional credentials needed — if Messages.app works, the bridge works.

### What Gets Bridged

- iMessage (blue bubble) and SMS (green bubble) conversations
- Text, images, videos, audio
- Tapback reactions
- Group chats
- Read receipts

### Limitations

- **macOS only** — cannot run in Docker or on Linux
- Requires a dedicated Mac (Mac Mini recommended)
- Apple may limit or break third-party access in future macOS updates
- SMS relay requires iPhone linked to the same Apple ID
- No iPad-only messages

### Hosting Considerations

For enterprise deployments:
- Dedicated Mac Mini in a rack or colo
- Remote management via SSH + VNC
- Auto-login configured (no FileVault on boot volume)
- Automatic macOS updates disabled (may break bridge)
- UPS for power protection

## Adding a New Platform

To integrate a platform not yet supported:

1. **Check mautrix**: They support 10+ platforms. Check [mau.fi/bridges](https://mau.fi/bridges/).
2. **Deploy the bridge** as a Docker container or systemd service
3. **Register with Synapse** via appservice registration YAML
4. **Configure** bridge → Synapse connection
5. **Authenticate** with the platform
6. **Add platform icon** in `frontend/src/components/PlatformIcon.tsx`
7. **Add to Dashboard** platform rows in `frontend/src/pages/Dashboard.tsx`
8. (Optional) **Add bridge DB integration** for enhanced identity resolution

The Go API server auto-discovers new rooms and contacts from Synapse — no backend code changes needed for basic support.

## Bridge Database Access

Some MultiChat features require read access to bridge SQLite databases:

| Bridge | DB Purpose | Env Var |
|--------|-----------|---------|
| Telegram | Phone number lookups | `TELEGRAM_DB` |
| Meta | Facebook vs Instagram distinction | `META_DB` |

Databases are opened in read-only mode (`?mode=ro`). The Go API server never writes to bridge databases.

## Troubleshooting Bridges

### General

```bash
# Check bridge is registered with Synapse
curl -s http://localhost:8008/_synapse/admin/v1/appservices \
  -H "Authorization: Bearer $ADMIN_TOKEN" | jq .

# Check bridge process
docker logs mautrix-whatsapp --tail 100 --follow

# Verify bridge can reach Synapse
docker exec mautrix-whatsapp curl -s http://synapse:8008/_matrix/client/versions
```

### Bridge Won't Start

- Check config YAML syntax
- Verify homeserver address is reachable from bridge container
- Check appservice registration is in Synapse's `app_service_config_files`
- Verify port isn't already in use

### Messages Not Bridging

- Check bridge is authenticated (`!<bridge> status` in admin room)
- Verify the conversation exists as a Matrix room
- Check for rate limiting from the platform
- Review bridge logs for error messages
- Try restarting the bridge

### Contacts Missing

- Run `!<bridge> sync` in admin room to force contact sync
- Check bridge's puppet table for the contact
- Verify the contact has messaged recently (some bridges only sync active contacts)
