#!/usr/bin/env bash
set -euo pipefail

# One shared gate decides whether this tree may be deployed (main clone, default
# branch, clean, pushed, not behind, and the same for every tree the build reads).
# It lives in healthcheck/scripts/deploy-gate.sh. Do not inline or copy it.
( cd "$(dirname "$0")" && "$HOME/bin/deploy-gate" check )

# Builds multichat (frontend + backend), installs the binary, the frontend and the
# systemd user unit, restarts the service, and refuses to report success unless the
# thing actually answers -- with authentication ON and its real database open.
#
# WHY THIS EXISTS: until now multichat had no deploy path at all. The binary was
# hand-built and hand-copied, the frontend was hand-built and hand-copied to a
# directory OUTSIDE the repo that nginx serves from, and the systemd unit existed
# only on this box. Three artifacts, three sets of steps, all of them in somebody's
# head. The sibling case is forge, whose binary sat 4 months and 30 commits behind
# HEAD with six shipped fixes stranded in the repo, because there was nothing to run.
#
# THIS IS A USER UNIT, NOT A SYSTEM ONE. multichat-server.service lives in
# ~/.config/systemd/user and runs inside user@1000.service, so everything here is
# `systemctl --user` and nothing here needs sudo. (In the SYSTEM manager this unit is
# LoadState=not-found, which reads exactly like a service that does not exist.)
#
# THE THREE ARTIFACTS, and where each actually goes:
#   1. the Go binary  -> ~/bin/multichat-server        (ExecStart)
#   2. the frontend   -> ~/multichat-frontend          (nginx's root, NOT in the repo)
#   3. the unit       -> ~/.config/systemd/user/...    (installed from deploy/)
# Miss #2 and you ship a backend nobody can see; the site keeps serving the last
# hand-copied bundle, so nothing looks broken.

REPO_DIR="$(cd "$(dirname "$0")" && pwd)"
UNIT_NAME="multichat-server.service"
REPO_UNIT="$REPO_DIR/deploy/$UNIT_NAME"
LIVE_UNIT="$HOME/.config/systemd/user/$UNIT_NAME"
BACKUP_DIR="$HOME/.local/share/multichat-deploy-backup"
STAMP="$(date +%Y%m%dT%H%M%S)"

cd "$REPO_DIR"

# systemctl --user talks to the user manager over DBus, and from a non-login shell
# (an agent, a cron job) those two variables are unset -- in which case systemctl
# returns EMPTY OUTPUT AND EXIT 0 rather than an error. Silence then reads as "the
# unit isn't there", which is indistinguishable from a real finding. Set them.
export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}"
export DBUS_SESSION_BUS_ADDRESS="${DBUS_SESSION_BUS_ADDRESS:-unix:path=${XDG_RUNTIME_DIR}/bus}"

# go, node and npm exist ONLY under mise on this host (there is no /usr/bin/node at
# all), and a stripped environment -- systemd, the scheduler -- carries HOME but no
# PATH. Resolve the real binaries rather than trusting the caller's PATH, and fail
# loudly naming the tool if we cannot. Prefer `mise which` over the shims: a shim
# re-reads mise config from the CWD and refuses to run from an untrusted path.
MISE="$HOME/.local/bin/mise"
resolve_tool() {
  local tool="$1" path=""
  if [ -x "$MISE" ]; then path="$("$MISE" which "$tool" 2>/dev/null || true)"; fi
  [ -n "$path" ] || path="$(command -v "$tool" 2>/dev/null || true)"
  [ -n "$path" ] || { echo "ERROR: cannot resolve '$tool' (not via mise, not on PATH)"; exit 1; }
  printf '%s\n' "$path"
}
GO="$(resolve_tool go)"
NPM="$(resolve_tool npm)"
NODE="$(resolve_tool node)"
export PATH="$(dirname "$NODE"):$PATH"   # vite/tsc shell out to node

[ -f "$REPO_UNIT" ] || { echo "ERROR: no unit template at $REPO_UNIT"; exit 1; }

# Everything the deploy needs is already declared in the unit, so read it from there
# rather than restating it here -- a second copy of a value is a second thing to
# drift. %h is systemd's specifier for the user's home; expand it as systemd will.
unit_field() { grep -E "^$1=" "$REPO_UNIT" | head -1 | cut -d= -f2- | sed "s|%h|$HOME|g"; }
env_field()  { grep -E "^Environment=$1=" "$REPO_UNIT" | head -1 | cut -d= -f3- | sed "s|%h|$HOME|g"; }
BINARY_PATH="$(unit_field ExecStart)"
BINARY_NAME="$(basename "$BINARY_PATH")"
WORK_DIR="$(unit_field WorkingDirectory)"
ENV_FILE="$(unit_field EnvironmentFile)"
PORT="$(env_field PORT)"
DB_PATH="$(env_field MULTICHAT_DB)"

# nginx serves the frontend from its own root, which is NOT inside this repo. Read
# that path out of the vhost rather than hardcoding a second copy of it here -- the
# vhost is the thing that actually decides, so it is the thing to ask.
VHOST="$REPO_DIR/deploy/nginx/chat.kayushkin.com.conf"
FRONTEND_ROOT="$(grep -E '^\s*root\s' "$VHOST" | head -1 | awk '{print $2}' | tr -d ';')"

echo "==> Unit declares:"
echo "    ExecStart        = $BINARY_PATH"
echo "    WorkingDirectory = $WORK_DIR"
echo "    EnvironmentFile  = $ENV_FILE"
echo "    PORT             = $PORT"
echo "    MULTICHAT_DB     = $DB_PATH"
echo "    frontend root    = $FRONTEND_ROOT   (from the vhost)"

# --- Secrets ------------------------------------------------------------------
# The three secrets used to sit inline in the live unit. They cannot be committed
# (this repo has a public remote), so they live in a host-local EnvironmentFile.
# On the first deploy, migrate them OFF the live unit rather than making a human
# retype a Matrix token from memory -- the values are right there.
REQUIRED_SECRETS="MULTICHAT_TOKEN MATRIX_TOKEN OPENAI_API_KEY"
if [ ! -f "$ENV_FILE" ]; then
  echo "==> No $ENV_FILE yet -- migrating secrets off the live unit..."
  [ -f "$LIVE_UNIT" ] || { echo "ERROR: $ENV_FILE does not exist and there is no live unit to migrate secrets from. Create it by hand with: $REQUIRED_SECRETS"; exit 1; }
  mkdir -p "$(dirname "$ENV_FILE")"
  # The umask is set INSIDE a subshell, so it cannot leak. Setting `umask 077` in the
  # main shell here -- which is the obvious way to write this -- silently poisons
  # every file the rest of the script creates: the frontend then builds and installs
  # mode 700/600, nginx runs as www-data, cannot read a byte of it, and the site
  # serves 403 while this script prints "smoke test OK". That is not hypothetical, it
  # is what the first run of this script did to chat.kayushkin.com.
  (
    umask 077
    {
      echo "# multichat secrets. Host-local, never committed -- this repo is public."
      echo "# Migrated off the inline Environment= lines of $LIVE_UNIT on $STAMP."
      for key in $REQUIRED_SECRETS; do
        grep -E "^Environment=$key=" "$LIVE_UNIT" | head -1 | cut -d= -f3- | sed "s|^|$key=|"
      done
    } > "$ENV_FILE"
  )
  chmod 600 "$ENV_FILE"
  echo "    wrote $ENV_FILE (mode 600)"
fi

# Refuse to deploy behind a secret we cannot prove. An EMPTY value is the dangerous
# case, not a missing file: `if authToken != ""` (api/router.go:49) means an empty
# MULTICHAT_TOKEN does not lock the door, it REMOVES it -- and this API is on the
# public internet at chat.kayushkin.com serving contacts, conversations and message
# history. A deploy that quietly wrote an empty token would publish all of it and
# print "smoke test OK". So: check, and say which one is missing.
echo "==> Checking secrets in $ENV_FILE..."
[ -f "$ENV_FILE" ] || { echo "ERROR: EnvironmentFile $ENV_FILE does not exist -- systemd cannot load it and the service will crash-loop"; exit 1; }
for key in $REQUIRED_SECRETS; do
  value="$(grep -E "^$key=" "$ENV_FILE" | head -1 | cut -d= -f2- || true)"
  [ -n "$value" ] || { echo "ERROR: $key is missing or EMPTY in $ENV_FILE. Refusing to deploy: an empty MULTICHAT_TOKEN disables authentication on a public API."; exit 1; }
  echo "    $key present (${#value} chars)"
done

echo "==> Preflight..."
# A WorkingDirectory that does not exist fails the start with an opaque `200/CHDIR`,
# and with Restart= set that becomes a silent crash loop -- three services on this
# box ran up 1.8 MILLION failed starts that way. Check it where we can name it.
[ -d "$WORK_DIR" ] || { echo "ERROR: WorkingDirectory $WORK_DIR does not exist -- systemd cannot chdir and will crash-loop"; exit 1; }
[ -d "$FRONTEND_ROOT" ] || { echo "ERROR: nginx frontend root $FRONTEND_ROOT does not exist"; exit 1; }
# The live database must already be there. This deploy must never be the thing that
# creates it: an empty DB is not an error to SQLite, it is a fresh start, and the
# service would come up green with every contact and tag gone.
[ -f "$DB_PATH" ] || { echo "ERROR: database $DB_PATH does not exist. Refusing to deploy -- multichat would silently create an empty one and come up looking healthy."; exit 1; }
ROWS_BEFORE="$(sqlite3 "$DB_PATH" 'SELECT COUNT(*) FROM tags;' 2>/dev/null || echo 0)"
echo "    WorkingDirectory, frontend root and database all exist ($ROWS_BEFORE tags before deploy)"

# Baseline what the public vhost serves BEFORE touching anything, so the check after
# the deploy can assert NO REGRESSION rather than an absolute. An absolute "must be
# 200" is a check that cries wolf on a host that was already broken for reasons this
# deploy did not cause; a baseline turns it into "it served before, so it must serve
# after", which is the thing we actually promise. Derive the hostname from the vhost's
# own server_name -- the filename is not the hostname and guessing is how you end up
# probing the default server and recording a green for a host you never tested.
SITE_HOST="$(grep -E '^\s*server_name\s' "$VHOST" | head -1 | awk '{print $2}' | tr -d ';')"
SITE_BEFORE="$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 --resolve "$SITE_HOST:443:127.0.0.1" "https://$SITE_HOST/" || true)"
echo "    baseline: $SITE_HOST currently serves HTTP $SITE_BEFORE"

echo "==> Building frontend..."
# ALWAYS `npm ci`, never "only if node_modules is missing". A present node_modules is
# not a correct one -- the sibling inber-party deploy hit exactly that: a four-month
# stale tree missing a dependency that had been in the lockfile the whole time, which
# fails the build looking like broken source rather than a stale sandbox.
FRONTEND_STAGING="$REPO_DIR/frontend/.dist-staging"
rm -rf "$FRONTEND_STAGING"
(
  cd frontend
  "$NPM" ci
  "$NPM" run build -- --outDir "$FRONTEND_STAGING" --emptyOutDir
)
[ -f "$FRONTEND_STAGING/index.html" ] || { echo "ERROR: frontend build produced no index.html"; exit 1; }
ASSET_COUNT="$(find "$FRONTEND_STAGING/assets" -type f 2>/dev/null | wc -l)"
[ "$ASSET_COUNT" -gt 0 ] || { echo "ERROR: frontend build produced no assets"; exit 1; }
echo "    frontend built: index.html + $ASSET_COUNT assets"

echo "==> Building $BINARY_NAME..."
# Build into a temp dir, NOT into the repo root. `go build -o multichat-server` writes
# to a path this repo used to TRACK -- a 13 MB ELF binary committed alongside the
# source -- so every deploy left the working tree dirty with a rebuilt binary, and the
# obvious way to "clean up" that dirty tree is to commit it. The binary is now
# untracked and ignored; building somewhere else means the deploy cannot resurrect it.
# CGO must stay ENABLED: multichat's store is mattn/go-sqlite3, a cgo driver. Built
# with CGO_ENABLED=0 it compiles, links and vets green, then fails at RUNTIME on the
# first sql.Open with "requires cgo to work". Do not "harden" this by turning cgo off.
BUILD_DIR="$(mktemp -d)"
trap 'rm -rf "$BUILD_DIR"' EXIT
BUILT_BIN="$BUILD_DIR/$BINARY_NAME"
CGO_ENABLED=1 "$GO" build -o "$BUILT_BIN" ./cmd/server
echo "    built: $(ls -lh "$BUILT_BIN" | awk '{print $5}')"

# The two companion binaries: the shell CLI the scheduler's jobs call, and the
# MCP server tool-store launches inside an agent session. Pure HTTP clients of
# the server above, no sqlite, so cgo is not needed -- but leaving it on costs
# nothing and keeps one build line shape.
echo "==> Building companion binaries..."
for companion in multichat-send multichat-mcp; do
  CGO_ENABLED=1 "$GO" build -o "$BUILD_DIR/$companion" "./cmd/$companion"
  echo "    built: $companion $(ls -lh "$BUILD_DIR/$companion" | awk '{print $5}')"
done

echo "==> Testing..."
"$GO" test ./... >/dev/null
echo "    tests green"

echo "==> Vetting..."
"$GO" vet ./...
echo "    vet clean"

# --- Backups. Timestamped, never a fixed filename. -----------------------------
# Verification necessarily runs AFTER the artifacts are installed -- that is the
# point of it -- so these are the only way back. A FIXED-filename backup is not a
# rollback point: the second run overwrites the good copy with the bad one, and the
# one thing you needed is gone.
mkdir -p "$BACKUP_DIR"
BACKUP_BIN="$BACKUP_DIR/$BINARY_NAME.$STAMP"
BACKUP_UNIT="$BACKUP_DIR/$UNIT_NAME.$STAMP"
BACKUP_FE="$BACKUP_DIR/frontend.$STAMP"
BACKUP_DB="$BACKUP_DIR/multichat.db.$STAMP"
# Written as if/then and not `[ -f x ] && cp ...`: in the one-liner form a FAILING cp
# is not the final command of the && list, so `set -e` exempts it and the backup
# silently does not happen -- and we would find that out at rollback time, which is
# the worst possible moment to learn a backup is missing.
if [ -f "$BINARY_PATH" ]; then cp -p "$BINARY_PATH" "$BACKUP_BIN"; echo "==> Backed up binary   -> $BACKUP_BIN"; fi
if [ -f "$LIVE_UNIT" ]; then cp -p "$LIVE_UNIT" "$BACKUP_UNIT"; echo "==> Backed up unit     -> $BACKUP_UNIT"; fi
if [ -d "$FRONTEND_ROOT" ]; then cp -a "$FRONTEND_ROOT" "$BACKUP_FE"; echo "==> Backed up frontend -> $BACKUP_FE"; fi
# Back the DB up through SQLite's own backup API, not `cp`: a plain copy of a live
# SQLite file can catch it mid-write and produce a corrupt snapshot.
sqlite3 "$DB_PATH" ".backup '$BACKUP_DB'"
echo "==> Backed up database -> $BACKUP_DB"

rollback() {
  echo "!!! Rolling back..."
  if [ -f "$BACKUP_BIN" ]; then cp -p "$BACKUP_BIN" "$BINARY_PATH.rb"; mv "$BINARY_PATH.rb" "$BINARY_PATH"; echo "    restored binary"; fi
  if [ -f "$BACKUP_UNIT" ]; then cp -p "$BACKUP_UNIT" "$LIVE_UNIT"; echo "    restored unit"; fi
  if [ -d "$BACKUP_FE" ]; then rm -rf "$FRONTEND_ROOT"; cp -a "$BACKUP_FE" "$FRONTEND_ROOT"; echo "    restored frontend"; fi
  systemctl --user daemon-reload
  systemctl --user restart "$UNIT_NAME" || true
  echo "!!! Rolled back to the previous binary/unit/frontend. Deploy FAILED."
  echo "!!! The database was NOT touched by the rollback; its pre-deploy snapshot is at $BACKUP_DB"
}

echo "==> Installing unit -> $LIVE_UNIT..."
mkdir -p "$(dirname "$LIVE_UNIT")"
if cmp -s "$REPO_UNIT" "$LIVE_UNIT" 2>/dev/null; then
  echo "    live unit already matches the template"
else
  echo "    live unit differs from the template (or is absent) -- installing"
  cp "$REPO_UNIT" "$LIVE_UNIT"
fi
systemctl --user daemon-reload

echo "==> Installing frontend -> $FRONTEND_ROOT..."
# REPLACE the directory rather than copying into it. vite emits content-hashed
# filenames, so copying over the top only ever ADDS: the live root had accumulated
# ten stale index-*.js/css bundles from previous hand-copies, none of them reachable,
# all of them served if anything ever asked. Swap the whole directory instead.
rm -rf "$FRONTEND_ROOT.old.$STAMP"
mv "$FRONTEND_ROOT" "$FRONTEND_ROOT.old.$STAMP"
mv "$FRONTEND_STAGING" "$FRONTEND_ROOT"
rm -rf "$FRONTEND_ROOT.old.$STAMP"

# Set the modes EXPLICITLY instead of inheriting whatever umask the caller happened
# to have. nginx serves these as www-data, so they must be world-readable; a deploy
# run from a shell (or a cron job) with a restrictive umask would otherwise install a
# site nginx cannot read and 403 the whole domain, with every other check still green.
# The deploy must not depend on the ambient umask to produce a servable site.
chmod 755 "$FRONTEND_ROOT"
find "$FRONTEND_ROOT" -type d -exec chmod 755 {} +
find "$FRONTEND_ROOT" -type f -exec chmod 644 {} +
echo "    frontend installed (replaced, not merged; 755/644)"

echo "==> Installing companion binaries -> $(dirname "$BINARY_PATH")..."
for companion in multichat-send multichat-mcp; do
  cp "$BUILD_DIR/$companion" "$(dirname "$BINARY_PATH")/$companion.new"
  chmod 755 "$(dirname "$BINARY_PATH")/$companion.new"
  mv "$(dirname "$BINARY_PATH")/$companion.new" "$(dirname "$BINARY_PATH")/$companion"
  echo "    installed $companion"
done

echo "==> Installing binary -> $BINARY_PATH..."
# Stage next to the target and rename into place. A plain `cp` over the live binary
# fails with ETXTBSY because the running service still holds it open for execution;
# rename(2) is atomic and swaps the inode.
cp "$BUILT_BIN" "$BINARY_PATH.new"
chmod 755 "$BINARY_PATH.new"
mv "$BINARY_PATH.new" "$BINARY_PATH"

echo "==> Restarting $UNIT_NAME..."
systemctl --user reset-failed "$UNIT_NAME" 2>/dev/null || true
systemctl --user restart "$UNIT_NAME"

# Poll for readiness; do not sleep and hope. `systemctl is-active` is not readiness:
# it says a process exists, not that the binary answers.
echo "==> Waiting for :$PORT to answer..."
READY_TIMEOUT=45
for i in $(seq 1 "$READY_TIMEOUT"); do
  if curl -fsS -o /dev/null --max-time 5 "http://127.0.0.1:$PORT/api/health"; then
    echo "    ready after ${i}s"
    break
  fi
  if ! systemctl --user is-active --quiet "$UNIT_NAME"; then
    echo "ERROR: $UNIT_NAME died while starting up"
    journalctl --user -u "$UNIT_NAME" -n 30 --no-pager || true
    rollback
    exit 1
  fi
  if [ "$i" -eq "$READY_TIMEOUT" ]; then
    echo "ERROR: $UNIT_NAME still not answering :$PORT after ${READY_TIMEOUT}s"
    journalctl --user -u "$UNIT_NAME" -n 30 --no-pager || true
    rollback
    exit 1
  fi
  sleep 1
done

echo "==> Smoke test..."
fail() { echo "ERROR: $1"; journalctl --user -u "$UNIT_NAME" -n 30 --no-pager || true; rollback; exit 1; }

# 1. AUTH IS ON. Assert the REJECTION, not the config -- reading MULTICHAT_TOKEN back
# out of a file proves only that the file says so, and this is the property that
# actually matters: with an empty token the auth branch is skipped entirely and every
# route below opens to the internet. An unauthenticated request MUST be refused.
UNAUTH_CODE="$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 "http://127.0.0.1:$PORT/api/tags")"
[ "$UNAUTH_CODE" = "401" ] || fail "an UNAUTHENTICATED request to /api/tags returned HTTP $UNAUTH_CODE, not 401 -- authentication is OFF on a service exposed at chat.kayushkin.com. Rolling back."
echo "    auth is ON (unauthenticated /api/tags -> 401)"

# 2. THE REAL DATABASE IS OPEN. The failure this guards is not a crash: point the
# binary at the wrong path and SQLite creates an empty DB, the schema migrates, every
# route answers 200, and the service looks perfectly healthy with every contact and
# tag gone. So assert the DATA, not the status code -- the rows we counted before the
# deploy must still be there afterwards.
TOKEN="$(grep -E '^MULTICHAT_TOKEN=' "$ENV_FILE" | head -1 | cut -d= -f2-)"
TAGS="$(curl -fsS --max-time 10 -H "Authorization: Bearer $TOKEN" "http://127.0.0.1:$PORT/api/tags")"
TAGS_COUNT="$(printf '%s' "$TAGS" | jq 'if type=="array" then length else (.tags//[]|length) end' 2>/dev/null || echo 0)"
[ "$TAGS_COUNT" -ge "$ROWS_BEFORE" ] || fail "/api/tags returned $TAGS_COUNT tags but there were $ROWS_BEFORE before the deploy -- the service is answering out of a DIFFERENT (probably empty) database. Rolling back."
echo "    real database is open ($TAGS_COUNT tags, $ROWS_BEFORE before the deploy)"

# 3. NGINX CAN ACTUALLY SERVE THE FRONTEND. Ask the vhost, over TLS, the way a
# browser does -- do not check that index.html exists on disk. "The file is there" is
# a proxy for the property we care about and it is the proxy that fooled the first run
# of this script: the files were all present and perfectly correct, mode 600, and
# nginx (www-data) could not read one of them, so the site served 403 while a
# `[ -f index.html ]` check passed happily. Assert the thing itself, from the other
# side. --resolve so this cannot depend on DNS.
if [ "$SITE_BEFORE" = "200" ]; then
  SITE_AFTER="$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 --resolve "$SITE_HOST:443:127.0.0.1" "https://$SITE_HOST/" || true)"
  [ "$SITE_AFTER" = "200" ] || fail "$SITE_HOST served 200 before this deploy and serves HTTP $SITE_AFTER now -- nginx cannot serve the frontend that was just installed (check permissions: it runs as www-data). Rolling back."
  echo "    $SITE_HOST still serves 200 (was 200 before the deploy)"
else
  # Do not assert an absolute 200 on a host that was not serving one to begin with --
  # that fails the deploy for a pre-existing condition it did not cause. Report the
  # no-regression comparison honestly instead.
  SITE_AFTER="$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 --resolve "$SITE_HOST:443:127.0.0.1" "https://$SITE_HOST/" || true)"
  echo "    $SITE_HOST: $SITE_BEFORE before -> $SITE_AFTER after (was not serving 200 before this deploy; not asserting one)"
fi

# is-active is the last word: a unit that is up now but scheduled to restart is not a
# successful deploy.
systemctl --user is-active --quiet "$UNIT_NAME" || fail "$UNIT_NAME is not active"
RESTARTS="$(systemctl --user show "$UNIT_NAME" -p NRestarts --value)"
[ "$RESTARTS" = "0" ] || fail "$UNIT_NAME has restarted $RESTARTS time(s) since the deploy -- it is crash-looping, not running"
echo "    $UNIT_NAME active, NRestarts=$RESTARTS"
echo "    smoke test OK"

# The vhost carries the /api/ and WebSocket-upgrade rules the app needs to function.
# It is part of the deploy, not something to hand-edit in /etc/nginx.
echo "==> Installing nginx vhost..."
./deploy/nginx/install.sh

echo "==> Done. Deployed $(git rev-parse --short HEAD)."

# Last act: write this deploy to repo-store's ledger, so the next agent sees what is live.
( cd "$(dirname "$0")" && "$HOME/bin/deploy-gate" record )
