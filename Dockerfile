ARG NODE_IMAGE
ARG GO_IMAGE
ARG RUNTIME_IMAGE

FROM --platform=$BUILDPLATFORM ${NODE_IMAGE:?} AS web

ARG PNPM_VERSION

# pnpm downloads its native binary on first run, so the first run happens in this cached layer.
RUN corepack enable pnpm \
 && corepack install --global "pnpm@${PNPM_VERSION}" \
 && pnpm --version

WORKDIR /web

COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml web/.pnpmfile.mjs \
     web/index.html web/tsconfig.json web/tsr.config.json web/vite.config.ts ./
COPY web/src src

RUN --mount=type=cache,target=/pnpm/store \
    --mount=type=tmpfs,target=/web/node_modules \
    --mount=type=bind,source=scripts/generate-third-party-notices.sh,target=/usr/local/bin/generate-third-party-notices.sh \
    pnpm install --frozen-lockfile --store-dir /pnpm/store \
 && pnpm run build \
 && generate-third-party-notices.sh npm /web > /third-party-notices-npm

FROM --platform=$BUILDPLATFORM ${GO_IMAGE:?} AS build

ARG TARGETOS
ARG TARGETARCH
ARG VERSION
ARG COMMIT
ARG BUILD_DATE

RUN --mount=type=bind,source=versions.env,target=/versions.env \
    --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    . /versions.env \
 && go install "github.com/google/go-licenses/v2@v${GO_LICENSES_VERSION:?}"

WORKDIR /source

COPY go.mod go.sum ./
COPY catalog catalog
COPY cmd cmd
COPY db db
COPY internal internal
COPY web/*.go web/
COPY --from=web /web/dist web/dist

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" go build -tags embedweb -trimpath \
      -ldflags "-s -w \
        -X github.com/preburn/preburn/internal/version.Version=${VERSION:?} \
        -X github.com/preburn/preburn/internal/version.Commit=${COMMIT:?} \
        -X github.com/preburn/preburn/internal/version.BuildDate=${BUILD_DATE:?}" \
      -o /preburn ./cmd/preburn

COPY LICENSE NOTICE /notices/
COPY --from=web /third-party-notices-npm /third-party-notices-npm

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=bind,source=scripts/generate-third-party-notices.sh,target=/usr/local/bin/generate-third-party-notices.sh \
    { cat /notices/NOTICE \
   && CGO_ENABLED=0 GOOS="${TARGETOS}" GOARCH="${TARGETARCH}" GOFLAGS=-tags=embedweb \
      generate-third-party-notices.sh go ./cmd/preburn \
   && cat /third-party-notices-npm; } > /notices/THIRD_PARTY_NOTICES

FROM ${RUNTIME_IMAGE:?} AS runtime

ARG VERSION
ARG COMMIT
ARG BUILD_DATE

LABEL org.opencontainers.image.source="https://github.com/preburn/preburn" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.created="${BUILD_DATE}"

COPY --from=build /notices /usr/share/doc/preburn
COPY --from=build /preburn /preburn

ENTRYPOINT ["/preburn"]
