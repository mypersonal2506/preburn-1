# Development

You need Docker and make. Go, Node, pnpm, sqlc, golangci-lint and the other tools run in a tools image that `make` builds from `docker/tools.Dockerfile`, pinned to the versions in `versions.env`. Read [CONTRIBUTING.md](../CONTRIBUTING.md) for the commit and code rules.

## Make targets

`make help` lists every target. The ones you use most:

| Target | Does |
|---|---|
| `make dev` | Starts the development stack and rebuilds on changes. |
| `make dev-reset` | Removes the development stack and its volumes. |
| `make lint` | golangci-lint, then Biome, `tsc` and the copy check for the dashboard. `PACKAGES` narrows the Go packages. |
| `make test-go` | Go tests with the race detector against the test Postgres and Valkey. `PACKAGES` narrows them, such as `make test-go PACKAGES=./internal/policies/...`. |
| `make test-web` | Vitest for the dashboard. `FILES` narrows it. |
| `make generate` | Runs every generator: sqlc, the OpenAPI document, the dashboard's API client and its route tree. |
| `make check` | Lint, every test, and a check that the generated files match their sources. Run it before a pull request. |
| `make image` | Builds the production image `preburn:local` for your platform. |
| `make smoke` | Runs `compose.yaml` with `preburn:local` under the project `preburn-smoke`: creates a member and a key, sends a check and a report, then removes the project. |
| `make test-services-down` | Stops the test Postgres and Valkey. |
| `make install-hooks` | Points git at `.githooks`, whose pre-push hook runs `make check-private-names` on the commits being pushed. |

## Development stack

`make dev` runs `compose.dev.yaml` as the Compose project `preburn-dev` and watches your files:

| Service | Address | Notes |
|---|---|---|
| `web` | http://localhost:5180 | Vite with hot reload. Proxies `/api` to the api. Open the dashboard here. |
| `api` | http://localhost:8480 | Built from source. |
| `worker` | | Built from source. |
| `migrate` | | Runs once at start. |
| `postgres` | `localhost:25432` | User, password and database `preburn`. |
| `valkey` | `localhost:26379` | |
| `stripe-mock` | `localhost:22111` | For the Stripe connector of a later release. |

- Go changes under `cmd`, `internal`, `catalog` and `db` are copied into the api and worker containers, which rebuild the binary and restart, about 9 seconds after a save. Changes to `go.mod` or `go.sum` rebuild the images.
- Changes under `web/src` reach the browser through hot reload within a second, and changes under `web/public`, such as the icons, are copied into the `web` container too. Changes to `web/package.json` or the lockfile rebuild the `web` image.
- `migrate` runs only when the stack starts. After adding a migration or changing a catalog file, stop `make dev` and start it again, or rerun migrate in another terminal:

  ```sh
  docker compose --env-file versions.env --file compose.dev.yaml up --build migrate
  ```

- The stack uses a fixed development secret key and `PREBURN_PUBLIC_URL=http://localhost:5180`. Find the setup link with `docker compose --env-file versions.env --file compose.dev.yaml logs api | grep setup_link_created`.
- Its data lives in the volumes `preburn-dev_dev-postgres` and `preburn-dev_dev-valkey` until `make dev-reset`.

A cold start takes about 35 seconds, a warm one about 7.

## Tests

- Go integration tests run against real Postgres and Valkey from `compose.test.yaml`, the project `preburn-test` on ports 35432 and 36379, with both stores on tmpfs. `make test-go` starts them. Each test package builds a template database once and gives each test its own copy, and each test uses its own Valkey key prefix.
- Pure packages, such as `internal/policies`, `internal/signals`, `internal/money` and the rating engine in `internal/pricing`, have table-driven unit tests.
- Tests never sleep. They inject a manual clock or poll with a deadline.
- Dashboard tests run with Vitest and Testing Library in jsdom.
- Guard tests keep the code and the docs in step: every log event is registered in `internal/logging/events.go`, and every error code has an anchor in [errors.md](errors.md).

## Generated files

These files are generated and committed. After changing their source, run `make generate` and commit the result. `make check` fails when they differ.

| File | Source |
|---|---|
| `internal/*/queries/` | The SQL under `db/queries/`, through sqlc. |
| `api/openapi.json` | The Go route declarations, through `preburn openapi`. |
| `web/src/client/` | `api/openapi.json`, through Hey API. |
| `web/src/routeTree.gen.ts` | The route files under `web/src/routes/`. |

## Pricing catalog

The curated prices live in `catalog/pricing/<provider>.yaml`, with aliases, display names, default attributes and parameter mappings next to them. `make catalog-refresh-litellm` downloads a new LiteLLM snapshot into `catalog/litellm/` together with the commit it came from. Tests load every catalog file, so a typo in a meter or attribute fails `make test-go`.

## Load test

`preburn bench check` sends checks and reports to a running api and prints their latencies. See [Sizing](self-hosting.md#sizing).
