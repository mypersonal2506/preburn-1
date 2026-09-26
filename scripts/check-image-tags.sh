#!/usr/bin/env bash
set -o errexit -o nounset -o pipefail

compose_file="compose.yaml"

if [[ $# -ne 0 ]]; then
  echo "usage: scripts/check-image-tags.sh" >&2
  exit 2
fi

cd "$(dirname "$0")/.."
. ./versions.env

failures=0
for expected_image in "${POSTGRES_IMAGE}" "${VALKEY_IMAGE}"; do
  repository="${expected_image%:*}"
  compose_images="$(sed -n "s|^[[:space:]]*image:[[:space:]]*\(${repository}:[^[:space:]]*\)[[:space:]]*$|\1|p" "${compose_file}")"
  if [[ -z "${compose_images}" ]]; then
    echo "image missing file=${compose_file} expected=${expected_image}" >&2
    failures=$((failures + 1))
    continue
  fi
  while read -r compose_image; do
    if [[ "${compose_image}" != "${expected_image}" ]]; then
      echo "image tag differs from versions.env file=${compose_file} image=${compose_image} expected=${expected_image}" >&2
      failures=$((failures + 1))
    fi
  done <<< "${compose_images}"
done

if ((failures > 0)); then
  exit 1
fi
