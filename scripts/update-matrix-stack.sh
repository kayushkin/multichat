#!/usr/bin/env bash
# Installs new releases of Synapse and the mautrix bridges, one run a week from
# a scheduler shell job.
#
#   1. Pulls every compose service whose image is a `:latest` tag. A pinned tag
#      (the Python mautrix-telegram) and a service in a profile (retired) are
#      left alone.
#   2. Stops here when no image changed.
#   3. For each service whose image changed: tags the image it runs now as
#      `<image>:before-<stamp>`, stops it, and archives every volume and bind
#      mount it has, less media and logs, into $MATRIX_BACKUP_DIR/matrix-update-<stamp>/. Synapse and
#      the bridges migrate their databases forward and cannot go back, so the
#      archive is the only way back.
#   4. `docker compose up -d`, then waits up to five minutes for every changed
#      container to run (and be healthy, where it has a health check) and for
#      every bridge login that was CONNECTED before to be CONNECTED again, and
#      every other login to still exist, as scripts/bridge-status.sh reports it.
#   5. Keeps the newest three backups and their `before-` tags, and removes older ones.
#
# Any failure files a noteboard todo naming what failed and how to roll back,
# and exits non-zero so the scheduler run shows `error`.
#
# --dry-run pulls and reports which services have a new image, and touches no
# running container.
#
# Needs MULTICHAT_COMPOSE_DIR, MATRIX_BACKUP_DIR and NOTEBOARD_URL, and
# membership of the docker group.
set -euo pipefail
: "${MULTICHAT_COMPOSE_DIR:?MULTICHAT_COMPOSE_DIR is not set}"
: "${MATRIX_BACKUP_DIR:?MATRIX_BACKUP_DIR is not set}"
: "${NOTEBOARD_URL:?NOTEBOARD_URL is not set}"
dry_run=no
case "${1:-}" in
  "") ;;
  --dry-run) dry_run=yes ;;
  *) echo "usage: update-matrix-stack.sh [--dry-run]" >&2; exit 2 ;;
esac

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
stamp=$(date -u +%Y-%m-%dT%H%MZ)
backup_dir="$MATRIX_BACKUP_DIR/matrix-update-$stamp"
keep_backups=3
log=$(mktemp)
trap 'rm -f "$log"' EXIT
exec > >(tee -a "$log") 2>&1

compose() { docker compose --project-directory "$MULTICHAT_COMPOSE_DIR" "$@"; }
container_of() { compose ps --all --format json "$1" | jq -rs '.[0].Name // empty'; }

changed_services=()
declare -A old_image_id=()

file_todo() {
  local reason=$1 rollback=""
  for service in "${changed_services[@]}"; do
    local image
    image=$(compose config --format json | jq -r --arg s "$service" '.services[$s].image')
    rollback+="
- \`$service\`: \`docker compose stop $service\`; restore each archive \`$backup_dir/$service-*.tgz\` (named for the volume, or the bind path with / as _; inside, the mount sits under its own base name) over the mount, as root with \`tar xzpf\`; \`docker tag ${image%:*}:before-$stamp $image\`; \`docker compose up -d $service\`."
  done
  local body
  body="The weekly Synapse and bridge update (\`scripts/update-matrix-stack.sh\` in repo multichat) failed on $stamp: $reason

Changed services: ${changed_services[*]:-none}. Backup: \`$backup_dir\`.

Roll back, in \`$MULTICHAT_COMPOSE_DIR\`:$rollback

Run output:
\`\`\`
$(tail -c 6000 "$log")
\`\`\`"
  jq -n --arg title "Matrix stack update failed ($stamp): $reason" --arg body "$body" \
    '{type: "todo", title: $title, body: $body, tags: ["multichat", "matrix-stack-update"], priority: 1}' |
    curl -sfS -X POST "$NOTEBOARD_URL/api/items" -H 'Content-Type: application/json' -d @- | jq -r '"filed noteboard todo \(.id)"'
}

fail() {
  failing=yes
  echo "FAIL: $1"
  file_todo "$1"
  exit 1
}
trap 'status=$?; if (( status != 0 )) && [[ ${failing:-} != yes ]]; then failing=yes; file_todo "line $LINENO exited $status"; fi' ERR

before_status=$("$script_dir/bridge-status.sh")
logins_before=$(jq -c '[.[] | .service as $s | .logins[]? | {service: $s, id, state}]' <<<"$before_status")
echo "logins before: $(jq -r 'map("\(.service)/\(.id) \(.state)") | join(", ")' <<<"$logins_before")"

mapfile -t candidate_services < <(compose config --format json | jq -r '
  .services | to_entries[]
  | select((.value.profiles // []) == [])
  | select(.value.image | test(":latest$") or (test(":") | not))
  | .key')
echo "following :latest: ${candidate_services[*]}"

for service in "${candidate_services[@]}"; do
  container=$(container_of "$service")
  [[ -n $container ]] || fail "$service has no container"
  old_image_id[$service]=$(docker inspect -f '{{.Image}}' "$container")
done

compose pull --quiet "${candidate_services[@]}" || fail "docker compose pull failed"

for service in "${candidate_services[@]}"; do
  image=$(compose config --format json | jq -r --arg s "$service" '.services[$s].image')
  new_id=$(docker image inspect -f '{{.Id}}' "$image")
  if [[ $new_id != "${old_image_id[$service]}" ]]; then
    changed_services+=("$service")
    echo "$service: new image ${new_id:7:12} (was ${old_image_id[$service]:7:12})"
  fi
done

if (( ${#changed_services[@]} == 0 )); then
  echo "nothing new; no service touched"
  exit 0
fi
if [[ $dry_run == yes ]]; then
  echo "dry run: would back up and restart ${changed_services[*]}"
  exit 0
fi

mkdir -p "$backup_dir"
for service in "${changed_services[@]}"; do
  container=$(container_of "$service")
  image=$(compose config --format json | jq -r --arg s "$service" '.services[$s].image')
  docker tag "${old_image_id[$service]}" "${image%:*}:before-$stamp"
  mounts=$(docker inspect -f '{{json .Mounts}}' "$container")
  compose stop "$service"
  # The old image archives its own data: every image here has tar, and it needs
  # nothing pulled. Media and logs are left out: no upgrade migrates them, and
  # Synapse's 4 GB media store would keep it down for minutes, while mautrix-discord
  # (missed-message backfill off) drops what Discord sends it while Synapse is down:
  # the first run, with media in, kept Synapse down four minutes and lost three.
  # Fields split on \x1f, not a tab: bash merges runs of tabs, so a bind
  # mount's empty name would shift its source into the name.
  while IFS=$'\x1f' read -r type name source; do
    if [[ $type == volume ]]; then from=$name; label=$name; else from=$source; label=${source//\//_}; fi
    archive="$service-$label.tgz"
    docker run --rm --user 0 --entrypoint tar \
      -v "$from:/source/${from##*/}:ro" -v "$backup_dir:/backup" "${old_image_id[$service]}" \
      czpf "/backup/$archive" --exclude=media_store --exclude=logs --exclude='*.log' --exclude='*.log.*' -C /source .
    echo "$service: archived $from to $archive ($(du -h "$backup_dir/$archive" | cut -f1))"
  done < <(jq -r '.[] | [.Type, (.Name // ""), .Source] | join("\u001f")' <<<"$mounts")
done

compose up -d --quiet-pull

deadline=$((SECONDS + 300))
while :; do
  problems=()
  for service in "${changed_services[@]}"; do
    container=$(container_of "$service")
    state=$(docker inspect -f '{{.State.Status}}{{if .State.Health}}/{{.State.Health.Status}}{{end}}' "$container")
    [[ $state == running || $state == running/healthy ]] || problems+=("$service is $state")
  done
  if after_status=$("$script_dir/bridge-status.sh" 2>&1); then
    # A login that was CONNECTED must be CONNECTED again; any other login must
    # at least still exist (mautrix-signal's first start on a new release once
    # loaded its login before migrating its own tables, and dropped it until
    # the next restart).
    missing=$(jq -r --argjson before "$logins_before" '
      [.[] | .service as $s | .logins[]? | {service: $s, id, state}] as $after
      | $before[]
      | . as $login
      | ($after | map(select(.service == $login.service and .id == $login.id)) | .[0]) as $now
      | if $now == null then "\(.service)/\(.id) is gone"
        elif .state == "CONNECTED" and $now.state != "CONNECTED" then "\(.service)/\(.id) is \($now.state), was CONNECTED"
        else empty end' <<<"$after_status")
    [[ -z $missing ]] || mapfile -t -O "${#problems[@]}" problems <<<"$missing"
  else
    problems+=("bridge-status.sh failed: $after_status")
  fi
  (( ${#problems[@]} == 0 )) && break
  (( SECONDS < deadline )) || fail "after five minutes: $(IFS='; '; echo "${problems[*]}")"
  sleep 15
done

for service in "${changed_services[@]}"; do
  container=$(container_of "$service")
  version=$(docker inspect -f '{{index .Config.Labels "org.opencontainers.image.version"}}' "$(docker inspect -f '{{.Image}}' "$container")")
  echo "updated $service${version:+ to $version}"
done

mapfile -t old_backups < <(find "$MATRIX_BACKUP_DIR" -maxdepth 1 -type d -name 'matrix-update-*' | sort | head -n -"$keep_backups")
for old in "${old_backups[@]}"; do
  old_stamp=${old##*/matrix-update-}
  rm -rf "$old" # root-owned archives, but the directory is ours
  docker images --format '{{.Repository}}:{{.Tag}}' | awk -v tag=":before-$old_stamp" 'index($0, tag)' | xargs -r docker rmi
  echo "removed backup and before- tags of $old_stamp"
done
echo "ok: updated ${changed_services[*]}; backup $backup_dir"
