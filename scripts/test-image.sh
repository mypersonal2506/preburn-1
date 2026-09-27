#!/usr/bin/env bash
set -o errexit -o nounset -o pipefail

image="preburn:local"
maximum_image_bytes=50000000
readiness_timeout_seconds=60
forbidden_path_pattern='private|node_modules|_test\.go|(^|/)\.env|(^|/)\.git'
notices_directory="/usr/share/doc/preburn"
immutable_cache_control="cache-control: public, max-age=31536000, immutable"
public_files=("favicon.svg" "manifest.webmanifest")
run_name="preburn-image-test-$$"
database_url="postgres://preburn:preburn@postgres:5432/preburn?sslmode=disable"
redis_url="redis://valkey:6379"

if [[ $# -ne 0 ]]; then
  echo "usage: scripts/test-image.sh" >&2
  exit 2
fi

cd "$(dirname "$0")/.."
. ./versions.env

containers=()
network_created=false
failures=0

pass() {
  echo "pass check=$1 ${*:2}"
}

fail() {
  echo "fail check=$1 ${*:2}"
  failures=$((failures + 1))
}

cleanup() {
  if [[ ${#containers[@]} -gt 0 ]]; then
    docker rm --force --volumes "${containers[@]}" > /dev/null
  fi
  if [[ "${network_created}" == true ]]; then
    docker network rm "${run_name}" > /dev/null
  fi
}
trap cleanup EXIT

wait_until_ready() {
  local container="$1" deadline=$((SECONDS + readiness_timeout_seconds))
  shift
  until "$@" > /dev/null 2>&1; do
    if ((SECONDS >= deadline)); then
      docker logs "${container}" >&2
      echo "container not ready container=${container} timeout_seconds=${readiness_timeout_seconds}" >&2
      exit 1
    fi
    sleep 1
  done
}

check_notice_file() {
  local notice_path="${notices_directory}/$1" notice_text notice_bytes marker missing_markers=()
  if ! notice_text="$(docker cp "${export_container}:${notice_path}" - | tar -xOf -)"; then
    fail notices "path=${notice_path} missing"
    return
  fi
  notice_bytes="$(printf '%s' "${notice_text}" | wc -c | tr -d ' ')"
  for marker in "${@:2}"; do
    if ! grep -qF "${marker}" <<< "${notice_text}"; then
      missing_markers+=("\"${marker}\"")
    fi
  done
  if ((notice_bytes == 0)); then
    fail notices "path=${notice_path} empty"
  elif ((${#missing_markers[@]} > 0)); then
    fail notices "path=${notice_path} bytes=${notice_bytes} missing_markers=${missing_markers[*]}"
  else
    pass notices "path=${notice_path} bytes=${notice_bytes}"
  fi
}

make --no-print-directory image

size_bytes="$(docker history --human=false --format '{{.Size}}' "${image}" | awk '{ total += $1 } END { print total }')"
size_megabytes="$(awk -v bytes="${size_bytes}" 'BEGIN { printf "%.1f", bytes / 1000000 }')"
size_details="size_bytes=${size_bytes} size_megabytes=${size_megabytes} maximum_bytes=${maximum_image_bytes}"
if ((size_bytes > maximum_image_bytes)); then
  fail size "${size_details}"
else
  pass size "${size_details}"
fi

image_user="$(docker image inspect --format '{{.Config.User}}' "${image}")"
case "${image_user%%:*}" in
  "" | 0 | root) fail user "user=${image_user}" ;;
  *) pass user "user=${image_user}" ;;
esac

expected_version_output="$(docker image inspect --format \
  'preburn {{index .Config.Labels "org.opencontainers.image.version"}} (commit {{index .Config.Labels "org.opencontainers.image.revision"}}, built {{index .Config.Labels "org.opencontainers.image.created"}})' \
  "${image}")"
version_output="$(docker run --rm "${image}" version)"
if [[ "${version_output}" == "${expected_version_output}" ]]; then
  pass version "output=\"${version_output}\""
else
  fail version "output=\"${version_output}\" expected=\"${expected_version_output}\""
fi

export_container="$(docker create "${image}")"
containers+=("${export_container}")
image_paths="$(docker export "${export_container}" | tar -tf -)"
path_count="$(wc -l <<< "${image_paths}" | tr -d ' ')"
if forbidden_paths="$(grep -E "${forbidden_path_pattern}" <<< "${image_paths}")"; then
  fail paths "paths=${path_count} forbidden=$(tr '\n' ' ' <<< "${forbidden_paths}")"
else
  pass paths "paths=${path_count}"
fi

check_notice_file "LICENSE" "Apache License"
check_notice_file "NOTICE" "LiteLLM"
check_notice_file "THIRD_PARTY_NOTICES" "LiteLLM" "Go modules" "npm packages" "SIL OPEN FONT LICENSE"

docker network create "${run_name}" > /dev/null
network_created=true
postgres="$(docker run --detach --network "${run_name}" --network-alias postgres \
  --tmpfs /var/lib/postgresql --env POSTGRES_USER=preburn --env POSTGRES_PASSWORD=preburn \
  "${POSTGRES_IMAGE}")"
containers+=("${postgres}")
valkey="$(docker run --detach --network "${run_name}" --network-alias valkey --tmpfs /data \
  "${VALKEY_IMAGE}" valkey-server --save "" --appendonly no)"
containers+=("${valkey}")
wait_until_ready "${postgres}" docker exec "${postgres}" pg_isready --host=127.0.0.1 --username=preburn --dbname=preburn
wait_until_ready "${valkey}" docker exec "${valkey}" valkey-cli ping

PREBURN_SECRET_KEY="$(docker run --rm "${image}" secret-key)"
export PREBURN_SECRET_KEY
preburn_options=(--network "${run_name}" --env "PREBURN_DATABASE_URL=${database_url}"
  --env "PREBURN_REDIS_URL=${redis_url}" --env PREBURN_SECRET_KEY)

docker run --rm "${preburn_options[@]}" "${image}" migrate
api="$(docker run --detach "${preburn_options[@]}" --publish 127.0.0.1::8080 "${image}" serve)"
containers+=("${api}")
api_address="$(docker port "${api}" 8080/tcp)"
wait_until_ready "${api}" curl --fail --silent --output /dev/null "http://${api_address}/healthz"

dashboard_page="$(curl --fail --silent --show-error "http://${api_address}/")"
entry_script="$(sed -n 's|.*<script type="module" crossorigin src="\(/assets/[^"]*\.js\)".*|\1|p' <<< "${dashboard_page}")"
if [[ -z "${entry_script}" ]]; then
  fail dashboard "page has no entry script"
elif entry_headers="$(curl --fail --silent --show-error --output /dev/null --dump-header - \
  "http://${api_address}${entry_script}")" && grep -qiF "${immutable_cache_control}" <<< "${entry_headers}"; then
  pass dashboard "entry_script=${entry_script}"
else
  fail dashboard "entry_script=${entry_script} not served with the immutable cache header"
fi

for public_file in "${public_files[@]}"; do
  public_url="http://${api_address}/${public_file}"
  public_status="$(curl --silent --show-error --output /dev/null --write-out '%{http_code}' "${public_url}")"
  if [[ "${public_status}" != 200 ]]; then
    fail public_file "path=/${public_file} status=${public_status}"
  elif ! cmp --silent <(curl --fail --silent --show-error "${public_url}") "web/public/${public_file}"; then
    fail public_file "path=/${public_file} status=${public_status} matches_source=false"
  else
    pass public_file "path=/${public_file} status=${public_status} matches_source=true"
  fi
done

if ((failures > 0)); then
  echo "image checks failed failures=${failures}" >&2
  exit 1
fi
