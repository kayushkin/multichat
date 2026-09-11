# Multichat ↔ bus integration — options

Status: **built 2026-09-11 as Option D** (bus for inbound events, direct HTTP for sends), plus Option C for agents. What exists:

- **Inbound (bus):** `internal/inbound.SyncWorker` long-polls `/sync` and publishes `messaging.inbound` (JetStream stream `MESSAGING`, envelope `messages.MessagingInbound` in `bus/messages/messaging.go`). `internal/inbound.Dispatcher` consumes it (durable `multichat-inbound-dispatcher`), applies the `inbound_rules` table and starts autonomous sessions by publishing `bridge.inbound` — see `docs/api.md` § Inbound rules.
- **Outbound (HTTP):** `POST /api/messages/send` (find-or-create the DM, then send); `~/bin/multichat-send` for shells and scheduler jobs, token resolved from auth-store provider `multichat`.
- **Agent tool (Option C):** `~/bin/multichat-mcp`, a stdio MCP server registered in tool-store as `multichat`; `send_message` is `ask` at global scope in permission-store.
- **Not built:** outbound over the bus (`messaging.outbound.*`, `messaging.sent`). Both callers are fine with a synchronous HTTP answer, and putting NATS in the path of "did it send" bought nothing.

The exploration below is kept as the record of why.

---

## Problem

Today **multichat** is an isolated service:
- Listens on `:8402`, REST only, SQLite-backed.
- Talks directly to Synapse (`SYNAPSE_URL`, `MATRIX_TOKEN`) — see `cmd/server/main.go:23-32`, `internal/matrix/client.go`.
- No NATS, no bus, no event fan-out.

Multiple services would benefit from chat access:
- **llm-bridge-server / agents** — react to inbound messages, send outbound replies.
- **scheduler** — trigger jobs on inbound message patterns; cron jobs that DM summaries on Telegram.
- **inber** — could route human messages through chat platforms instead of (or in addition to) its own surfaces.
- **logstack / dash** — archive chat history, display unread counts, escalation widgets.

Because the consumer set is plural, **point-to-point HTTP integrations don't scale** — each service would need its own multichat client, its own polling loop, its own auth handling. The bus solves this once.

## Why hermes/openclaw aren't a clean template

Both are **subprocess harnesses** spawned by llm-bridge-server (stdin/stdout NDJSON, canonical `msg.Event` types). They wrap **LLM agent backends** — Hermes is an OpenAI-compatible chat API, OpenClaw is a filesystem-backed agent runner. Their session model (`start`/`message`/`interrupt`/`fork`/`compact`) assumes "an agent that thinks."

Multichat doesn't think. It routes human messages between platforms. Wrapping it as a harness binary is the wrong shape — it'd be a stateless tool surface forced into a stateful agent contract.

The useful inspiration from hermes/openclaw is the **translation pattern**: small adapter that converts an existing service's protocol into the canonical events llm-bridge consumes. Whether that adapter lives as a subprocess, a sidecar, or inside llm-bridge itself is the open question.

## Options

### Option A — Direct HTTP from llm-bridge-server → multichat REST

llm-bridge-server gains a `multichat` client package that calls multichat's existing endpoints (`/conversations`, `/conversations/{id}/send`, `/contacts`, `/search`).

**Pros**
- Zero new infra; reuses what multichat already exposes.
- Synchronous request/response is fine for "send a message" actions.
- Simple to test.

**Cons**
- No push: agents can't react to new inbound messages without polling, or multichat needs a new SSE/WS endpoint.
- Tightly couples llm-bridge-server to multichat's HTTP shape.
- Doesn't help any *other* service (logstack, dash, scheduler) consume chat events.

**Where it fits**: best if the only need is "agent sends an outbound message" — e.g. a scheduled job that DMs a summary on Telegram.

### Option B — Multichat publishes/consumes on the bus

Multichat gains a NATS client. Two flows:
- **Inbound events**: when Synapse delivers a message into multichat (today via `/sync` polling or sync loop), multichat publishes to e.g. `chat.message.received` on the bus.
- **Outbound commands**: multichat subscribes to `chat.message.send`; any service (llm-bridge agents, scheduler, dash) can publish a send request and multichat handles delivery to Synapse.

**Pros**
- Decoupled, async. Fits the existing inber/dash/scheduler ecosystem.
- Other services get chat events for free (e.g. logstack archives, dash widgets, scheduler triggers).
- llm-bridge-server doesn't need to know multichat exists — it just sees bus topics.

**Cons**
- Two integrations to build (multichat ↔ bus, llm-bridge ↔ bus for chat topics).
- Bus becomes critical-path for messaging — outage means no sends.
- Need to define a stable event schema upfront (`chat.v1.message.received`, etc.).
- Multichat currently has no Matrix sync loop wired to push events — today its API is read-on-demand. Would need a sync worker.

**Where it fits**: best if chat is going to be a first-class signal across the system, not just a thing one agent uses.

### Option C — Multichat as a tool surface for agents

Add a `multichat` tool to agent harnesses (hermes, openclaw, claudecode). Tool calls translate to multichat HTTP. Agents decide when to send/read messages as part of their reasoning loop.

**Pros**
- LLM-native — the model decides when to reach for chat.
- No bus needed; no llm-bridge-server core changes.
- Mirrors how other tools (filesystem, web) are added.

**Cons**
- Each harness adapter needs the tool plumbed in separately.
- Still no push — agents only use chat when prompted to.
- Tool authorization/scope per session becomes a thing to design.

**Where it fits**: complementary to A or B, not a replacement. Good for "agent that operates a customer support inbox."

### Option D — Hybrid: bus for events, direct HTTP for sends

- **Inbound events**: Option B publish side. Multichat adds a Synapse sync worker → publishes `chat.message.received` on the bus. Any service subscribes.
- **Outbound sends**: Option A. Caller hits multichat HTTP directly. Synchronous response, simpler error handling than fire-and-forget pub/sub.

This avoids putting the bus in the critical path of *sending* (which has to succeed synchronously for the user to know it landed) while giving us the async event stream for *reacting*.

## Recommended direction: Option B (full bus), with D as fallback

Given the multi-consumer set (llm-bridge, scheduler, possibly inber/dash), going **bus-first for both directions** is the cleaner long-term shape. D is the pragmatic "ship it next week" version if outbound-via-bus turns out to need too much work (idempotency, ack semantics, error feedback to caller).

### How the bus works today (ground truth)

Verified in `~/repos/bus`:
- Fully migrated to NATS. The old HTTP `/publish` / `/ack` + SQLite WAL is in `*.old/` directories; current transport is pure NATS (pub/sub + JetStream for durability).
- **Each service has one subscription per subject.** Identity is the queue group (core NATS) or durable consumer name (JetStream) — NATS itself enforces "only one instance gets each message." The bus has no per-service routing logic.
- Subject convention: dotted, no version suffix. Routing-to-a-specific-service is done via **subject suffix**, not subscription logic — e.g. `chat.inbound.inber` vs `chat.inbound.bridge`.
- Existing namespaces in use: `chat.inbound.*`, `chat.stream`, `chat.outbound`, `spawn.>`, `health.>`, `forge.>`, `logs.>`.

The existing `chat.*` namespace is for **agent conversations** (user ↔ inber/llm-bridge backends). Multichat is a different flow — human ↔ human via external platforms — so it needs its own namespace to avoid collision.

### Proposed bus topology for multichat

Namespace: `messaging.*` (distinct from agent `chat.*`).

| Subject | Direction | Publisher | Subscribers | Delivery |
|---|---|---|---|---|
| `messaging.inbound` | platform → us | multichat | llm-bridge, scheduler, logstack, any interested service | JetStream, durable per subscriber |
| `messaging.outbound.multichat` | us → platform | any service | multichat (queue group: `multichat`) | JetStream, ack on send |
| `messaging.sent` | ack/echo | multichat | optional auditors, original publisher | JetStream |
| `messaging.escalation` | derived | multichat (AI scan) | scheduler, dash | JetStream |

Payloads (v0 — evolve by adding fields, not renaming):
- `messaging.inbound`: `{conversation_id, platform, sender, body, timestamp, attachments[], matrix_event_id}`
- `messaging.outbound.multichat`: `{conversation_id, body, attachments[], idempotency_key, reply_to?}`
- `messaging.sent`: `{idempotency_key, conversation_id, matrix_event_id, status, error?}`

Notes:
- **Why the `.multichat` suffix on outbound** — matches the existing `chat.inbound.<orchestrator>` pattern. If we later add a second sender (e.g. a direct-Telegram path that bypasses Synapse), it becomes `messaging.outbound.telegram` and lives alongside.
- **Queue group on outbound** — if multichat ever scales to >1 instance, NATS ensures only one consumes each send. This is the "one subscription per service, bus stays dumb" point you raised.
- **Durable consumers per service on inbound** — llm-bridge, scheduler, logstack each pick their own durable name so restarts replay from last ack. No bus-side state.
- **Idempotency key on outbound is mandatory** — JetStream is at-least-once, multichat must dedupe before hitting Synapse.
- **No `.v1` suffix** — not the current convention. Additive schema evolution instead.

### What multichat needs to add

1. **Sync worker** — `internal/matrix/sync.go` long-polls Synapse `/sync`, normalizes events. (First open question to verify: does any sync loop exist today?)
2. **Bus publisher** — `internal/bus/publisher.go`, env-gated (`NATS_URL`). Publishes `messaging.inbound`, `messaging.sent`, `messaging.escalation`.
3. **Bus consumer** — durable JetStream consumer on `messaging.outbound.multichat` (queue group `multichat`), dedupes on `idempotency_key`, calls existing `internal/matrix/client.go` send path, publishes `messaging.sent`.
4. **Schema docs** — extend `docs/integrations.md` with subject contract.

HTTP API stays as-is — useful for dashboards and direct human use; bus is additive.

### What other services need to add

The bus client likely already exists in inber-adjacent services. Each service:
- Picks its own durable consumer name (e.g. `llm-bridge`, `scheduler-multichat`, `logstack-messaging`) on `messaging.inbound`.
- Publishes to `messaging.outbound.multichat` with an `idempotency_key`.

For llm-bridge agents specifically: chat send/receive can also be exposed as a **tool** (Option C) so the LLM decides when to use it. Inbound triggering a session start is a separate decision — TBD.

## Open questions

- Does multichat already have a Synapse sync loop, or is every read a fresh `/sync`? Affects how much work the sync worker is. (Check `internal/matrix/client.go`.)
- Send authorization: trust internal services on the bus (current pattern), or require a token in the message envelope?
- Should we co-locate `messaging.*` subjects with the agent `chat.*` namespace, or keep strict separation? (Leaning: separate — different audiences, different durability needs.)
- Inbound trigger model for llm-bridge: does a received message *start a session* automatically, or just become available via a tool the agent can poll?
- MVP scope: Telegram only first, or all bridges (Telegram + WhatsApp) from day one?
