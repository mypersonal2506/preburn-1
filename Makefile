.DEFAULT_GOAL := help

PACKAGES ?= ./...
FILES ?=
DOMAIN ?=
REVISIONS ?=

COMPOSE := docker compose --env-file versions.env --file compose.test.yaml
TOOLS := $(COMPOSE) run --rm --no-deps tools
TOOLS_WITH_SERVICES := $(COMPOSE) run --rm tools

WEB := $(wildcard web/package.json)
WEB_MODULES := $(if $(WEB),web/node_modules/.modules.yaml)

SQLC_CONFIGS := $(if $(DOMAIN),db/queries/$(DOMAIN)/sqlc.yaml,$(wildcard db/queries/*/sqlc.yaml))

DRIFT_GENERATORS := generate-sql \
	$(if $(wildcard api/openapi.json),generate-openapi) \
	$(if $(wildcard web/src/client),generate-web-client) \
	$(if $(wildcard web/src/routeTree.gen.ts),generate-routes)
GENERATED_DIGEST := $(COMPOSE) run --rm --no-deps --no-TTY tools bash -o pipefail -c "find . \
	\( -path ./.git -o -path ./private -o -path ./web/node_modules \) -prune -o -type f \
	\( -path './internal/*/queries/*' -o -path ./api/openapi.json -o -path './web/src/client/*' -o -path ./web/src/routeTree.gen.ts \) \
	-print | sort | xargs --no-run-if-empty sha256sum"

.PHONY: help tools test-services-up test-services-down lint test-go test-web web-build image generate generate-sql \
	generate-openapi generate-web-client generate-routes check dev dev-reset smoke e2e catalog-refresh-litellm \
	check-private-names install-hooks

help: ## List targets
	@awk 'BEGIN { FS = ":.*## " } /^[a-z][a-z0-9-]*:.*## / { printf "  %-26s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

tools: ## Build the tools image
	$(COMPOSE) build tools

test-services-up: ## Start Postgres and Valkey for tests
	$(COMPOSE) up --detach --wait postgres valkey

test-services-down: ## Stop the test services
	$(COMPOSE) down

lint: $(WEB_MODULES) ## golangci-lint for PACKAGES, plus Biome, tsc and the copy check once web/ exists
	$(TOOLS) golangci-lint run $(PACKAGES)
ifeq ($(WEB),)
	@echo "skip web lint: web/package.json does not exist"
else
	$(TOOLS) pnpm --dir web run lint
	$(TOOLS) pnpm --dir web run typecheck
	$(TOOLS) pnpm --dir web run check-copy
endif

test-go: ## Go tests for PACKAGES (default ./...) against the test services
	$(TOOLS_WITH_SERVICES) go test -race -p 3 $(PACKAGES)

test-web: $(WEB_MODULES) ## Vitest for FILES (default all)
ifeq ($(WEB),)
	@echo "skip test-web: web/package.json does not exist"
else
	$(TOOLS) pnpm --dir web run test $(FILES)
endif

web-build: $(WEB_MODULES) ## Build the dashboard into web/dist for go build -tags embedweb
ifeq ($(WEB),)
	@echo "skip web-build: web/package.json does not exist"
else
	$(TOOLS) pnpm --dir web run build
endif

image: VERSION ?= dev
image: COMMIT ?= unknown
image: BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
image: ## Build preburn:local for the host platform, stamped with VERSION, COMMIT and BUILD_DATE
	. ./versions.env && docker build --load \
		--build-arg NODE_IMAGE="$$NODE_IMAGE" \
		--build-arg GO_IMAGE="$$GO_IMAGE" \
		--build-arg RUNTIME_IMAGE="$$RUNTIME_IMAGE" \
		--build-arg PNPM_VERSION="$$PNPM_VERSION" \
		--build-arg VERSION="$(VERSION)" \
		--build-arg COMMIT="$(COMMIT)" \
		--build-arg BUILD_DATE="$(BUILD_DATE)" \
		--tag preburn:local .

generate: generate-sql generate-openapi generate-web-client generate-routes ## Run every generator

generate-sql: ## sqlc for DOMAIN (default every domain)
ifeq ($(SQLC_CONFIGS),)
	@echo "skip generate-sql: no db/queries/*/sqlc.yaml"
else
	$(TOOLS) bash -o errexit -c 'for config in $(SQLC_CONFIGS); do sqlc generate --file "$$config"; done'
endif

generate-openapi: ## Write api/openapi.json with preburn openapi
	$(TOOLS) bash -o errexit -c 'trap "rm -f api/openapi.json.tmp" EXIT; mkdir -p api; go run ./cmd/preburn openapi > api/openapi.json.tmp; mv api/openapi.json.tmp api/openapi.json'

generate-web-client: $(WEB_MODULES) ## Hey API client in web/src/client from api/openapi.json
ifeq ($(WEB),)
	@echo "skip generate-web-client: web/package.json does not exist"
else
	$(TOOLS) pnpm --dir web run generate-client
endif

generate-routes: $(WEB_MODULES) ## TanStack Router route tree
ifeq ($(WEB),)
	@echo "skip generate-routes: web/package.json does not exist"
else
	$(TOOLS) pnpm --dir web run generate-routes
endif

check: lint test-go test-web ## Lint, every test, the compose.yaml image tag check and the generated drift check
	$(TOOLS) scripts/test-check-private-names.sh
	$(TOOLS) scripts/check-image-tags.sh
	@set -o errexit; \
	digest_before="$$($(GENERATED_DIGEST))"; \
	$(MAKE) --no-print-directory $(DRIFT_GENERATORS); \
	digest_after="$$($(GENERATED_DIGEST))"; \
	if [ "$$digest_before" != "$$digest_after" ]; then \
		echo "generated files differ from their sources, run make generate" >&2; \
		exit 1; \
	fi

dev: ## Dev stack (project preburn-dev) from compose.dev.yaml, rebuilt and restarted on changes
	docker compose --env-file versions.env --file compose.dev.yaml up --build --watch

dev-reset: ## Remove the dev stack and its volumes, project preburn-dev only
	docker compose --env-file versions.env --file compose.dev.yaml --project-name preburn-dev down --volumes

smoke: ## Run compose.yaml end to end with preburn:local (project preburn-smoke, build the image with make image)
	scripts/smoke.sh preburn:local

e2e: $(WEB_MODULES) ## Playwright against the dev stack
ifeq ($(WEB),)
	@echo "skip e2e: web/package.json does not exist"
else
	$(TOOLS) pnpm --dir web run e2e
endif

catalog-refresh-litellm: ## Refresh the vendored LiteLLM price snapshot and its SOURCE file
	$(TOOLS) scripts/refresh-litellm-snapshot.sh

check-private-names: ## Scan the working tree, or REVISIONS (git rev-list arguments), for private names
ifeq ($(wildcard private/leak-guard/names.txt),)
	@echo "skip check-private-names: private/leak-guard/names.txt does not exist"
else
	$(TOOLS) env PREBURN_PRIVATE_NAMES_FILE=private/leak-guard/names.txt \
		scripts/check-private-names.sh $(if $(REVISIONS),--revisions $(REVISIONS))
endif

install-hooks: ## Set core.hooksPath to .githooks
	git config core.hooksPath .githooks

web/node_modules/.modules.yaml: web/package.json web/pnpm-lock.yaml
	$(TOOLS) pnpm --dir web install --frozen-lockfile
	@touch $@
