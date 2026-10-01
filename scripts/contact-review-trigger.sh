#!/usr/bin/env bash
# Starts the contact review agent job when people-store's review queue holds
# handles and no review session holds a claim. Run from a scheduler shell job:
#   contact-review-trigger.sh <agent job id> <scheduler url> <people-store url>
# The queue (GET /reviews/pending) is the handles the operator talks with that
# nobody has reviewed; its activity comes from the hourly people-store
# multichat import. people-store binds 127.0.0.1 and asks for no token.
set -euo pipefail
usage='usage: contact-review-trigger.sh <agent job id> <scheduler url> <people-store url>'
agent_job_id=${1:?$usage}
scheduler_url=${2:?$usage}
people_store_url=${3:?$usage}

queue=$(curl -sfS "$people_store_url/reviews/pending?limit=1")
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
