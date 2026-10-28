#!/usr/bin/env bash
set -euo pipefail

# One shared gate decides whether this tree may be deployed (main clone, default
# branch, clean, pushed, not behind, and the same for every tree the build reads).
# It lives in healthcheck/scripts/deploy-gate.sh. Do not inline or copy it.
( cd "$(dirname "$0")" && "$HOME/bin/deploy-gate" check )

# Builds multichat, installs the binaries and the systemd user unit, restarts the
# service, and refuses to report success unless the
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
# THE ARTIFACTS, and where each actually goes:
#   1. the Go binaries -> ~/bin/multichat-server, multichat-send, multichat-mcp
#   2. the unit        -> ~/.config/systemd/user/...    (installed from deploy/)
#
# multichat HAS NO WEB SITE OF ITS OWN. Its web UI is bridge-ui's Messages pages in
# dash, which reach this API through dash's proxy. chat.kayushkin.com, the old site
# (a React app plus /api and /ws on the internet), was switched off on 2026-09-28;
# the step "Retiring the old public site" below removes its nginx site wherever it
# is still installed, and does nothing once it is gone.

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

# go exists ONLY under mise on this host (there is no /usr/bin/node at
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

echo "==> Unit declares:"
echo "    ExecStart        = $BINARY_PATH"
echo "    WorkingDirectory = $WORK_DIR"
echo "    EnvironmentFile  = $ENV_FILE"
echo "    PORT             = $PORT"
echo "    MULTICHAT_DB     = $DB_PATH"

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
  # every file the rest of the script creates. When multichat still had a web site,
  # the first run of this script did exactly that: nginx (www-data) could not read the
  # frontend it installed, and the site served 403 while this printed "smoke test OK".
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
# MULTICHAT_TOKEN does not lock the door, it REMOVES it -- and this API serves
# contacts, conversations and message history, sends messages as the operator, and
# spends the OpenAI key. It is no longer on the internet directly, but dash forwards
# part of it there and every local process can reach :8402. A deploy that quietly
# wrote an empty token would open all of it and print "smoke test OK". So: check,
# and say which one is missing.
echo "==> Checking secrets in $ENV_FILE..."
[ -f "$ENV_FILE" ] || { echo "ERROR: EnvironmentFile $ENV_FILE does not exist -- systemd cannot load it and the service will crash-loop"; exit 1; }
for key in $REQUIRED_SECRETS; do
  value="$(grep -E "^$key=" "$ENV_FILE" | head -1 | cut -d= -f2- || true)"
  [ -n "$value" ] || { echo "ERROR: $key is missing or EMPTY in $ENV_FILE. Refusing to deploy: an empty MULTICHAT_TOKEN disables authentication."; exit 1; }
  echo "    $key present (${#value} chars)"
done

# Reacting with a custom Discord emoji the bridge has not seen writes the emoji
# into the bridge's own database (internal/discordemoji). The bridge container
# chowns its data folder to its UID/GID on every start, so the service user can
# write there only when ~/multichat/docker-compose.yaml runs mautrix-discord as
# this user (UID and GID in its environment). Say so here rather than let the
# first custom reaction fail.
DISCORD_DB_PATH="$(env_field DISCORD_DB)"
if [ -n "$DISCORD_DB_PATH" ]; then
  echo "==> Checking DISCORD_DB is writable (custom emoji reactions add rows to it)..."
  for f in "$DISCORD_DB_PATH" "$DISCORD_DB_PATH-wal" "$DISCORD_DB_PATH-shm" "$(dirname "$DISCORD_DB_PATH")"; do
    [ ! -e "$f" ] || [ -w "$f" ] || { echo "ERROR: $f is not writable by $(id -un). Run mautrix-discord with UID=$(id -u) and GID=$(id -g) in ~/multichat/docker-compose.yaml and recreate it (docker compose up -d mautrix-discord)."; exit 1; }
  done
  echo "    writable"
fi

echo "==> Preflight..."
# A WorkingDirectory that does not exist fails the start with an opaque `200/CHDIR`,
# and with Restart= set that becomes a silent crash loop -- three services on this
# box ran up 1.8 MILLION failed starts that way. Check it where we can name it.
[ -d "$WORK_DIR" ] || { echo "ERROR: WorkingDirectory $WORK_DIR does not exist -- systemd cannot chdir and will crash-loop"; exit 1; }
# The live database must already be there. This deploy must never be the thing that
# creates it: an empty DB is not an error to SQLite, it is a fresh start, and the
# service would come up green with every contact and tag gone.
[ -f "$DB_PATH" ] || { echo "ERROR: database $DB_PATH does not exist. Refusing to deploy -- multichat would silently create an empty one and come up looking healthy."; exit 1; }
ROWS_BEFORE="$(sqlite3 "$DB_PATH" 'SELECT COUNT(*) FROM tags;' 2>/dev/null || echo 0)"
echo "    WorkingDirectory and database exist ($ROWS_BEFORE tags before deploy)"

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

# Checked BEFORE the install, for the same reason the smokes are: an unidentifiable
# binary compiles perfectly and reads clean in the log, so installing first would put
# it in front of live sessions and only then tell us it cannot be traced to a commit.
echo "==> Checking provenance..."
buildinfo="$("$GO" version -m "$BUILT_BIN")"
vcs_revision="$(printf '%s\n' "$buildinfo" | awk -F= '$1 ~ /[[:space:]]vcs\.revision$/ {print $2}')"
vcs_modified="$(printf '%s\n' "$buildinfo" | awk -F= '$1 ~ /[[:space:]]vcs\.modified$/ {print $2}')"
if [ -z "$vcs_revision" ]; then
    echo "    REFUSING TO INSTALL: this binary carries no vcs.revision, so nothing can tie" >&2
    echo "    it back to a commit. 'go build' writes no VCS stamp when it cannot find a .git" >&2
    echo "    DIRECTORY, and it does not fail when that happens -- not even with -buildvcs=true." >&2
    echo "    The usual cause is building from a git worktree, whose .git is a pointer file." >&2
    echo "    Build from a real clone or checkout instead." >&2
    exit 1
fi
echo "    vcs.revision=$vcs_revision"
if [ "$vcs_modified" = "true" ]; then
    echo "    WARNING: built from a DIRTY tree (vcs.modified=true). $vcs_revision names the" >&2
    echo "    commit this binary was built NEAR, not the source it was built FROM, and that" >&2
    echo "    source is not recoverable from any commit. Commit first for a reproducible build." >&2
fi

echo "==> Vetting..."
"$GO" vet ./...
echo "    vet clean"

# A misspelled owned variable (MULTICHAT_DB_PATH, MATRIX_TOKENS, ...), a PORT
# that is not a number, or NATS_URL without the Matrix client
# stops the new binary at boot. Ask before anything is installed: build the
# registry from the running service's own environment. The test prints a
# verdict, never a value -- that environment holds the Matrix token and the
# OpenAI key. It checks the environment the unit runs with NOW, so a deploy
# that also changes the unit's Environment= lines is checked against the old ones.
echo "==> Checking the running service's environment against the declared settings..."
LIVE_PID="$(systemctl --user show -p MainPID --value "$UNIT_NAME")"
if [ -n "$LIVE_PID" ] && [ "$LIVE_PID" != "0" ]; then
  "$GO" test -count=1 -run '^TestTheLiveProcessEnvironmentBuildsARegistry$' ./internal/config -args -live-environment-file="/proc/$LIVE_PID/environ" \
    || { echo "ERROR: the running $UNIT_NAME's environment would stop the new binary at boot -- not installing"; exit 1; }
  echo "    the new binary starts in the running service's environment"
else
  echo "    $UNIT_NAME is not running, so there is no environment to check"
fi

# --- Backups. Timestamped, never a fixed filename. -----------------------------
# Verification necessarily runs AFTER the artifacts are installed -- that is the
# point of it -- so these are the only way back. A FIXED-filename backup is not a
# rollback point: the second run overwrites the good copy with the bad one, and the
# one thing you needed is gone.
mkdir -p "$BACKUP_DIR"
BACKUP_BIN="$BACKUP_DIR/$BINARY_NAME.$STAMP"
BACKUP_UNIT="$BACKUP_DIR/$UNIT_NAME.$STAMP"
BACKUP_DB="$BACKUP_DIR/multichat.db.$STAMP"
# Written as if/then and not `[ -f x ] && cp ...`: in the one-liner form a FAILING cp
# is not the final command of the && list, so `set -e` exempts it and the backup
# silently does not happen -- and we would find that out at rollback time, which is
# the worst possible moment to learn a backup is missing.
if [ -f "$BINARY_PATH" ]; then cp -p "$BINARY_PATH" "$BACKUP_BIN"; echo "==> Backed up binary   -> $BACKUP_BIN"; fi
if [ -f "$LIVE_UNIT" ]; then cp -p "$LIVE_UNIT" "$BACKUP_UNIT"; echo "==> Backed up unit     -> $BACKUP_UNIT"; fi
# Back the DB up through SQLite's own backup API, not `cp`: a plain copy of a live
# SQLite file can catch it mid-write and produce a corrupt snapshot.
sqlite3 "$DB_PATH" ".backup '$BACKUP_DB'"
echo "==> Backed up database -> $BACKUP_DB"

rollback() {
  echo "!!! Rolling back..."
  if [ -f "$BACKUP_BIN" ]; then cp -p "$BACKUP_BIN" "$BINARY_PATH.rb"; mv "$BINARY_PATH.rb" "$BINARY_PATH"; echo "    restored binary"; fi
  if [ -f "$BACKUP_UNIT" ]; then cp -p "$BACKUP_UNIT" "$LIVE_UNIT"; echo "    restored unit"; fi
  systemctl --user daemon-reload
  systemctl --user restart "$UNIT_NAME" || true
  echo "!!! Rolled back to the previous binary and unit. Deploy FAILED."
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
[ "$UNAUTH_CODE" = "401" ] || fail "an UNAUTHENTICATED request to /api/tags returned HTTP $UNAUTH_CODE, not 401 -- authentication is OFF. Rolling back."
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

# is-active is the last word: a unit that is up now but scheduled to restart is not a
# successful deploy.
systemctl --user is-active --quiet "$UNIT_NAME" || fail "$UNIT_NAME is not active"
RESTARTS="$(systemctl --user show "$UNIT_NAME" -p NRestarts --value)"
[ "$RESTARTS" = "0" ] || fail "$UNIT_NAME has restarted $RESTARTS time(s) since the deploy -- it is crash-looping, not running"
echo "    $UNIT_NAME active, NRestarts=$RESTARTS"
echo "    smoke test OK"

# --- Retiring the old public site ----------------------------------------------
# chat.kayushkin.com served the old web app and put /api and /ws on the internet.
# It was switched off on 2026-09-28, when multichat's pages moved into dash. Remove
# its nginx site wherever it is still installed -- backed up first, config tested
# before the reload, the files put back if the test fails -- and remove the built
# app nginx served from. Once both are gone this step only says so.
RETIRED_SITE="chat.kayushkin.com"
RETIRED_FRONTEND_ROOT="$HOME/multichat-frontend"
echo "==> Retiring the old public site $RETIRED_SITE..."
if [ -e "/etc/nginx/sites-enabled/$RETIRED_SITE" ] || [ -e "/etc/nginx/sites-available/$RETIRED_SITE" ]; then
  SITE_BACKUP="$BACKUP_DIR/nginx-$RETIRED_SITE.$STAMP"
  mkdir -p "$SITE_BACKUP"
  if [ -e "/etc/nginx/sites-available/$RETIRED_SITE" ]; then sudo cat "/etc/nginx/sites-available/$RETIRED_SITE" > "$SITE_BACKUP/available"; fi
  sudo rm -f "/etc/nginx/sites-enabled/$RETIRED_SITE" "/etc/nginx/sites-available/$RETIRED_SITE"
  if ! sudo nginx -t 2>/dev/null; then
    echo "ERROR: nginx -t fails without $RETIRED_SITE -- putting it back and not reloading"
    sudo nginx -t || true
    if [ -f "$SITE_BACKUP/available" ]; then
      sudo install -m644 "$SITE_BACKUP/available" "/etc/nginx/sites-available/$RETIRED_SITE"
      sudo ln -sf "/etc/nginx/sites-available/$RETIRED_SITE" "/etc/nginx/sites-enabled/$RETIRED_SITE"
    fi
    exit 1
  fi
  sudo systemctl reload nginx
  echo "    removed the nginx site (backup: $SITE_BACKUP) and reloaded nginx"
else
  echo "    no nginx site for $RETIRED_SITE is installed"
fi
if [ -d "$RETIRED_FRONTEND_ROOT" ]; then
  mv "$RETIRED_FRONTEND_ROOT" "$BACKUP_DIR/multichat-frontend.$STAMP"
  echo "    moved the old app out of $RETIRED_FRONTEND_ROOT (to $BACKUP_DIR/multichat-frontend.$STAMP)"
fi
# The property is that no nginx site claims the name any more; ask the config
# nginx actually loads rather than a URL (with the site gone, nginx answers the name
# from its default site, which may well have an /api/health of its own).
if sudo nginx -T 2>/dev/null | grep -Eq "^[[:space:]]*server_name[^;]*[[:space:]]$RETIRED_SITE[[:space:];]"; then
  echo "ERROR: an nginx site still claims $RETIRED_SITE"
  exit 1
fi
echo "    no nginx site claims $RETIRED_SITE"

echo "==> Done. Deployed $(git rev-parse --short HEAD)."

# Last act: write this deploy to repo-store's ledger, so the next agent sees what is live.
( cd "$(dirname "$0")" && "$HOME/bin/deploy-gate" record )
