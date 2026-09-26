#!/usr/bin/env bash
set -o errexit -o nounset -o pipefail

repository_url="https://github.com/BerriAI/litellm"
raw_base_url="https://raw.githubusercontent.com/BerriAI/litellm"
snapshot_name="model_prices_and_context_window.json"
snapshot_directory="catalog/litellm"
download_timeout_seconds=120

if [[ $# -ne 0 ]]; then
  echo "usage: scripts/refresh-litellm-snapshot.sh" >&2
  exit 2
fi

cd "$(dirname "$0")/.."

commit="$(git ls-remote "${repository_url}.git" refs/heads/main | cut --fields=1)"
if [[ ! "${commit}" =~ ^[0-9a-f]{40}$ ]]; then
  echo "main branch commit not found repository=${repository_url}" >&2
  exit 1
fi

mkdir --parents "${snapshot_directory}"
download="$(mktemp "${snapshot_directory}/.${snapshot_name}.XXXXXX")"
trap 'rm --force "${download}"' EXIT

curl --fail --silent --show-error --location --max-time "${download_timeout_seconds}" \
  --output "${download}" "${raw_base_url}/${commit}/${snapshot_name}"

node --eval '
const snapshot = JSON.parse(require("fs").readFileSync(process.argv[1], "utf8"));
if (typeof snapshot !== "object" || snapshot === null || Array.isArray(snapshot)) {
  throw new Error("snapshot is not a JSON object");
}
' "${download}"

chmod 0644 "${download}"
mv "${download}" "${snapshot_directory}/${snapshot_name}"
cat > "${snapshot_directory}/SOURCE" <<EOF
repository: ${repository_url}
file: ${snapshot_name}
commit: ${commit}
fetched_on: $(date --utc +%Y-%m-%d)
EOF
echo "snapshot refreshed commit=${commit}"
