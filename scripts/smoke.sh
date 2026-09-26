#!/usr/bin/env bash
set -o errexit -o nounset -o pipefail

project="preburn-smoke"
readiness_timeout_seconds=120
member_email="smoke@example.com"
member_name="Smoke test"
api_key_name="smoke"
check_body='{"customer_id":"smoke-customer","feature":"smoke_check","provider":"openai","model":"gpt-6-luna","usage_estimate":{"input_tokens":"1000","output_tokens":"500"}}'

if [[ $# -ne 1 ]]; then
  echo "usage: scripts/smoke.sh <image>" >&2
  exit 2
fi
image="$1"

cd "$(dirname "$0")/.."
unset "${!PREBURN_@}" "${!OTEL_@}" POSTGRES_PASSWORD

secret_key="$(docker run --rm "${image}" secret-key)"
postgres_password="$(openssl rand -hex 16)"
directory="$(mktemp -d)"
environment_file="${directory}/smoke.env"
override_file="${directory}/compose.smoke.yaml"
authorization_file="${directory}/authorization"

{
  cat .env.example
  echo "PREBURN_SECRET_KEY=${secret_key}"
  echo "POSTGRES_PASSWORD=${postgres_password}"
  echo "PREBURN_PORT=0"
} > "${environment_file}"

cat > "${override_file}" << EOF
services:
  migrate:
    image: "${image}"
  api:
    image: "${image}"
  worker:
    image: "${image}"
EOF

compose() {
  docker compose --project-name "${project}" --file compose.yaml --file "${override_file}" --env-file "${environment_file}" "$@"
}

cleanup() {
  local status=$?
  if ((status != 0)); then
    compose logs --no-color >&2
  fi
  compose down --volumes
  rm -r "${directory}"
}
trap cleanup EXIT

request() {
  local path="$1" body="$2" expected_status="$3" response_file="$4" status
  status="$(curl --silent --show-error --output "${response_file}" --write-out '%{http_code}' \
    --header @"${authorization_file}" --header "Content-Type: application/json" \
    --data "${body}" "http://${api_address}${path}")"
  if [[ "${status}" != "${expected_status}" ]]; then
    echo "smoke request failed path=${path} status=${status} expected_status=${expected_status}" >&2
    cat "${response_file}" >&2
    exit 1
  fi
}

compose down --volumes
started=${SECONDS}
compose up --detach --wait --wait-timeout "${readiness_timeout_seconds}"
api_address="$(compose port api 8080)"
echo "smoke ready address=${api_address} seconds=$((SECONDS - started))"

password="$(openssl rand -hex 24)"
member_id="$(compose exec -T api /preburn admin create --email "${member_email}" --name "${member_name}" \
  --password-stdin <<< "${password}")"
echo "smoke member created member_id=${member_id}"

api_key="$(compose exec -T api /preburn admin api-key create --environment test --scope runtime --name "${api_key_name}")"
echo "Authorization: Bearer ${api_key}" > "${authorization_file}"
echo "smoke api key created environment=test scope=runtime"

request /api/v1/check "${check_body}" 200 "${directory}/check.json"
decision_id="$(jq --raw-output .decision_id "${directory}/check.json")"
echo "smoke check status=200 decision_id=${decision_id}"
jq . "${directory}/check.json"

report_body="$(jq --null-input --compact-output --arg decision_id "${decision_id}" \
  '{decision_source: "server", decision_id: $decision_id, usage: {input_tokens: "900", output_tokens: "400"}}')"
request /api/v1/report "${report_body}" 202 "${directory}/report.json"
echo "smoke report status=202 decision_id=${decision_id}"

echo "smoke passed project=${project} image=${image}"
