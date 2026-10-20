#!/usr/bin/env bash
# Runs multichat's short-name pass once: POST /api/short-names/assign, which
# names every chat with no current name in short-name-store. Run from a
# scheduler shell job; pass --dry-run to see the proposals without writing.
# Names are permanent once written.
#
# Needs MULTICHAT_URL, AUTH_STORE_URL and AUTH_STORE_TOKEN, which the scheduler
# unit gets from scheduler.service.d/multichat-send.conf. multichat's token is
# resolved from auth-store provider "multichat" on each run. Both tokens reach
# curl on stdin (-K -), never on a command line another process could read.
set -euo pipefail
: "${MULTICHAT_URL:?MULTICHAT_URL is not set}"
: "${AUTH_STORE_URL:?AUTH_STORE_URL is not set}"
: "${AUTH_STORE_TOKEN:?AUTH_STORE_TOKEN is not set}"
query=""
case "${1:-}" in
  "") ;;
  --dry-run) query="?dry_run=true" ;;
  *) echo "usage: assign-short-names.sh [--dry-run]" >&2; exit 2 ;;
esac

resolved=$(printf 'header = "Authorization: Bearer %s"\n' "$AUTH_STORE_TOKEN" |
  curl -sfS -K - -H 'X-Auth-App: multichat-short-namer' -H 'X-Auth-Reason: assign short names' \
    "$AUTH_STORE_URL/api/resolve/multichat")
multichat_token=$(jq -er 'select(.auth_type == "api_key") | .api_key' <<<"$resolved")

# A run asks a model about 40 rooms at a time; the first run over every chat
# takes several minutes.
answer=$(printf 'header = "Authorization: Bearer %s"\n' "$multichat_token" |
  curl -sS -K - --max-time 1700 -X POST -w '\n%{http_code}' "$MULTICHAT_URL/api/short-names/assign$query")
status=${answer##*$'\n'}
body=${answer%$'\n'*}
jq '{dry_run, model_role, models_used, candidate_count, named_count, refused_count, skipped_count,
     server_short_names, named: [.named[]? | "\(.path) <- \(.room_name) (\(.platform), \(.message_count))"],
     refused: [.refused[]? | "\(.room_name) (\(.platform), \(.message_count)): \(.proposed_path) refused: \(.why)"]}' \
  <<<"$(jq '.result // .' <<<"$body")"
if [[ $status != 200 ]]; then
  echo "POST /api/short-names/assign answered $status: $(jq -r '.error // .' <<<"$body")" >&2
  exit 1
fi
