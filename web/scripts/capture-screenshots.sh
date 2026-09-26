#!/usr/bin/env bash
set -o errexit -o nounset -o pipefail

database="${SCREENSHOT_DATABASE:?name of a migrated database on the test Postgres}"
key_prefix="${SCREENSHOT_KEY_PREFIX:?Valkey key prefix}"
email="${SCREENSHOT_EMAIL:?email of the member the screenshots sign in as}"
port="${SCREENSHOT_PORT:?free host port for the server}"
output="${SCREENSHOT_OUTPUT:?output directory relative to the repository root}"
server_name="preburn-screenshot-server"
server_port=8480
development_port=5180
readiness_attempts=60
browsers_path="/opt/playwright-browsers"
compose=(docker compose --env-file versions.env --file compose.test.yaml)

if [[ $# -ne 0 || "${output}" == /* ]]; then
  echo "usage: SCREENSHOT_DATABASE=<database> SCREENSHOT_KEY_PREFIX=<prefix> SCREENSHOT_EMAIL=<email>" \
    "SCREENSHOT_PORT=<port> SCREENSHOT_OUTPUT=<relative directory> web/scripts/capture-screenshots.sh" >&2
  exit 2
fi

cd "$(dirname "$0")/../.."

if lsof -nP -iTCP:"${port}" -sTCP:LISTEN >/dev/null; then
  echo "screenshot port busy port=${port}" >&2
  exit 1
fi

stop_server() {
  if [[ -n "$(docker ps --quiet --filter "name=^${server_name}$")" ]]; then
    docker kill --signal TERM "${server_name}" >/dev/null
  fi
}

make web-build
"${compose[@]}" run --rm --no-deps --no-TTY tools go build -tags embedweb -o bin/preburn ./cmd/preburn

PREBURN_SECRET_KEY="$(openssl rand -base64 32)"
export PREBURN_SECRET_KEY
server_environment=(
  --env "PREBURN_DATABASE_URL=postgres://preburn:preburn@postgres:5432/${database}?sslmode=disable"
  --env "PREBURN_REDIS_URL=redis://valkey:6379"
  --env "PREBURN_REDIS_KEY_PREFIX=${key_prefix}"
  --env PREBURN_SECRET_KEY
  --env "PREBURN_PUBLIC_URL=http://127.0.0.1:${port}"
  --env "PREBURN_HTTP_ADDRESS=:${server_port}"
)

server_log="$(mktemp)"
trap stop_server EXIT
"${compose[@]}" run --rm --no-TTY --name "${server_name}" --publish "127.0.0.1:${port}:${server_port}" \
  "${server_environment[@]}" tools bin/preburn serve </dev/null >"${server_log}" 2>&1 &
server_process=$!

for attempt in $(seq "${readiness_attempts}"); do
  if curl --fail --silent --output /dev/null "http://127.0.0.1:${port}/readyz"; then
    break
  fi
  if [[ "${attempt}" -eq "${readiness_attempts}" ]]; then
    echo "server not ready attempts=${readiness_attempts} log=${server_log}" >&2
    exit 1
  fi
  sleep 1
done
echo "server ready port=${port} log=${server_log}"

SCREENSHOT_LINK_URL="$("${compose[@]}" run --rm --no-TTY "${server_environment[@]}" tools \
  bin/preburn admin reset-password --email "${email}" </dev/null)"
export SCREENSHOT_LINK_URL

"${compose[@]}" run --rm --no-deps --no-TTY --user root --env SCREENSHOT_LINK_URL \
  --env "PLAYWRIGHT_BROWSERS_PATH=${browsers_path}" tools bash -o errexit -o nounset -o pipefail -c "
    pnpm --dir web exec playwright install --with-deps --only-shell chromium >/dev/null
    runuser --user preburn -- env PREBURN_DEV_API_URL='http://${server_name}:${server_port}' \
      pnpm --dir web exec vite --logLevel warn >/tmp/vite.log 2>&1 &
    for attempt in \$(seq ${readiness_attempts}); do
      if curl --fail --silent --output /dev/null http://127.0.0.1:${development_port}/; then
        break
      fi
      if [[ \${attempt} -eq ${readiness_attempts} ]]; then
        echo 'dev server not ready attempts=${readiness_attempts}' >&2
        exit 1
      fi
      sleep 1
    done
    runuser --user preburn -- node web/scripts/capture-screenshots.ts \
      --base-url 'http://${server_name}:${server_port}' \
      --development-url 'http://127.0.0.1:${development_port}' \
      --email '${email}' --output '${output}'
  " </dev/null

docker kill --signal TERM "${server_name}" >/dev/null
server_exit_code=0
wait "${server_process}" || server_exit_code=$?
if [[ "${server_exit_code}" -ne 0 ]] || ! grep --quiet '"msg":"server.stopped"' "${server_log}"; then
  echo "server stop failed exit_code=${server_exit_code} log=${server_log}" >&2
  exit 1
fi
echo "server stopped exit_code=${server_exit_code}"
