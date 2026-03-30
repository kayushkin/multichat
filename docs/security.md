# Security

## Threat Model

MultiChat aggregates business communications across multiple platforms. A compromise exposes:

- **All messaging history** across every connected platform
- **Active sessions** to Telegram, WhatsApp, Signal, Facebook, Instagram, iMessage
- **Contact databases** with phone numbers, names, platform identifiers
- **AI API keys** with billing implications
- **Business intelligence** — conversation patterns, customer relationships, negotiation details

This makes MultiChat a **high-value target**. Security must be treated accordingly.

## Authentication & Authorization

### Current: Token-Based Auth

All API requests require a bearer token (`MULTICHAT_TOKEN`) passed via:
- `Authorization: Bearer <token>` header, or
- `multichat_token` HTTP cookie (set at login)

Unauthenticated endpoints: `/api/auth/login`, `/api/health`

### Planned: Role-Based Access Control (RBAC)

| Role | Permissions |
|------|------------|
| **Admin** | Full access. User management, platform configuration, API keys |
| **Manager** | View all conversations, alerts, analytics. Cannot send messages |
| **Broker** | View and respond to assigned conversations. Cannot see other brokers' chats |
| **Auditor** | Read-only access to all conversations. Export capability. No send |
| **API** | Programmatic access with scoped permissions |

Implementation plan:
- User table with role assignments
- Per-request authorization middleware
- Conversation-level access control (broker can only see their assigned contacts)
- Audit log for all role changes

### Planned: SSO Integration

- **SAML 2.0** — For enterprise identity providers (Okta, Azure AD, OneLogin)
- **OpenID Connect** — For Google Workspace, Auth0
- **LDAP** — For on-premise Active Directory
- Automatic role mapping from IdP groups

### Session Security

- Sessions expire after 30 days (configurable)
- Token rotation on sensitive operations
- Concurrent session limits per user
- Session revocation on password change
- IP binding (optional — flag sessions from new IPs)

## Network Security

### Required

- **TLS everywhere** — All traffic over HTTPS. HSTS enforced.
- **Internal-only Synapse** — Matrix homeserver should NOT be exposed to the internet. Only the Go API server communicates with it.
- **Bridge isolation** — Bridge processes communicate only with Synapse on localhost or Docker network. Never exposed externally.
- **Firewall rules** — Only ports 443 (HTTPS) and optionally 80 (redirect) open to the internet.

### Network Architecture

```
Internet → nginx (:443) → Go API (:8402) → Synapse (:8008)
                                               ↕
                                          Bridges (internal)
```

All inter-service communication is localhost or Docker-internal network. No service except nginx listens on a public interface.

### Recommended

- **VPN/Tailnet** — Access MultiChat only via corporate VPN or Tailscale
- **IP allowlisting** — Restrict access to known office IPs
- **Geo-blocking** — Block traffic from countries where your team doesn't operate
- **DDoS protection** — Cloudflare or similar in front of nginx
- **WAF rules** — Block common attack patterns

## Data Security

### Encryption

| Layer | Method | Status |
|-------|--------|--------|
| Transport | TLS 1.3 | Required |
| Matrix events | Megolm E2E (optional per room) | Available |
| SQLite at rest | OS-level disk encryption | Recommended |
| Backups | GPG encryption | Required |
| API keys | Environment variables, not in code | Required |

### Data Residency

All data stays on your infrastructure. MultiChat does not phone home or send telemetry. The only external calls are:
- Bridge connections to messaging platform APIs (Telegram, WhatsApp, etc.)
- AI API calls to OpenAI/Anthropic (when AI features are used)

For maximum data control:
- Deploy on your own hardware or trusted cloud provider
- Use a self-hosted LLM (e.g., Llama, Mixtral) instead of cloud AI APIs
- Disable Matrix federation (default: disabled)

### Data Retention

Configure retention policies per your compliance requirements:

```yaml
# Synapse homeserver.yaml
retention:
  enabled: true
  default_policy:
    min_lifetime: 1d
    max_lifetime: 365d  # Auto-purge after 1 year
  purge_jobs:
    - longest_max_lifetime: 365d
      interval: 1d
```

Application-level retention:
- Alert history: configurable (default 90 days)
- Search index: follows Synapse retention
- Tags and contacts: retained until manually deleted
- AI analysis results: not persisted (computed on-demand)

## API Security

### Rate Limiting

Configured at nginx level:

| Endpoint Pattern | Rate Limit | Burst |
|-----------------|------------|-------|
| `/api/auth/login` | 5/min | 3 |
| `/api/ai/*` | 10/min | 5 |
| `/api/search` | 30/min | 10 |
| `/api/*` (general) | 60/s | 100 |

### Input Validation

- All user input sanitized before Matrix API calls
- JSON request bodies size-limited (1MB default)
- Query parameters validated and bounded (e.g., `limit` max 200)
- Room IDs validated against Matrix ID format
- No raw SQL — parameterized queries only

### CORS

Currently permissive (`Access-Control-Allow-Origin: *`) for development. Production should restrict to the deployment domain:

```go
w.Header().Set("Access-Control-Allow-Origin", "https://chat.example.com")
```

## Bridge Security

### Credential Storage

Each bridge maintains platform credentials:

| Bridge | Credential Type | Storage |
|--------|----------------|---------|
| Telegram | Bot token or user session | Bridge SQLite DB |
| WhatsApp | Multi-device keys | Bridge SQLite DB |
| Signal | Identity keys + session keys | Bridge SQLite DB |
| Meta | Facebook cookies | Bridge SQLite DB |
| iMessage | Apple ID session | macOS Keychain |

**Critical**: Bridge databases contain live session credentials. If stolen, an attacker can impersonate your accounts on those platforms.

Protect bridge databases:
- File permissions: `600` (owner read/write only)
- Dedicated service user per bridge
- Encrypted filesystem or volume
- Never include in non-encrypted backups

### Bridge Process Isolation

Each bridge should run as a separate OS user or in a separate container:

```
multichat-telegram  → runs as user 'mautrix-telegram'
multichat-whatsapp  → runs as user 'mautrix-whatsapp'
multichat-signal    → runs as user 'mautrix-signal'
multichat-meta      → runs as user 'mautrix-meta'
```

Or via Docker with:
- Read-only root filesystem
- No network access except to Synapse
- Dropped capabilities
- Memory limits

## Compliance Considerations

### GDPR / Data Privacy

- **Right to erasure**: Implement contact deletion that purges from Matrix rooms, bridge DBs, and application DB
- **Data export**: Conversation export feature supports data portability
- **Consent**: Document that contacts' messages are stored in your system
- **DPA**: If using cloud AI (OpenAI/Anthropic), their DPA covers data processing
- **Retention**: Configure auto-purge aligned with your data processing agreement

### SOC 2 / ISO 27001

If pursuing compliance certification:
- Enable audit logging for all API access
- Implement change management for configuration
- Document incident response procedures
- Regular security assessments
- Access reviews (who has what role)

### Industry-Specific

**Freight/Logistics (FMCSA)**:
- Retain communication records per broker recordkeeping requirements
- Ensure rate confirmations and load tenders in messages are preserved
- Export capability for dispute resolution

**Financial Services**:
- Message archiving may fall under SEC/FINRA requirements
- Consider write-once storage for compliance archives
- Supervisor review capabilities (Manager/Auditor roles)

## Audit Logging

### Planned: Comprehensive Audit Trail

Every significant action should be logged:

```json
{
  "timestamp": "2026-03-30T21:30:00Z",
  "user": "broker@example.com",
  "action": "message.send",
  "room_id": "!abc123:chat.example.com",
  "platform": "whatsapp",
  "ip": "10.0.1.50",
  "user_agent": "Mozilla/5.0..."
}
```

Log events:
- Login/logout (success and failure)
- Message sent/read
- Contact created/modified/deleted
- Tag changes
- AI analysis requests
- Configuration changes
- Role/permission changes
- Export/download actions

Store audit logs separately from application data. Forward to SIEM (Splunk, ELK) for analysis and alerting.

## Incident Response

### If Bridge Credentials Are Compromised

1. **Immediately**: Stop the compromised bridge service
2. **Revoke**: Log out of the platform (Telegram: revoke session, WhatsApp: unlink device, etc.)
3. **Rotate**: Re-authenticate the bridge with new credentials
4. **Audit**: Review Matrix event history for unauthorized messages
5. **Notify**: Inform affected contacts if messages were sent on their behalf

### If API Token Is Compromised

1. **Rotate**: Change `MULTICHAT_TOKEN` in `.env`, restart service
2. **Audit**: Review access logs for unauthorized API calls
3. **Review**: Check for data exfiltration (bulk contact/message reads)

### If Server Is Compromised

1. **Isolate**: Take server offline
2. **Preserve**: Snapshot disk for forensic analysis
3. **Revoke all**: Rotate every credential (Matrix token, API keys, bridge sessions, AI keys)
4. **Rebuild**: Deploy fresh from known-good source
5. **Restore**: Restore data from verified backup
6. **Notify**: Disclose per your incident response policy

## Security Checklist

### Before Going Live

- [ ] TLS certificate installed and HSTS enabled
- [ ] `MULTICHAT_TOKEN` is a strong random secret (32+ characters)
- [ ] `MATRIX_TOKEN` has minimal required permissions
- [ ] Synapse not exposed to public internet
- [ ] Bridge databases have restrictive file permissions
- [ ] Firewall allows only 443/80 inbound
- [ ] CORS restricted to deployment domain
- [ ] Rate limiting configured
- [ ] Backup encryption configured and tested
- [ ] Monitoring and alerting in place
- [ ] Audit logging enabled

### Regular Maintenance

- [ ] Rotate API tokens quarterly
- [ ] Review user access and roles monthly
- [ ] Update Synapse, bridges, and dependencies monthly
- [ ] Test backup restoration quarterly
- [ ] Review audit logs for anomalies weekly
- [ ] Renew TLS certificates before expiry (automate with certbot)
