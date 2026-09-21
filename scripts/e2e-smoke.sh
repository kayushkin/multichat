#!/usr/bin/env bash
# Boot-and-answer smoke test for multichat.
#
# Builds cmd/server from THIS source tree, boots it on a temp port against a
# throwaway SQLite DB, and drives its real routes: health, the schema it just
# migrated, and a tag write-then-read-back.
#
# Why this exists: `go build` proves the tree compiles, not that the binary is
# alive. Go 1.22+ http.ServeMux panics on a conflicting route pattern at
# REGISTRATION time — the compiler never sees it, `go vet` never sees it, and
# the process dies on its first breath. Tier 1 of the repo guard calls such a
# tree green. This is tier 2: it boots the thing and makes it answer.
#
# WHAT MAKES THIS HERMETIC:
#   * THE WORKING DIRECTORY IS THE SANDBOX. multichat has NO env var and no flag
#     for its database path: cmd/server/main.go:17 calls
#     db.Initialize("multichat.db") with a RELATIVE path, so the DB is created in
#     whatever directory the process is started from. This script therefore runs
#     the binary with cwd set to a temp dir. Redirecting HOME — which is what the
#     nightly guard does — would NOT have protected anything here, because
#     multichat never reads HOME at all. Getting this wrong does not open a live
#     DB (there is no fixed live path to open), it litters the repo checkout with
#     a multichat.db; the assertion below pins that the file landed in the temp
#     dir and NOT in the source tree.
#   * PORT moves it off the live :8402, where multichat-server is running now.
#     A collision is fatal (main.go:92 log.Fatal on ListenAndServe).
#   * SYNAPSE_URL / MATRIX_TOKEN are left unset, so no Matrix client is built and
#     nothing dials Synapse (main.go:27-32). Matrix is NOT required to boot.
#   * MULTICHAT_TOKEN is forced empty: an ambient token in the environment would
#     switch auth on (router.go:49) and lock this script out of its own server.
#   * No NATS anywhere in this module, and no network calls at boot.
#
# ROUTES DELIBERATELY NOT DRIVEN: /api/stats and /api/contacts/unified call
# r.matrix.* with NO nil check (router.go:558, :622), so without a Matrix client
# they nil-panic the handler. That is a real bug, but it is a HANDLER bug, not a
# boot bug, and this guard is about booting. Filed separately; do not "fix" it by
# giving the smoke a Matrix.
#
# Exits 0 on success, non-zero on the FIRST failing assertion, dumping the
# server log to stderr.
#
# Env:
#   E2E_PORT   — multichat port (default 19134)
#   E2E_KEEP=1 — leave $TMP_DIR in place after the run

set -euo pipefail

REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
PORT="${E2E_PORT:-19134}"
# Every route is served under the /api prefix — main.go:83 registers the router
# behind http.StripPrefix("/api", ...), so router.go:72's "/health" is on the
# wire as "/api/health". Do not go looking for a bare /health; it 404s.
BASE="http://127.0.0.1:$PORT/api"

for bin in go curl jq; do
  if ! command -v "$bin" >/dev/null 2>&1; then
    echo "ERROR: required tool '$bin' not found on PATH" >&2
    exit 2
  fi
done

TMP_DIR="$(mktemp -d -t multichat-e2e.XXXXXX)"
BIN_DIR="$TMP_DIR/bin"
RUN_DIR="$TMP_DIR/run"      # cwd for the server == where its SQLite file lands
FAKE_HOME="$TMP_DIR/home"
DB_PATH="$RUN_DIR/multichat.db"
SERVER_LOG="$TMP_DIR/server.log"
mkdir -p "$BIN_DIR" "$RUN_DIR" "$FAKE_HOME"

# --- the live database, and how we prove we never touched it ------------------
#
# multichat resolves its SQLite file from the CWD (db.Initialize("multichat.db"),
# main.go:17). The LIVE multichat-server runs with cwd=<this repo>, so the live
# database is literally $REPO_DIR/multichat.db — a gitignored file sitting in the
# source checkout. `git status` cannot see it and HOME redirection cannot protect
# it. Running the binary from the clone is all it would take to write prod data.
#
# So: fingerprint that file BEFORE anything runs, and re-check it on EVERY exit
# path. Two lessons are baked in here, both previously paid for in real bugs:
#   * Snapshot up front. A bare `[ ! -e $REPO_DIR/multichat.db ]` check would
#     blame this run for a file that has been there since June. Fail only on a
#     change THIS run caused.
#   * Check from the trap, not on the success path. An assertion that only runs
#     when the smoke passes cannot tell you that the run which just FAILED
#     corrupted the live database on its way out.
LIVE_DB="$REPO_DIR/multichat.db"
live_fingerprint() {
  # mtime+size, or the literal "absent" — never a bare [ -e ] test.
  if [ -e "$LIVE_DB" ]; then stat -c '%Y:%s' "$LIVE_DB"; else echo absent; fi
}
LIVE_DB_BEFORE="$(live_fingerprint)"

SERVER_PID=""
cleanup() {
  if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  # Runs on success, on failure, and on interrupt — see above.
  after="$(live_fingerprint)"
  if [ "$after" != "$LIVE_DB_BEFORE" ]; then
    echo "" >&2
    echo "*** LIVE DATABASE MODIFIED: $LIVE_DB ***" >&2
    echo "    before: $LIVE_DB_BEFORE" >&2
    echo "    after:  $after" >&2
    echo "    This smoke wrote to the database the live multichat-server is using." >&2
    echo "    multichat takes its DB path from the CWD, so the binary must only" >&2
    echo "    ever be launched from a temp directory. Fix that before re-running." >&2
    rm -rf "$TMP_DIR"
    exit 1
  fi
  if [ "${E2E_KEEP:-}" = "1" ]; then
    echo "[e2e] keeping $TMP_DIR"
  else
    rm -rf "$TMP_DIR"
  fi
}
trap cleanup EXIT INT TERM

step() { printf '\n==> %s\n' "$*"; }
dump_logs() {
  echo "----- server.log -----" >&2
  cat "$SERVER_LOG" >&2 2>/dev/null || true
}
fail() { echo "FAIL: $*" >&2; dump_logs; exit 1; }

TAG="e2e-tag-$$"
COLOR="#123456"

step "build cmd/server from $REPO_DIR"
cd "$REPO_DIR"
# Explicit -o: a bare `go build ./cmd/server` writes ./server into the CWD, and
# the repo root already carries a gitignored file by that name.
# CGO stays ENABLED on purpose — multichat's only dependency is mattn/go-sqlite3,
# which is cgo-only. CGO_ENABLED=0 must fail the BUILD here rather than ship a
# binary that dies opening its own database.
go build -o "$BIN_DIR/multichat" ./cmd/server
echo "    multichat: $(ls -lh "$BIN_DIR/multichat" | awk '{print $5}')"

step "launch multichat on :$PORT (cwd: $RUN_DIR)"
# cwd IS the DB sandbox — see the header. Run from RUN_DIR, never from the clone.
cd "$RUN_DIR"
env -i \
  PATH="$PATH" \
  HOME="$FAKE_HOME" \
  PORT="$PORT" \
  "$BIN_DIR/multichat" >"$SERVER_LOG" 2>&1 &
SERVER_PID=$!
echo "    pid: $SERVER_PID"

# Poll — never sleep-and-hope. Abort the instant the pid dies: that is how a
# route-registration panic surfaces as a named failure instead of a mystery
# timeout.
health=""
for _ in $(seq 1 50); do
  if ! kill -0 "$SERVER_PID" 2>/dev/null; then
    fail "multichat exited during startup (route registration panic? port in use? sqlite schema failure?)"
  fi
  if health=$(curl -fsS --max-time 2 "$BASE/health" 2>/dev/null); then break; fi
  health=""
  sleep 0.2
done
[ -n "$health" ] || fail "multichat did not answer $BASE/health within 10s"
[ "$(jq -r '.status' <<<"$health")" = "ok" ] || fail "/api/health did not report status=ok: $health"
echo "    health OK: $health"

step "the SQLite file must be in the temp cwd, and the LIVE db must be untouched"
[ -f "$DB_PATH" ] || fail "no db at $DB_PATH — multichat did not create its DB in its cwd, so this smoke is not sandboxed"
# The live-DB check itself runs from the trap (every exit path); assert it here
# too so a leak is named at the point it happened rather than at teardown.
[ "$(live_fingerprint)" = "$LIVE_DB_BEFORE" ] \
  || fail "the live database at $LIVE_DB changed during boot — the cwd sandbox leaked"
echo "    db: $DB_PATH ($(ls -lh "$DB_PATH" | awk '{print $5}'))"
echo "    live db untouched: $LIVE_DB_BEFORE"

step "GET /api/schema — the migration really ran against that fresh DB"
SCHEMA=$(curl -fsS --max-time 10 "$BASE/schema")
[ "$(jq -r 'type' <<<"$SCHEMA")" = "array" ] || fail "/api/schema is not an array: $SCHEMA"
# Assert the tables createSchema() promises are actually there. A binary that
# boots but silently skipped its migration answers 200 with an empty list.
for want in users conversations messages platforms; do
  jq -e --arg t "$want" 'any(.[]; .name == $t)' <<<"$SCHEMA" >/dev/null \
    || fail "table '$want' missing from the migrated schema: $SCHEMA"
done
echo "    $(jq -r 'length' <<<"$SCHEMA") schema objects, core tables present"

step "GET /api/tags — must be EMPTY (proves we are on a fresh temp DB)"
TAGS=$(curl -fsS --max-time 10 "$BASE/tags")
COUNT=$(jq -r 'length' <<<"$TAGS")
[ "$COUNT" = "0" ] \
  || fail "a fresh DB should hold 0 tags but /api/tags returned $COUNT — ABORTING, this may be a REAL database"
echo "    0 tags"

step "POST /api/tags — write"
CREATED=$(curl -fsS --max-time 10 -X POST "$BASE/tags" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"$TAG\",\"color\":\"$COLOR\"}") \
  || fail "POST /api/tags failed"
NEW_ID=$(jq -r '.id' <<<"$CREATED")
case "$NEW_ID" in
  ''|null|*[!0-9]*) fail "POST /api/tags did not return a numeric id: $CREATED" ;;
esac
[ "$(jq -r '.name'  <<<"$CREATED")" = "$TAG"   ] || fail "POST echoed the wrong name: $CREATED"
[ "$(jq -r '.color' <<<"$CREATED")" = "$COLOR" ] || fail "POST echoed the wrong color: $CREATED"
echo "    created tag id=$NEW_ID name=$TAG color=$COLOR"

step "GET /api/tags — read the write back out of SQLite"
TAGS=$(curl -fsS --max-time 10 "$BASE/tags")
MINE=$(jq -c --arg n "$TAG" '.[] | select(.name == $n)' <<<"$TAGS")
[ -n "$MINE" ] || fail "the tag just written is not in /api/tags: $TAGS"
[ "$(jq -r '.id'    <<<"$MINE")" = "$NEW_ID" ] || fail "read-back id != $NEW_ID: $MINE"
[ "$(jq -r '.color' <<<"$MINE")" = "$COLOR"  ] || fail "read-back color != $COLOR: $MINE"
echo "    read back: $MINE"

step "POST /api/tags again — the UNIQUE constraint must reject it with 409"
# A write path that cannot reject a duplicate is a write path that is not really
# talking to the database it claims to be. Drive the failure branch too: a gate
# that never closes looks identical to a working one.
CODE=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 10 -X POST "$BASE/tags" \
  -H 'Content-Type: application/json' \
  -d "{\"name\":\"$TAG\",\"color\":\"$COLOR\"}")
[ "$CODE" = "409" ] || fail "duplicate tag POST returned $CODE, expected 409"
echo "    duplicate rejected with 409"

step "GET /api/settings — the port in force is the one this script chose"
# A registry that is built and never read from would show the default, 8402,
# and no unit test sees the binary's own main. MULTICHAT_TOKEN is unset here, so
# the route answers without a bearer, as every other route in this script does.
SETTINGS=$(curl -fsS --max-time 10 "$BASE/settings") || fail "GET /api/settings failed"
[ "$(jq -r '.service' <<<"$SETTINGS")" = "multichat" ] || fail "/api/settings describes the wrong service: $(jq -r '.service' <<<"$SETTINGS")"
LISTEN_PORT=$(jq -c '.settings[] | select(.key=="listen_port") | {value, source}' <<<"$SETTINGS")
[ "$LISTEN_PORT" = "{\"value\":\"$PORT\",\"source\":\"environment\"}" ] || fail "/api/settings shows listen_port as $LISTEN_PORT, want $PORT from the environment"
echo "    listen_port: $LISTEN_PORT"
CODE=$(curl -sS -o /dev/null -w '%{http_code}' --max-time 10 -X PUT "$BASE/settings/listen_port" -d '{"value":"1"}')
[ "$CODE" = "404" ] || fail "PUT /api/settings/listen_port returned $CODE, expected 404: nothing is editable"
echo "    no write mounted (PUT -> 404)"

step "process still alive after serving every route"
kill -0 "$SERVER_PID" 2>/dev/null || fail "multichat died while serving"

printf '\nsmoke test OK (multichat boots, migrates, and round-trips on :%s)\n' "$PORT"
