# Releasing

Preburn follows [semantic versioning](https://semver.org) and stays on 0.x until the API is stable. While below 1.0, a release with features or breaking changes bumps the minor version and a release with only fixes bumps the patch version. The Python SDK in [preburn/sdk-python](https://github.com/preburn/sdk-python) has its own versions and releases.

## How a release happens

1. Commits on `main` follow Conventional Commits (`feat:`, `fix:`, `docs:`, `chore:`), as [CONTRIBUTING.md](../CONTRIBUTING.md) describes.
2. On every push to `main`, the `release-please` workflow updates a release pull request. It holds the next version, the `CHANGELOG.md` entries written from the commits since the last release, and the version in the README's quickstart download links. Its commits are signed off as `github-actions[bot]`.
3. A maintainer checks and merges the release pull request, as [Before merging a release pull request](#before-merging-a-release-pull-request) describes. release-please then tags the merge commit `vX.Y.Z` and creates the GitHub release.
4. In the same run, the `release-please` workflow calls the `release` workflow with the new tag. It builds the image from the tag's commit for `linux/amd64` and `linux/arm64`, pushes `ghcr.io/preburn/preburn:X.Y.Z` and `ghcr.io/preburn/preburn:X.Y` with SBOM and provenance attestations, and attaches `api/openapi.json` to the release. It moves `ghcr.io/preburn/preburn:latest` only when the tag's commit is on `main`. A tag that is not `vX.Y.Z` fails the workflow before anything is pushed.

Both workflows run with the default workflow token, `GITHUB_TOKEN`. GitHub starts no workflow for a pull request, push or tag made with that token. The release tag therefore cannot start the `release` workflow, and the `release-please` workflow calls it instead.

The SDK repository runs the same `release-please` workflow with the same token and sign-off. It builds no image, so it has no `release` workflow.

## Before merging a release pull request

The release pull request gets no CI, because it is opened with `GITHUB_TOKEN`. It changes only `CHANGELOG.md` and version files: `.release-please-manifest.json` and the README, and in the SDK repository also `pyproject.toml` and `uv.lock`. Before merging, check that:

- CI passed on `main` at the commit the release pull request is based on: lint, tests, the generated file check, the image build and the Compose smoke test.
- The changelog describes every change a user must act on, such as a new required variable or a changed API field.
- `make image smoke` passes locally.

Then merge it as a repository admin, bypassing the required checks that never ran on it.

## After the release

- Confirm both architectures of the new tags on GHCR.
- Follow the README quickstart in an empty folder with the published image, and see the check in the dashboard.
- In the SDK repository, set `tests/contract/SERVER_VERSION` to the new version, so its contract tests check against the released `openapi.json`.

## Rebuild a release

To build and push an existing release again, for example after a failed push to GHCR, run the `release` workflow by hand with the tag. In the Actions tab choose `release`, then Run workflow, and enter the tag, or run:

```sh
gh workflow run release.yaml --repo preburn/preburn --field tag=vX.Y.Z
```

It checks out the tag, builds its commit with the image versions in that commit's `versions.env`, pushes the same image tags and replaces `openapi.json` on the release. Rebuilding a release that is not the newest can move `X.Y`, and `latest` when its commit is on `main`, back to it. Rebuild the newer releases afterwards.

## Repository settings

- In both repositories, Settings > Actions > General > Workflow permissions has "Allow GitHub Actions to create and approve pull requests" turned on. Without it release-please cannot open the release pull request.
- Each workflow job declares its own token permissions. The `release-please` job gets write access to contents, issues and pull requests. The job that calls the `release` workflow grants write access to contents and packages, because a called workflow cannot have more permissions than the job that calls it.
- Branch protection on `main` requires the CI checks and is not enforced for admins, so an admin can merge the release pull request.
- A tag ruleset on `v*` in this repository blocks updating and deleting release tags, and only the repository admin role can bypass it. It does not restrict creating tags, because release-please creates them with the built-in workflow token and GitHub does not accept GitHub Actions as a bypass actor on a repository ruleset.

## Dependency updates

Renovate runs early on Monday mornings (UTC) and opens one pull request per group: Go modules, npm packages, GitHub Actions, Docker images, the tool versions in `versions.env`, Go, Node.js and gitleaks.

- The Go group changes `GO_VERSION` and `GO_IMAGE` together, the Node.js group `NODE_VERSION` and `NODE_IMAGE`, and the gitleaks group `GITLEAKS_VERSION` and `GITLEAKS_IMAGE`.
- The Docker images group covers the other images in `versions.env` and the Compose files, so the Postgres and Valkey images in `compose.yaml` and `versions.env` change together.
- Renovate also moves the Python SDK tag in the README quickstart to the newest `preburn/sdk-python` release.
- New Postgres major versions are left out, because existing installations need a data upgrade.

Nothing merges automatically. Every commit, from people or bots, carries a `Signed-off-by` trailer.

### What is pinned

| Dependency | Where | Pinned by |
|---|---|---|
| Node.js image of the dashboard build stage | `NODE_IMAGE` in `versions.env` | tag and digest |
| Go image of the binary build stage | `GO_IMAGE` in `versions.env` | tag and digest |
| Distroless base of the release image | `RUNTIME_IMAGE` in `versions.env` | tag and digest |
| gitleaks image of the CI `secrets` job | `GITLEAKS_IMAGE` in `versions.env` | tag and digest |
| GitHub Actions | `.github/workflows/` | commit SHA, with the version in a comment |
| Postgres and Valkey images | `POSTGRES_IMAGE` and `VALKEY_IMAGE` in `versions.env`, and `compose.yaml` | tag, until Renovate's first pull request adds digests to both files |
| stripe-mock image of the dev stack | `STRIPE_MOCK_IMAGE` in `versions.env` | tag, until Renovate's first pull request adds its digest |
| Base of the tools image | `golang:${GO_VERSION}-bookworm` in `docker/tools.Dockerfile` | tag |
| Node.js, sqlc, golangci-lint, gitleaks and actionlint in the tools image | `versions.env` | version and SHA-256 checksum of each archive |
| goimports in the tools image | `GOIMPORTS_VERSION` in `versions.env` | version, verified by the Go checksum database |
| go-licenses of the binary build stage | `GO_LICENSES_VERSION` in `versions.env` | version, verified by the Go checksum database |
| pnpm | `PNPM_VERSION` in `versions.env` and `packageManager` in `web/package.json` | version |
| Go and Node.js of the CI `go` and `web` jobs | `GO_VERSION` and `NODE_VERSION` in `versions.env` | version |
| Go modules and npm packages | `go.sum` and `web/pnpm-lock.yaml` | version and checksum |

The tools image builds only development and CI tooling and never ships, so its base keeps a plain tag.

### Tool checksums

`docker/tools.Dockerfile` checks every archive it downloads against the SHA-256 values in `versions.env`. Renovate updates the versions of Node.js, sqlc, golangci-lint, gitleaks and actionlint, but not their checksums. Their pull request fails `make tools` until you replace the tool's `_SHA256_AMD64` and `_SHA256_ARM64` values with the checksums of the new version's linux amd64 and arm64 archives:

| Tool | Where the checksums come from |
|---|---|
| Node.js | `SHASUMS256.txt` of the release on nodejs.org |
| golangci-lint | `golangci-lint-<version>-checksums.txt` on the GitHub release |
| gitleaks | `gitleaks_<version>_checksums.txt` on the GitHub release |
| actionlint | `actionlint_<version>_checksums.txt` on the GitHub release |
| sqlc | No checksum file is published. Run `curl -fsSL <archive URL> \| sha256sum` with the archive URLs from `docker/tools.Dockerfile`. |

In the SDK repository, Renovate updates the uv version and its checksum together, and the gitleaks image with its digest, in one tool versions pull request.
