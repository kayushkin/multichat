# Administration Guide

## User Management

### Current State

MultiChat currently uses a shared token for all users. Everyone who has the token has full access.

### Planned: Multi-User System

The multi-user system will support:

- **User accounts** with email/password or SSO
- **Role assignments** (Admin, Manager, Broker, Auditor)
- **Team management** — group brokers into teams with shared conversation pools
- **Contact assignment** — assign contacts/accounts to specific brokers
- **Activity tracking** — who sent what, when, from which IP

### User Onboarding Flow

1. Admin creates user account or sends invite link
2. User sets password (or logs in via SSO)
3. Admin assigns role and team
4. System generates per-user API token
5. User accesses dashboard at the shared URL

### User Offboarding

1. Admin disables account (immediate session revocation)
2. Reassign contacts to another broker
3. Retain conversation history (for compliance)
4. After retention period: option to anonymize user's actions in audit log

## Platform Management

### Connecting a New Platform

Each platform requires its own bridge service. The general flow:

1. Deploy the mautrix bridge (Docker container or systemd service)
2. Configure bridge → Synapse connection
3. Authenticate with the platform (bot token, QR code, phone number, etc.)
4. Bridge creates "ghost" users in Matrix for each platform contact
5. MultiChat automatically discovers conversations via the Synapse API

### Platform Status Monitoring

The Dashboard page shows connection status per platform:
- **Connected** (green) — Bridge is running and has synced contacts
- **Not connected** (gray) — Bridge not configured or no contacts synced

Check bridge health:
```bash
# Docker
docker logs mautrix-telegram --tail 50

# Systemd
journalctl -u mautrix-telegram -n 50
```

Common bridge issues:
- **Session expired** — Re-authenticate (WhatsApp: re-scan QR, Telegram: re-login)
- **Rate limited** — Reduce sync frequency in bridge config
- **Database locked** — Restart bridge (SQLite WAL mode issue)

### Platform-Specific Notes

**Telegram:**
- Bot mode: limited to messages sent to the bot
- User mode (MTProto): sees all conversations but may trigger Telegram's abuse detection
- Recommendation: Use bot mode for receiving, user mode for historical import

**WhatsApp:**
- Multi-device mode: phone can be offline
- Session expires if phone is offline for 14+ days
- Group messages supported
- Media: images and documents bridged automatically

**Signal:**
- Requires signald or native Signal protocol
- Phone number verification needed
- Disappearing messages are respected (not stored after expiry)

**Meta (Facebook + Instagram):**
- Single bridge handles both platforms
- Uses browser cookies for authentication (fragile — cookies expire)
- Monitor for re-authentication needs weekly

**iMessage:**
- Requires macOS host (or VM) with Messages app
- Apple ID must be signed in
- Only works on the machine running the bridge
- Cannot run in Docker (requires macOS frameworks)

## Alert Configuration

### Default Keywords

MultiChat ships with these alert keywords:

| Keyword | Default Severity |
|---------|-----------------|
| urgent | high |
| escalate | high |
| angry | high |
| complaint | medium |
| failed | medium |
| refund | medium |
| late | low |
| damaged | low |
| delay | low |
| cancel | low |
| problem | low |
| issue | low |
| wrong | low |
| broken | low |
| missing | low |

### Custom Keywords

Add industry-specific keywords via the Alerts page or API:

```bash
# Add a keyword
curl -X POST https://chat.example.com/api/alerts \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"action": "add", "keyword": "detention", "severity": "high"}'

# Change severity
curl -X POST https://chat.example.com/api/alerts \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"action": "set-severity", "keyword": "detention", "severity": "medium"}'
```

### Recommended Keywords for Freight Brokerage

**High severity:** `detention`, `refused`, `lawsuit`, `DOT`, `FMCSA`, `accident`, `spill`, `contaminated`

**Medium severity:** `late pickup`, `wrong address`, `overweight`, `reefer breakdown`, `double brokered`, `no-show`

**Low severity:** `reschedule`, `delay`, `partial`, `reroute`, `ETA change`

## AI Configuration

### Model Selection

MultiChat supports OpenAI and Anthropic for AI features:

| Feature | Default Model | Tokens/Call |
|---------|--------------|-------------|
| Summarize | gpt-4o-mini / claude-sonnet | ~500 |
| Escalation scan | gpt-4o-mini | ~500 × 10 rooms |
| Auto-tag | gpt-4o-mini | ~500 |

Estimated cost: ~$0.01–0.05 per AI action with gpt-4o-mini.

### Self-Hosted AI (Future)

For maximum data privacy, run a local LLM:
- Ollama with Llama 3.1 70B or Mixtral 8x22B
- Configure `AI_BASE_URL` to point to local inference server
- No data leaves your network

## Tag System

### Best Practices

Organize tags into categories:

**Freight type:** LTL, FTL, Partial, Intermodal
**Equipment:** Dry Van, Refrigerated, Flatbed, Step Deck, Power Only
**Service level:** Standard, Expedited, White Glove, Last Mile
**Status:** Active, Pending Quote, Booked, In Transit, Delivered, Closed
**Priority:** Urgent, Routine, Low Priority
**Region:** Northeast, Southeast, Midwest, West Coast, Cross-Country

### Auto-Tagging

The AI auto-tag feature analyzes conversation content and suggests tags. It:
1. Reads the last 50 messages in a conversation
2. Compares against your existing tag list
3. Suggests which existing tags apply
4. Proposes new tags for uncovered topics

Best used after initial contact setup to categorize your book of business.

## Performance Tuning

### Synapse

For deployments with 1000+ rooms:

```yaml
# homeserver.yaml
caches:
  global_factor: 2.0
  per_cache_factors:
    get_users_in_room: 5.0

# Consider PostgreSQL for 5000+ rooms
database:
  name: psycopg2
  args:
    host: localhost
    database: synapse
    user: synapse
    password: ...
    cp_min: 5
    cp_max: 10
```

### Go API Server

The API server is lightweight. For high-traffic deployments:
- Run 2–4 instances behind a load balancer
- Enable connection pooling for SQLite (WAL mode)
- Cache contact lists in Redis (60s TTL)
- Paginate search results (currently scans all rooms)

### Frontend

- Static assets served by nginx with 30-day cache headers
- Gzip/Brotli compression via nginx
- Consider CDN for multi-region deployments

## Troubleshooting

### Messages Not Appearing

1. Check bridge logs for the platform
2. Verify bridge is running: `docker ps` or `systemctl status`
3. Check Synapse is reachable: `curl http://localhost:8008/_matrix/client/versions`
4. Verify Matrix token: try a manual API call
5. Check room membership: bridge user must be in the room

### Slow Search

Search currently scans all conversations serially. For large deployments:
- Reduce conversation count by archiving old rooms in Synapse
- Implement full-text search index (planned: SQLite FTS5 or Elasticsearch)
- Limit search scope to recent conversations

### High Memory Usage

Usually Synapse, not MultiChat itself:
- Check `synapse` process memory
- Tune cache factors (see Performance Tuning)
- Consider PostgreSQL migration (lower memory footprint for large datasets)
- Restart Synapse weekly as preventive measure

### Bridge Disconnected

Each platform has different reconnection behavior:
- **Telegram**: Usually auto-reconnects. Check for IP bans if repeated.
- **WhatsApp**: May need QR re-scan if phone was offline too long.
- **Signal**: Check signald process. May need re-verification.
- **Meta**: Cookies expire. Re-authenticate in bridge admin.
