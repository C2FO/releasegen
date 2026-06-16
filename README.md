![releasegen-logo.png](docs/images/releasegen-logo.png)

# ReleaseGen

---

`ReleaseGen` is a Go application designed to automate versioning and release creation based on the content of `CHANGELOG.md` files. The application adheres to Semantic Versioning (SemVer) principles, ensuring that versions are incremented correctly based on the types of changes documented in your changelog.

You write a normal, human-readable [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) entry under `## [Unreleased]`; when you merge to your release branch, ReleaseGen decides the next version, promotes those notes into a numbered section, commits and tags it, and publishes a matching GitHub Release. That's the whole job — no plugins, no runtime, no DSL.

## Why ReleaseGen?

Most release automation derives the version and notes from **commit messages** (Conventional Commits) or from special **intent files**. ReleaseGen takes the position that the changelog you already maintain *is* the source of truth, and that the only thing standing between a merged PR and a published release is mechanical work a tool should do for you.

It is a good fit when you want:

- **Changelog-driven, not commit-driven.** Your `CHANGELOG.md` is the contract. You don't have to enforce Conventional Commits, squash policies, or commit linting across every contributor to get correct versions.
- **Language-agnostic monorepo releases.** A "module" is just any directory containing a `CHANGELOG.md`. Each gets its own independent version line and tag (e.g. `services/api/v1.2.3`). It works equally well for Go, Node, Python, or polyglot repos — it never inspects your source.
- **One small, auditable step.** A single static binary (or container image) that does exactly one thing: turn curated changelog intent into a tag + GitHub Release. It composes with whatever builds and publishes your artifacts, rather than replacing them.
- **Safe by default.** `--dry-run` previews every decision, runs fail atomically (a bad module aborts the run rather than half-releasing), bearer tokens are scrubbed from error output, and structured exit codes let CI branch on the failure class instead of grepping logs.
![releasegen-features.png](docs/images/releasegen-features.png)
### How it compares

| Tool | Decides version from | Monorepo model | Scope |
| ---- | -------------------- | -------------- | ----- |
| **ReleaseGen** | Curated `CHANGELOG.md` (Keep a Changelog) | Any dir with a changelog; per-module tags | Tag + GitHub Release only |
| semantic-release | Conventional Commit messages | Plugins / extra config | Versioning + publishing (Node-centric) |
| release-please | Conventional Commit messages | Release PRs per package | Release PR + tag + release |
| Changesets | Hand-written intent files (`.changeset/`) | First-class (JS/TS workspaces) | Versioning + publishing (JS-centric) |
| GoReleaser | Existing git tags | N/A (builds artifacts) | Build + package + publish |

ReleaseGen deliberately does **not** build artifacts, publish to package registries, open PRs, or write your changelog for you. If you need those, run ReleaseGen for the version/tag/release step and pair it with your existing build tooling.

## Quick Start

Install the CLI with Go:

```bash
go install github.com/c2fo/releasegen/cmd/releasegen@latest
```

Or pull the container image from GitHub Container Registry:

```bash
docker pull ghcr.io/c2fo/releasegen:latest
```

Preview what would happen for your repo without changing anything:

```bash
releasegen --dry-run --repo-root . --repository your-org/your-repo --branch main --actor "$USER" --token "$GH_TOKEN"
```

When you're ready to automate it, drop the [example GitHub Actions workflow](#workflow-example) into `.github/workflows/`.

## Features

---

- **Automatic Versioning**: Detects changes in `CHANGELOG.md` and increments your project’s version following SemVer.
- **Monorepo Support**: Discovers changes to `CHANGELOG.md` files across different directories and generates appropriate release tags for each module.
- **Release Tagging**: Creates a Git tag for the new version, optionally prefixed by the directory path if the `CHANGELOG.md` file is located outside the repo’s root.
- **GitHub Releases**: Automatically creates a GitHub release, pulling release notes directly from your changelog.

## How It Works

---

### Versioning Logic

ReleaseGen inspects the `CHANGELOG.md` file for notable changes and applies version bumps according to SemVer:

- **Major Version Bump**: If the words "BREAKING CHANGE" appear under any “Changed” or “Removed” sections, indicating backward-incompatible changes.
- **Minor Version Bump**: If there are new features, non-breaking changes, deprecations, or security updates.
- **Patch Version Bump**: If only bug fixes are found.
- **Manual Version Override**: If the MANUAL_VERSION environment variable is set, it overrides the calculated version.

### Monorepo Support & Release Tag Naming

If you maintain multiple modules in a single repository (a monorepo), ReleaseGen will:
1.	Detect all `CHANGELOG.md` files in different subdirectories.
2.	Assign separate release tags to each directory that has new changes under ## [Unreleased].
![releasegen-mono.png](docs/images/releasegen-mono.png)
#### Single Module (Root)

- The tag is simply `vX.Y.Z` (e.g., `v1.2.3`).

#### Multiple Modules (Monorepo)

- The tag is prefixed by the path to the module, (e.g. `worker/v2.3.4` or `services/api/v0.2.0`).

This convention keeps releases organized in larger repositories. A future enhancement may allow “flat” tag naming if you prefer to omit directory prefixes.

### Custom Change Types

You can define custom change types and their corresponding bump types using the `CUSTOM_CHANGE_TYPES` environment variable. For example:

```yaml
CUSTOM_CHANGE_TYPES: |
  documentation:patch
  performance:minor
```

### Debug Mode

For troubleshooting tag detection issues, enable detailed logging with the `DEBUG` environment variable or the `--debug` flag:

```yaml
DEBUG: true
```

When enabled, ReleaseGen will output detailed information about:

- Which tags are being processed
- Module names extracted from tags
- Which tags are successfully added vs skipped

This is particularly useful for diagnosing issues in multi-module repositories or when tags aren't being detected as expected.

### v2 highlights

Releasegen v2 ships several quality-of-life and safety improvements while
keeping the v1 contract intact for end users (CHANGELOG format, env vars,
GitHub Actions integration). The breaking changes are mostly internal /
distribution-side:

- **New module path.** When consumed as a library, the import is
  `github.com/c2fo/releasegen/...`. The CLI binary path is
  unchanged inside the docker image.
- **`c2fo/vfs/v7` and `golang.org/x/oauth2` are gone.** The binary uses the
  standard library + `google/go-github/v68` only.
- **CLI flags for every env var.** Every documented env var has a matching
  `--flag`. Flags > env > built-in defaults.
- **`--dry-run`.** Prints what would happen (next version, bump type) without
  rewriting files, committing, pushing, tagging, or publishing. Safe to run
  locally against your real repo.
- **`--summary-file`** / **`SUMMARY_FILE`.** Writes a JSON summary of the
  run that downstream workflow steps can read instead of screen-scraping
  logs.
- **`--repo-root`** / **`REPO_ROOT`.** Run releasegen against a worktree
  that isn't `.`.
- **`--version`.** Prints the build-time version.
- **Structured exit codes.** `0` (success / nothing to do), `1` (config),
  `2` (changelog validation), `3` (git), `4` (GitHub API), `10` (internal).
  CI scripts can branch on these instead of grepping logs.
- **Structured logging.** `log/slog`-based; in GitHub Actions
  (`GITHUB_ACTIONS=true`) it still emits `::group::`, `::endgroup::`, and
  `::error::` markers. Locally, output is plain text.
- **Validated `MANUAL_VERSION`.** Must be a valid semver string before it is
  used.
- **Token scrubbing.** Bearer tokens are stripped from go-git push errors
  before they reach the logs.
- **Configurable self-release.** The "releasegen releasing itself" detection
  (`RELEASEGEN_SELF_MODULE` / `RELEASEGEN_SELF_REPO`) is now overridable;
  defaults are unchanged for c2fo/releasegen.

### Building locally

```bash
go build -ldflags "-X main.version=$(git describe --tags --always)" -o release-gen ./cmd/releasegen
./release-gen --help
# Preview what a release would do against another checkout, without writing anything:
./release-gen --dry-run --repo-root /path/to/your/repo --repository your-org/your-repo --branch main --actor you --token "$GH_TOKEN"
```

## Example `CHANGELOG.md`

---

Your CHANGELOG.md should follow the `CHANGELOG.md` files following the [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) format. For example:

```markdown
# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- New feature X.

### Changed
- Modified behavior of Y.
- **BREAKING CHANGE**: Changed API behavior in module Z.

### Removed
- Deprecated feature W removed.
- **BREAKING CHANGE**: Removed support for legacy API.

### Deprecated
- Feature V is now deprecated.

### Security
- Updated dependencies for security patches.

### Fixed
- Fixed bug related to issue #123.

## [my-project/v1.2.3] - 2024-08-09
### Added
- Another new feature.

### Fixed
- Fixed a minor bug.

```

Note that while we require adhering to the Keep a Changelog format, `ReleaseGen` allows for custom change type headings when used with the env var `CUSTOM_CHANGE_TYPES`.

## Developer Expectations

---

When using `ReleaseGen`, developers should follow these guidelines to ensure the application can parse the `CHANGELOG.md` file correctly:

1. **Use Section Headings**: (`### Added`, `### Changed`, `### Removed`, `### Deprecated`, `### Security`, `### Fixed`) — these are case-insensitive.
2. **Mark Breaking Changes**: Include the exact phrase “BREAKING CHANGE” for backward-incompatible changes to ensure a major version bump. This safeguards against unintentional major version increments.
3. **Don’t Manually Change Versions**: Keep new changes under ## [Unreleased]. Let `ReleaseGen` handle the version bump when merged to `main`.
4. **Maintain Consistency**: Be clear and consistent in wording so the application can parse changes accurately.
5. **Organize New Entries**: Always add new changes under the ## [Unreleased] section so that `ReleaseGen` can move them into the next release.

## Integrating ReleaseGen into a GitHub Actions Workflow

---

### Prerequisites: GitHub App Setup for Branch Protection

To enable ReleaseGen to work with branch protection rules (requiring PR reviews, status checks, etc.), you need to create a GitHub App that can bypass these protections:

1. **Create a GitHub App**:
  - Go to `https://github.com/settings/apps` (personal) or `https://github.com/organizations/YOUR_ORG/settings/apps` (organization)
  - Click **New GitHub App**
  - Set a name (e.g., `releasegen-bot`)
  - Set Homepage URL to your repository URL
  - Uncheck **Webhook** → Active
  - Set **Repository permissions**:
    - Contents: **Read and write**
  - Click **Create GitHub App**
  - **Save the App ID** shown at the top of the settings page
2. **Generate Private Key**:
  - On the app settings page, scroll to "Private keys"
  - Click **Generate a private key**
  - Download and save the `.pem` file securely
3. **Install the App**:
  - Go to app settings → **Install App** (left sidebar)
  - Install on your organization or account
  - Select the repositories where you want to use ReleaseGen
4. **Add Secrets and Variables**:
  - For each repository, go to Settings → Secrets and variables → Actions
  - Add secret: `RELEASEGEN_APP_PRIVATE_KEY` = contents of the `.pem` file
  - Add secret: `RELEASEGEN_APP_ID` = your App ID
5. **Configure Branch Protection**:
  - Go to repository Settings → Rules
  - Create or edit branch protection for `main`
  - Enable desired protections (PR reviews, status checks, etc.)
  - Under **Bypass list**, add your GitHub App by name

### Workflow Example

Below is an example GitHub Actions workflow to automate releases using `ReleaseGen`:

```yaml
name: Release by Changelog

on:
  push:
    branches:
      - main
  workflow_dispatch:
    inputs:
      branch:
        description: 'Branch to create a release from'
        required: true
        default: 'main'
      version:
        description: 'Specify the semantic version for the release (vX.Y.Z)'
        required: true
      reason:
        description: 'Reason for the manual release'
        required: false

jobs:
  release:
    runs-on: ubuntu-latest

    steps:
      - name: Generate GitHub App token
        id: generate-token
        uses: actions/create-github-app-token@v1
        with:
          app-id: ${{ secrets.RELEASEGEN_APP_ID }}
          private-key: ${{ secrets.RELEASEGEN_APP_PRIVATE_KEY }}

      - name: Checkout repository
        uses: actions/checkout@8e8c483db84b4bee98b60c0593521ed34d9990e8 #v6
        with:
          ref: ${{ github.event.inputs.branch || github.ref_name }}
          fetch-depth: 0
          token: ${{ steps.generate-token.outputs.token }}

      - name: Run ReleaseGen
        env:
          GITHUB_TOKEN: ${{ steps.generate-token.outputs.token }}
          GITHUB_REPOSITORY: ${{ github.repository }}
          GITHUB_ACTOR: ${{ github.actor }}
          GITHUB_REF_NAME: ${{ github.event.inputs.branch || github.ref_name }}
          MANUAL_VERSION: ${{ github.event.inputs.version || '' }}
          REASON: ${{ github.event.inputs.reason || '' }}
          # Optional: EXCLUDE_DIRS exclude certain directories from changelog generation
          EXCLUDE_DIRS: |
            some/app
            some/other/app
          # Optional: CUSTOM_CHANGE_TYPES allow for custom change types
          CUSTOM_CHANGE_TYPES: |
            documentation:patch
            performance:minor
        run: |
          docker run --rm \
            -e GITHUB_TOKEN \
            -e GITHUB_REPOSITORY \
            -e GITHUB_ACTOR \
            -e GITHUB_REF_NAME \
            -e MANUAL_VERSION \
            -e REASON \
            -e EXCLUDE_DIRS \
            -e CUSTOM_CHANGE_TYPES \
            -v $(pwd):/workspace \
            ghcr.io/c2fo/releasegen:latest \
            --repo-root /workspace
```

> The image's entrypoint is `/usr/local/bin/release-gen`, so any args after
> the image name are passed directly. Use `--dry-run` to preview without
> publishing, or `--summary-file /workspace/release-summary.json` to capture
> a machine-readable result.

### Explanation of the Workflow

- **Generate GitHub App token**: Creates a short-lived authentication token from your GitHub App credentials that can bypass branch protection rules.
- **Checkout repository**: Checks out your repository using the app token so that the workflow has access to the code and `CHANGELOG.md`.
- **Run ReleaseGen**: Runs the `ReleaseGen` Docker container, which reads the `CHANGELOG.md`, determines the next version, commits the updated changelog back to the main branch, creates tags, and generates a GitHub release.
- **Environment Variables**:
  - `GITHUB_TOKEN`: The GitHub App token for authentication (required)
  - `GITHUB_REPOSITORY`: The repository identifier (required)
  - `GITHUB_ACTOR`: The user who triggered the workflow (required)
  - `GITHUB_REF_NAME`: The release branch (required; usually injected by Actions)
  - `MANUAL_VERSION` / `REASON`: Used by the manual workflow dispatch to force a specific version
  - `EXCLUDE_DIRS`: Optional list of directories to exclude from changelog-based releases
  - `CUSTOM_CHANGE_TYPES`: Optional custom change types and their corresponding version increments
  - `REPO_ROOT`: Optional path to the git working tree (defaults to `.`; useful when invoking from outside the repo)
  - `SUMMARY_FILE`: Optional path; when set, releasegen writes a JSON summary of the run there
  - `DEBUG`: When `true`, emits verbose tag/discovery diagnostics
  - `RELEASEGEN_SELF_MODULE` / `RELEASEGEN_SELF_REPO`: Identify the "releasegen releasing itself" case so that the resolved version is printed to stdout for downstream workflow steps. Defaults are `releasegen` and `c2fo/releasegen`; override only if you fork.

Every environment variable above also has an equivalent CLI flag. Flag values take precedence over environment values, which take precedence over built-in defaults.

### Manual Release Workflow Dispatch

If you want to **manually** trigger a release on a different branch:

1. Go to **Actions** in your repository.
2. Select **Release by Changelog**.
3. Click **Run workflow**.
4. Choose the branch.
5. (Optional) Add a reason or message.
6. Click **Run workflow** again.

This will create a release based on the changes in the specified branch.

## FAQ

---

### Table of Contents

1. [What happens when no tags exist yet?](#what-happens-when-no-tags-exist-yet)
2. [What if there are no changes in the CHANGELOG.md?](#what-if-there-are-no-changes-in-the-changelogmd)
3. [How does ReleaseGen determine which version to increment?](#how-does-releasegen-determine-which-version-to-increment)
4. [Will a major version bump automatically update my go.mod in a Golang project?](#will-a-major-version-bump-automatically-update-my-gomod-in-a-golang-project)
5. [Can I exclude certain directories from release generation?](#can-i-exclude-certain-directories-from-release-generation)
6. [What if there are multiple CHANGELOG.md files in different directories?](#what-if-there-are-multiple-changelogmd-files-in-different-directories)
7. [Why does the release tag include the directory path in a monorepo?](#why-does-the-release-tag-include-the-directory-path-in-a-monorepo)
8. [Can I manually trigger a release from a specific branch?](#can-i-manually-trigger-a-release-from-a-specific-branch)
9. [What if I want to advance the version to a specific number?](#what-if-i-want-to-advance-the-version-to-a-specific-number)
10. [What if an error occurs during the release process?](#what-if-an-error-occurs-during-the-release-process)
11. [Can I customize the versioning logic?](#can-i-customize-the-versioning-logic)
12. [How can I contribute to ReleaseGen?](#how-can-i-contribute-to-releasegen)

### What happens when no tags exist yet?

`ReleaseGen` treats the repository as though it started at v0.0.0. It will create the first tag according to the changes found under ## [Unreleased].

### What if there are no changes in the CHANGELOG.md?

No new release is created. `ReleaseGen` only processes a release if it finds valid entries under ## [Unreleased].

### How does ReleaseGen determine which version to increment?

`ReleaseGen` scans each `CHANGELOG.md` under the ## [Unreleased] section and looks for specific keywords or headings (e.g., BREAKING CHANGE) to decide whether to bump the major, minor, or patch version.

### Will a major version bump automatically update my go.mod in a Golang project?

No. You must update your go.mod file manually if you wish to reflect the new major version.

### Can I exclude certain directories from release generation?

Yes. Set the `EXCLUDE_DIRS` environment variable (in YAML, as shown above) to a list of directories you want to skip.

### What if there are multiple CHANGELOG.md files in different directories?

`ReleaseGen` will independently process each file. Each directory’s changes result in its own release tag (e.g., `worker/vX.Y.Z`).

### Why does the release tag include the directory path in a monorepo?

Prefixing tags (e.g., `services/api/v1.2.3`) keeps releases organized and prevents collisions in complex repos. A flat naming option may be considered in the future.

### Can I manually trigger a release from a specific branch?

Yes. You can use the workflow_dispatch event in GitHub Actions to specify the branch (see above workflow example). `ReleaseGen` will then create a release based on that branch’s `CHANGELOG.md`.

### What if I want to advance the version to a specific number?

Use the `MANUAL_VERSION` env var (or `--manual-version` flag, or the
`version` input on the manual workflow dispatch) to force a specific
semantic version. `REASON` / `--reason` is appended to the changelog footer
to record why the manual bump was needed. The value must be a valid semver
string; releasegen rejects anything else with exit code 1.

### What if an error occurs during the release process?

The process exits non-zero, the failure is logged with a `::error::`
GitHub Actions marker, and no further modules are released. The exit code
tells you which layer failed:

| Code | Meaning                                     |
| ---- | ------------------------------------------- |
| 0    | Success or "nothing to release"             |
| 1    | Configuration error (missing/invalid input) |
| 2    | Changelog validation error (malformed `[Unreleased]`, unknown change type, incomplete `BREAKING CHANGE`) |
| 3    | Git error (push, tag, commit, etc.)         |
| 4    | GitHub API error (release creation)         |
| 10   | Internal error (bug; please file an issue)  |

If the run wrote any tags or releases before failing, those are not rolled
back — fix the failing module, push a new commit, and rerun.

### Can I customize the versioning logic?

By default, `ReleaseGen` follows the Keep a Changelog headings and SemVer rules. You can define additional headings and the bump they trigger via the `CUSTOM_CHANGE_TYPES` environment variable (or the `--custom-change-types` flag) using newline-separated `<heading>:<bump>` pairs, where `<bump>` is `major`, `minor`, or `patch`. For example, to make a `### Documentation` section trigger a minor release:

```yaml
CUSTOM_CHANGE_TYPES: |
  Documentation:patch
```

See the [Workflow Example](#workflow-example) for how to set this in a GitHub Actions workflow.

### How can I contribute to ReleaseGen?

We welcome bug reports, feature requests, and pull requests. Feel free to open an issue or submit a PR on our repository.

## License

This project is licensed under the MIT License. See the `LICENSE` file for details.