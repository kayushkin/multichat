#!/usr/bin/env bash
# Starts the contact review agent job when people are waiting for review and
# no review session is already on them. Run from a scheduler shell job:
#   contact-review-trigger.sh <agent job id> <scheduler url>
# MULTICHAT_URL, AUTH_STORE_URL and AUTH_STORE_TOKEN come from the scheduler
# unit's multichat-send drop-in.
set -euo pipefail
agent_job_id=${1:?usage: contact-review-trigger.sh <agent job id> <scheduler url>}
scheduler_url=${2:?usage: contact-review-trigger.sh <agent job id> <scheduler url>}
: "${MULTICHAT_URL:?MULTICHAT_URL is not set}" "${AUTH_STORE_URL:?AUTH_STORE_URL is not set}" "${AUTH_STORE_TOKEN:?AUTH_STORE_TOKEN is not set}"

token=$(curl -sfS -H "Authorization: Bearer $AUTH_STORE_TOKEN" -H 'X-Auth-App: multichat-contact-review-trigger' -H 'X-Auth-Reason: read the contact review queue' \
  "$AUTH_STORE_URL/api/resolve/multichat" | jq -er .api_key)
queue=$(curl -sfS -H "Authorization: Bearer $token" "$MULTICHAT_URL/api/contacts/reviews/pending?limit=1")
waiting=$(jq -er .total <<<"$queue")
claims=$(jq -er .active_claims <<<"$queue")
if (( waiting == 0 )); then
  echo "nobody waiting for review"
  exit 0
fi
if (( claims > 0 )); then
  echo "$waiting waiting; a review session holds $claims claims"
  exit 0
fi
# A session takes a minute or two to claim its batch; don't start a second one meanwhile.
last_started=$(curl -sfS "$scheduler_url/api/jobs/$agent_job_id/runs" | jq -r '.[0].started_at // empty')
if [[ -n $last_started ]] && (( $(date +%s) - $(date -d "$last_started" +%s) < 900 )); then
  echo "$waiting waiting; job $agent_job_id started at $last_started and may not have claimed yet"
  exit 0
fi
curl -sfS -X POST "$scheduler_url/api/jobs/$agent_job_id/run" >/dev/null
echo "$waiting waiting; started job $agent_job_id"
