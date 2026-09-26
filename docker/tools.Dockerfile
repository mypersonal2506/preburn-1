ARG GO_VERSION

FROM golang:${GO_VERSION}-bookworm

ARG TARGETARCH
ARG NODE_VERSION
ARG NODE_SHA256_AMD64
ARG NODE_SHA256_ARM64
ARG PNPM_VERSION
ARG SQLC_VERSION
ARG SQLC_SHA256_AMD64
ARG SQLC_SHA256_ARM64
ARG GOLANGCI_LINT_VERSION
ARG GOLANGCI_LINT_SHA256_AMD64
ARG GOLANGCI_LINT_SHA256_ARM64
ARG GITLEAKS_VERSION
ARG GITLEAKS_SHA256_AMD64
ARG GITLEAKS_SHA256_ARM64
ARG ACTIONLINT_VERSION
ARG ACTIONLINT_SHA256_AMD64
ARG ACTIONLINT_SHA256_ARM64
ARG GOIMPORTS_VERSION
ARG USER_ID=1000
ARG GROUP_ID=1000

ENV COREPACK_HOME=/opt/corepack \
    COREPACK_ENABLE_DOWNLOAD_PROMPT=0

SHELL ["/bin/bash", "-o", "errexit", "-o", "nounset", "-o", "pipefail", "-c"]

RUN groupadd --gid "${GROUP_ID}" preburn \
 && useradd --uid "${USER_ID}" --gid preburn --create-home --shell /bin/bash preburn

RUN <<EOF
case "${TARGETARCH}" in
  amd64) node_architecture=x64 ;;
  arm64) node_architecture=arm64 ;;
  *) echo "unsupported architecture=${TARGETARCH}" >&2; exit 1 ;;
esac
checksum_variable="NODE_SHA256_${TARGETARCH^^}"
archive=/tmp/node.tar.gz
curl --fail --silent --show-error --location --output "${archive}" \
  "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-${node_architecture}.tar.gz"
echo "${!checksum_variable}  ${archive}" | sha256sum --check --strict
tar --extract --gzip --file "${archive}" --directory /usr/local --strip-components=1 --no-same-owner
rm "${archive}"
corepack enable pnpm
corepack install --global "pnpm@${PNPM_VERSION}"
# pnpm downloads its native binary on first run. Containers run with --rm, so the first run happens here.
pnpm --version
chown --recursive preburn:preburn "${COREPACK_HOME}"
EOF

RUN <<EOF
case "${TARGETARCH}" in
  amd64) gitleaks_architecture=x64 ;;
  arm64) gitleaks_architecture=arm64 ;;
esac

install_release_binary() {
  local name="$1" checksum_variable="${2}_${TARGETARCH^^}" url="$3" member="$4"
  local directory
  directory="$(mktemp --directory)"
  curl --fail --silent --show-error --location --output "${directory}/archive.tar.gz" "${url}"
  echo "${!checksum_variable}  ${directory}/archive.tar.gz" | sha256sum --check --strict
  tar --extract --gzip --file "${directory}/archive.tar.gz" --directory "${directory}" "${member}"
  install --mode=0755 "${directory}/${member}" "/usr/local/bin/${name}"
  rm --recursive "${directory}"
}

install_release_binary sqlc SQLC_SHA256 \
  "https://github.com/sqlc-dev/sqlc/releases/download/v${SQLC_VERSION}/sqlc_${SQLC_VERSION}_linux_${TARGETARCH}.tar.gz" \
  sqlc
install_release_binary golangci-lint GOLANGCI_LINT_SHA256 \
  "https://github.com/golangci/golangci-lint/releases/download/v${GOLANGCI_LINT_VERSION}/golangci-lint-${GOLANGCI_LINT_VERSION}-linux-${TARGETARCH}.tar.gz" \
  "golangci-lint-${GOLANGCI_LINT_VERSION}-linux-${TARGETARCH}/golangci-lint"
install_release_binary gitleaks GITLEAKS_SHA256 \
  "https://github.com/gitleaks/gitleaks/releases/download/v${GITLEAKS_VERSION}/gitleaks_${GITLEAKS_VERSION}_linux_${gitleaks_architecture}.tar.gz" \
  gitleaks
install_release_binary actionlint ACTIONLINT_SHA256 \
  "https://github.com/rhysd/actionlint/releases/download/v${ACTIONLINT_VERSION}/actionlint_${ACTIONLINT_VERSION}_linux_${TARGETARCH}.tar.gz" \
  actionlint
EOF

RUN GOBIN=/usr/local/bin go install "golang.org/x/tools/cmd/goimports@v${GOIMPORTS_VERSION}" \
 && go clean -cache -modcache

# Named volumes take the ownership of the cache directories on first mount.
# Without them the cache volumes mount as root and the tools cannot write.
RUN mkdir --parents \
      /go/pkg/mod \
      /home/preburn/.cache/go-build \
      /home/preburn/.cache/golangci-lint \
      /home/preburn/.local/share/pnpm/store \
 && chown --recursive preburn:preburn /go/pkg /home/preburn

# Without this pnpm puts its store inside /workspace, because the store volume is on another filesystem.
ENV pnpm_config_store_dir=/home/preburn/.local/share/pnpm/store

USER preburn

WORKDIR /workspace
