#!/usr/bin/env bash
set -euo pipefail
umask 077

script_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$script_dir/.." && pwd)"

pocket_id_url="${POCKET_ID_API_URL:-https://auth.aries.timdumol.com}"
pocket_id_url="${pocket_id_url%/}"
api_key_file="${POCKET_ID_API_KEY_FILE:-$repo_root/.pocket-id-api-key}"
ssh_host="${FITNESSD_SSH_HOST:-aries.timdumol.com}"
compose_file="${FITNESSD_COMPOSE_FILE:-/opt/fitsync/app/compose.production.yaml}"

die() {
  printf 'error: %s\n' "$1" >&2
  exit 1
}

command -v curl >/dev/null 2>&1 || die "curl is required"
command -v python3 >/dev/null 2>&1 || die "python3 is required"
command -v ssh >/dev/null 2>&1 || die "ssh is required"
[[ -r "$api_key_file" ]] || die "Pocket ID API key file is not readable: $api_key_file"

key_mode="$(stat -c '%a' "$api_key_file" 2>/dev/null || true)"
[[ "$key_mode" == "400" || "$key_mode" == "600" ]] || die "Pocket ID API key file must have mode 0400 or 0600"
api_key="$(tr -d '\r\n' < "$api_key_file")"
[[ -n "$api_key" ]] || die "Pocket ID API key file is empty"
curl_config="$(mktemp "${TMPDIR:-/tmp}/fitsync-pocket-id-curl.XXXXXX")"
chmod 600 "$curl_config"
trap 'rm -f "$curl_config"' EXIT
printf 'header = "X-API-KEY: %s"\n' "$api_key" > "$curl_config"

lookup_user() {
  local handle="$1"
  local payload user_record
  if ! payload="$(curl --silent --show-error --fail --connect-timeout 10 --max-time 20 \
    --config "$curl_config" --get \
    --data-urlencode "search=$handle" \
    --data-urlencode 'pagination[limit]=100' \
    "$pocket_id_url/api/users" 2>/dev/null)"; then
    die "Pocket ID user lookup failed for $handle"
  fi
  if ! user_record="$(printf '%s' "$payload" | python3 -c '
import json
import re
import sys
import uuid

handle = sys.argv[1]
payload = json.load(sys.stdin)
items = payload.get("data", []) if isinstance(payload, dict) else payload
matches = [item for item in items if isinstance(item, dict) and item.get("username") == handle]
if len(matches) != 1:
    raise SystemExit(1)
user_id = matches[0].get("id")
try:
    subject = str(uuid.UUID(str(user_id)))
except (ValueError, AttributeError):
    raise SystemExit(1)
if not re.fullmatch(r"[0-9a-f-]{36}", subject):
    raise SystemExit(1)
print(subject + "\t" + handle)
' "$handle")"; then
    die "Pocket ID must return exactly one UUID user whose username is $handle"
  fi
  printf '%s\n' "$user_record"
}

bind_identity() {
  local handle="$1"
  local subject="$2"
  local username="$3"
  local remote_command
  local -a remote_args=(
    sudo docker compose --file "$compose_file" exec -T fitnessd fitnessd identities bind
    --subject "$subject" --username "$username" --athlete "$handle"
  )
  printf -v remote_command '%q ' "${remote_args[@]}"
  if ! ssh -o BatchMode=yes -o ConnectTimeout=10 "$ssh_host" "$remote_command"; then
    die "Fitnessd identity binding failed for $handle"
  fi
}

tim_record="$(lookup_user tim)"
jopi_record="$(lookup_user jopi)"
IFS=$'\t' read -r tim_subject tim_username <<< "$tim_record"
IFS=$'\t' read -r jopi_subject jopi_username <<< "$jopi_record"
[[ "$tim_username" == "tim" ]] || die "Pocket ID username mismatch for tim"
[[ "$jopi_username" == "jopi" ]] || die "Pocket ID username mismatch for jopi"

bind_identity tim "$tim_subject" "$tim_username"
printf 'synchronized tim\n'
bind_identity jopi "$jopi_subject" "$jopi_username"
printf 'synchronized jopi\n'
