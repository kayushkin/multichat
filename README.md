# multichat

One inbox for WhatsApp, Telegram, Signal, Messenger, Instagram, Discord, Slack and Google Messages. The mautrix bridges carry each network into a Matrix homeserver (Synapse); multichat reads and sends through Matrix and serves the result as an HTTP API, an MCP server and a few command-line tools.

## Build

The module uses its two sibling repos through `replace` lines in `go.mod`, so clone all three side by side:

```bash
git clone https://github.com/kayushkin/multichat
git clone https://github.com/kayushkin/bus
git clone https://github.com/kayushkin/llm-bridge
cd multichat && go build ./... && go test ./...
```

## Run

`docs/deployment.md` covers Synapse and the bridges, and `docs/api.md` the HTTP API. The server takes its settings from environment variables; `GET /api/settings` lists each one and what it does.

The server still assumes the Matrix homeserver is `chat.kayushkin.com` and its admin account is `@admin`. To run it against another homeserver, change those first.

## License

MIT; see `LICENSE`.
