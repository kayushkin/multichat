#!/usr/bin/env bash
# Reports whether each mautrix bridge in the Synapse compose stack is running
# and logged in, by asking the bridge itself.
#
#   bridge-status.sh           one JSON object per bridge, as a JSON array
#   bridge-status.sh --check   healthcheck `command` contract: prints "ok" and
#                              exits 0, or prints one FAIL line per fault and exits 1
#
# A bridge is a compose service whose /data holds a registration.yaml; a service
# in a compose profile (retired, started only by hand) is not checked. --check
# fails when a bridge container is not running, has no login, or has a login in
# any state but CONNECTED. A bridge kept on purpose without a login is a fault
# here: move it to the `retired` profile instead.
#
# The question is asked inside each container, so the provisioning secret never
# leaves it:
#   - bridgev2 bridges (whatsapp, signal, meta, slack): GET /_matrix/provision/v3/whoami
#     answers each login's state (CONNECTED, BAD_CREDENTIALS, TRANSIENT_DISCONNECT…).
#   - mautrix-discord (legacy Go): GET /_matrix/provision/v1/ping answers
#     logged_in and connected.
#   - the pinned Python mautrix-telegram: its provisioning API answers 500 on
#     every call (an aiohttp incompatibility in that build), so the login is read
#     from its database: a `user` row with a tgid is logged in. Connection state
#     is not available there.
# The Matrix user asked about is the one the bridge's config gives admin.
#
# Needs MULTICHAT_COMPOSE_DIR (the directory with docker-compose.yaml) and
# membership of the docker group.
set -euo pipefail
: "${MULTICHAT_COMPOSE_DIR:?MULTICHAT_COMPOSE_DIR is not set}"

mode=json
case "${1:-}" in
  "") ;;
  --check) mode=check ;;
  *) echo "usage: bridge-status.sh [--check]" >&2; exit 2 ;;
esac

compose() { docker compose --project-directory "$MULTICHAT_COMPOSE_DIR" "$@"; }

# Runs inside a bridge container; prints one JSON object describing its logins.
# shellcheck disable=SC2016
probe='
set -eu
config=/data/config.yaml
port=$(yq -r ".appservice.port" "$config")
user=$(yq -r ".bridge.permissions | to_entries | map(select(.value == \"admin\")) | .[0].key" "$config")
if [ "$(yq -r ".provisioning | has(\"shared_secret\")" "$config")" = true ]; then
  secret=$(yq -r ".provisioning.shared_secret" "$config")
  answer=$(wget -qO- --header "Authorization: Bearer $secret" "http://127.0.0.1:$port/_matrix/provision/v3/whoami?user_id=$user")
  printf "{\"api\":\"bridgev2\",\"user\":\"%s\",\"answer\":%s}\n" "$user" "$answer"
elif [ -f /data/mautrix-telegram.db ]; then
  logged_in=$(python3 -c "
import sqlite3, sys
connection = sqlite3.connect(\"file:/data/mautrix-telegram.db?mode=ro\", uri=True)
row = connection.execute(\"select tgid is not null from user where mxid = ?\", (sys.argv[1],)).fetchone()
print(\"true\" if row and row[0] else \"false\")" "$user")
  printf "{\"api\":\"telegram-database\",\"user\":\"%s\",\"answer\":{\"logged_in\":%s}}\n" "$user" "$logged_in"
else
  secret=$(yq -r ".bridge.provisioning.shared_secret" "$config")
  prefix=$(yq -r ".bridge.provisioning.prefix" "$config")
  answer=$(wget -qO- --header "Authorization: Bearer $secret" "http://127.0.0.1:$port$prefix/v1/ping?user_id=$user")
  printf "{\"api\":\"legacy-ping\",\"user\":\"%s\",\"answer\":%s}\n" "$user" "$answer"
fi
'

reports=()
for service in $(compose config --format json | jq -r '.services | to_entries[] | select((.value.profiles // []) == []) | .key'); do
  container=$(compose ps --all --format json "$service" | jq -rs '.[0].Name // empty')
  if [[ -z $container ]]; then
    reports+=("$(jq -nc --arg service "$service" '{service: $service, running: false, fault: "no container"}')")
    continue
  fi
  state=$(docker inspect -f '{{.State.Status}}' "$container")
  if [[ $state != running ]]; then
    data_dir=$(docker inspect -f '{{range .Mounts}}{{if eq .Destination "/data"}}{{.Source}}{{end}}{{end}}' "$container")
    if [[ -n $data_dir && -e $data_dir/registration.yaml ]]; then
      reports+=("$(jq -nc --arg service "$service" --arg state "$state" '{service: $service, running: false, fault: "container is \($state)"}')")
    fi
    continue
  fi
  docker exec "$container" test -f /data/registration.yaml || continue # Synapse, not a bridge
  if ! raw=$(docker exec "$container" sh -c "$probe" 2>&1); then
    reports+=("$(jq -nc --arg service "$service" --arg error "$raw" '{service: $service, running: true, fault: "status API failed: \($error)"}')")
    continue
  fi
  reports+=("$(jq -c --arg service "$service" '
    .api as $api
    | if .api == "bridgev2" then
      {logins: [.answer.logins[]? | {id, name, state: .state.state_event, error: (.state.error // "")}]}
    elif .api == "legacy-ping" then
      (.answer | to_entries[] | select(.value | type == "object" and has("logged_in")) | .value) as $login
      | {logins: (if $login.logged_in then [{id: $login.id, name: $login.id,
                   state: (if $login.connected then "CONNECTED" else "DISCONNECTED" end), error: ""}] else [] end)}
    else
      {logins: (if .answer.logged_in then [{id: .user, name: .user, state: "LOGGED_IN", error: "connection state unknown"}] else [] end)}
    end
    | {service: $service, running: true, api: $api} + .
    | .fault = (if (.logins | length) == 0 then "no login"
                elif any(.logins[]; .state != "CONNECTED" and .state != "LOGGED_IN") then
                  "login " + ([.logins[] | select(.state != "CONNECTED" and .state != "LOGGED_IN") | "\(.name): \(.state) \(.error)"] | join(", "))
                else "" end)
  ' <<<"$raw")")
done

all=$(printf '%s\n' "${reports[@]}" | jq -s .)
if [[ $mode == json ]]; then
  jq . <<<"$all"
  exit 0
fi
faults=$(jq -r '.[] | select(.fault != "") | "FAIL \(.service): \(.fault)"' <<<"$all")
if [[ -n $faults ]]; then
  echo "$faults"
  exit 1
fi
echo "ok: $(jq -r 'map(.service) | join(", ")' <<<"$all")"
