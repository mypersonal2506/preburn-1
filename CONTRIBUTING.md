# Contributing to Preburn

Everyone taking part in the project follows the [Code of Conduct](CODE_OF_CONDUCT.md). Report vulnerabilities privately as described in [SECURITY.md](SECURITY.md), never in a public issue.

## Development setup

You need Docker and make. Every other tool runs in a container. `make help` lists the targets, and [docs/development.md](docs/development.md) describes the development workflow.

## Sign off your commits

Preburn uses the [Developer Certificate of Origin](https://developercertificate.org) (DCO). By signing off a commit you certify that you wrote the change or have the right to submit it under the project's open-source license. You also acknowledge that the contribution and your sign-off, including your name and email, are public and kept indefinitely.

Sign off with `git commit -s`. Git adds a trailer with the name and email from your Git configuration:

```
Signed-off-by: Your Name <you@example.com>
```

To sign off commits already on your branch, run `git rebase --signoff main`. CI checks every commit in a pull request except merge commits and fails when one has no `Signed-off-by` trailer.

## Commit messages

Use the Conventional Commits format. Start the subject with a type:

- `feat:` a new feature
- `fix:` a bug fix
- `docs:` documentation only
- `chore:` build, tooling or maintenance

Example: `fix: reject negative quantities in usage reports`. The release tooling reads these types to write the changelog and choose the next version.

## Pull requests

- Add or update tests for every change in behavior.
- `make check` must pass.
- After changing the source of a generated file, regenerate it and commit the result. `.gitattributes` marks the generated paths: sqlc queries under `internal/*/queries`, the OpenAPI document `api/openapi.json`, the dashboard API client under `web/src/client` and the route tree `web/src/routeTree.gen.ts`.
- Put ignores for your own editor or tools in `.git/info/exclude`, not in `.gitignore`.

## Code standards

### Go

- Format with `gofmt` and `goimports`. `make lint` runs golangci-lint with the configuration in `.golangci.yaml`.
- Use full words for names. The only short names allowed are `ctx` and `err`. Receivers use the type's lowercase noun (`service`, `store`), not single letters. Initialisms stay upper case (`ID`, `URL`, `HTTP`, `API`, `JSON`, `SQL`).
- Wrap errors with lowercase context and no "failed to" (`fmt.Errorf("load customer state: %w", err)`). Name sentinel errors `ErrXxx`. Domain errors are typed and mapped to API problems in one place.
- Pass `context.Context` as the first parameter of anything that does I/O. Do not add global mutable state, `init` functions or panics outside startup wiring.
- Define limits as named constants that fail loud when exceeded.
- Order each file as package doc, imports, constants and types, then functions.
- Give every package a `doc.go` with a `// Package <name>` comment. Every exported identifier has a doc comment that starts with its name and states its behavior and contract. Add an inline comment only where an obvious change would silently break something.
- Write tests with the standard `testing` package and `github.com/google/go-cmp/cmp` for diffs, table-driven when there are several cases. Integration tests run against real Postgres and Valkey, using a template database and a unique Redis key prefix. Do not sleep in tests. Inject the clock or poll with a deadline.
- Write SQL with uppercase keywords, snake_case identifiers and one statement per sqlc `-- name:` block.

### TypeScript and React

- Format and lint with Biome defaults (tabs, double quotes) and the recommended rules. `noUnusedImports`, `noUnusedVariables` and `noNonNullAssertion` are errors.
- Compile in TypeScript strict mode with `noUncheckedIndexedAccess`. Do not use `any` or non-null assertions. Use `as` only in `as const`.
- Name files in kebab-case. Put one component in each file and use named exports.
- Write TSDoc for exported shared components, hooks and `lib` functions, not for route components.
- Write console and thrown error messages as lowercase key=value (`request schema mismatch path=<path>`).
- Name things after the API (`CheckResponse`, `PlanResponse`), never with generic names like `data` or `item`.

### Python SDK

The Python SDK lives in [preburn/sdk-python](https://github.com/preburn/sdk-python). It uses ruff for formatting and linting, mypy in strict mode, Google-style docstrings on the public API, and `logging.getLogger("preburn")` with key=value messages. Library code has no `assert` and no `print`.

### Writing

These rules apply to docs, docstrings, the README, dashboard text and log messages.

- No em dashes, no double hyphens used as dashes, no semicolons joining clauses, no unicode arrows or ellipses, no emoji.
- No filler, no cutesy phrasing, no "successfully" endings, no gratuitous exclamation marks.
- Short, direct, active sentences. Sentence case headings.
- Log messages are event names with key=value attributes and a lowercase first word.
