#!/bin/sh
set -o errexit -o nounset
export LC_ALL=C

usage="usage: scripts/generate-third-party-notices.sh go <package>... | npm <web directory>"
preburn_module="github.com/preburn/preburn"
rule="================================================================================"
tab="$(printf '\t')"
go_rows_template="{{ range . }}{{ .Name }}${tab}{{ .Version }}${tab}{{ .LicensePath }}
{{ end }}"
go_introduction="The preburn binary is built with the Go standard library and these Go modules.
The source code of every module version listed here is available from the Go
module mirror at https://proxy.golang.org."
npm_rows_program='
const { readFileSync } = require("node:fs");
const { join } = require("node:path");
const packagesByLicense = JSON.parse(readFileSync(0, "utf8"));
const rows = [];
for (const [license, packages] of Object.entries(packagesByLicense)) {
  for (const entry of packages) {
    for (const directory of entry.paths) {
      const manifest = JSON.parse(readFileSync(join(directory, "package.json"), "utf8"));
      rows.push([manifest.name, manifest.version, directory, license].join("\t"));
    }
  }
}
for (const row of rows.sort()) {
  console.log(row);
}
'
npm_introduction="The dashboard embedded in the preburn binary bundles code and fonts from these
npm packages."

print_heading() {
  printf '\n%s\n%s %s\n%s\n\n' "${rule}" "$1" "$2" "${rule}"
}

print_files() {
  while IFS= read -r listed_file; do
    cat "${listed_file}"
    printf '\n'
  done < "$1"
}

generate_go_section() {
  printf '%s' "${go_rows_template}" > "${temporary_directory}/go-rows.template"
  go-licenses report --ignore "${preburn_module}" --template "${temporary_directory}/go-rows.template" "$@" \
    > "${temporary_directory}/go-report"
  uniq "${temporary_directory}/go-report" > "${temporary_directory}/go-rows"
  go_version="$(go env GOVERSION)"
  go_root="$(go env GOROOT)"
  printf '\nGo modules\n\n%s\n' "${go_introduction}"
  print_heading "Go standard library" "${go_version}"
  cat "${go_root}/LICENSE"
  printf '\n'
  while IFS="${tab}" read -r name version license_file; do
    print_heading "${name}" "${version}"
    printf '%s\n' "${license_file}" > "${temporary_directory}/go-files"
    find "$(dirname "${license_file}")" -maxdepth 1 -type f -iname 'notice*' ! -path "${license_file}" \
      > "${temporary_directory}/go-notice-files"
    sort "${temporary_directory}/go-notice-files" >> "${temporary_directory}/go-files"
    print_files "${temporary_directory}/go-files"
  done < "${temporary_directory}/go-rows"
}

generate_npm_section() {
  pnpm --dir "$1" licenses list --prod --json > "${temporary_directory}/npm-licenses.json"
  node --eval "${npm_rows_program}" < "${temporary_directory}/npm-licenses.json" > "${temporary_directory}/npm-rows"
  printf '\nnpm packages\n\n%s\n' "${npm_introduction}"
  while IFS="${tab}" read -r name version directory license; do
    print_heading "${name}" "${version}"
    find "${directory}" -maxdepth 1 -type f \( -iname 'licen[cs]e*' -o -iname 'copying*' -o -iname 'notice*' \) \
      > "${temporary_directory}/npm-files"
    sort -o "${temporary_directory}/npm-files" "${temporary_directory}/npm-files"
    if [ -s "${temporary_directory}/npm-files" ]; then
      print_files "${temporary_directory}/npm-files"
    else
      printf 'License: %s. The package includes no license file.\n' "${license}"
    fi
  done < "${temporary_directory}/npm-rows"
}

if [ $# -lt 2 ]; then
  echo "${usage}" >&2
  exit 2
fi
mode="$1"
shift

temporary_directory="$(mktemp -d)"
trap 'rm -rf "${temporary_directory}"' EXIT

case "${mode}" in
  go) generate_go_section "$@" ;;
  npm)
    if [ $# -ne 1 ]; then
      echo "${usage}" >&2
      exit 2
    fi
    generate_npm_section "$1"
    ;;
  *)
    echo "${usage}" >&2
    exit 2
    ;;
esac
