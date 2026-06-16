# ReleaseGen — Product Requirements Document

## 1. Overview

**ReleaseGen** is an automated, changelog-driven release tool. It removes the
human guesswork from versioning and release publication by treating the
project's `CHANGELOG.md` as the single source of truth for *what changed* and
*how significant the change is*.

When a repository is merged to its release branch, ReleaseGen reads the
`## [Unreleased]` section of every relevant changelog, decides the next
[Semantic Version](https://semver.org/spec/v2.0.0.html) based on the kinds of
entries it finds, rewrites the changelog to "promote" those notes to a numbered
release, commits and tags the result, and publishes a corresponding GitHub
Release whose body is the freshly-cut release notes.

ReleaseGen is purpose-built for **monorepos**: it understands that a single
repository can host many independently versioned modules, each with its own
`CHANGELOG.md`, its own version history, and its own tag namespace. A change to
one module produces a release for that module only, and never accidentally
implies a release of its siblings.

ReleaseGen is designed to run unattended inside CI (typically a GitHub Actions
workflow on `push` to `main`, with an optional `workflow_dispatch` escape
hatch). It is opinionated, conventions-first, and deliberately small in scope:
it does not generate code, write changelog entries for the developer, or open
pull requests. It only completes the last mile — turning curated changelog
intent into a real, tagged, published release.

## 2. Goals & Non-Goals

### Goals
- Make releases a side effect of merging well-formed changelog entries, not a
  separate manual ritual.
- Enforce [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) conventions
  so that humans and the tool agree on what each section means.
- Apply SemVer rigorously and predictably: the highest-impact change in the
  unreleased section determines the bump.
- Treat each module in a monorepo as an independent release unit, with its own
  version line and its own tag.
- Produce releases that are self-describing: tag, GitHub Release, and the
  changelog entry all reference the same notes and link back to each other.
- Be safe to run on every merge: do nothing if there is nothing to release;
  fail loudly and atomically if something is malformed.
- Provide a controlled override for humans (manual version, manual reason)
  without compromising the automated path.

### Non-Goals
- ReleaseGen does **not** author changelog content. Developers (or their
  tooling) must add entries to `## [Unreleased]` themselves.
- ReleaseGen does **not** open or merge pull requests, run tests, build
  artifacts, or publish packages to language-specific registries.
- ReleaseGen does **not** rewrite version strings inside source code (e.g.
  `go.mod` major version paths, `package.json` version, etc.). That is the
  developer's responsibility.
- ReleaseGen does **not** support pre-release identifiers (`-rc.1`, `-beta`)
  or build metadata as a first-class workflow.
- ReleaseGen does **not** support release branches other than the branch it is
  invoked on; it is not a release-train manager.
- ReleaseGen does **not** delete or amend existing releases or tags.

## 3. Users & Use Cases

### Primary users
- **Application and library maintainers** who want a no-friction
  "merge to main → cut a release" loop.
- **Monorepo platform owners** who need each module in a shared repository to
  be released independently and without coordination.
- **CI/CD engineers** who want a single, auditable Docker step in their
  release workflow.

### Representative use cases
1. **Single-module repository.** A developer adds an `### Added` entry under
   `## [Unreleased]` in the root `CHANGELOG.md`, opens a PR, and merges.
   ReleaseGen cuts `v1.4.0`, tags it, and publishes a GitHub Release.
2. **Monorepo with many modules.** A PR touches `services/api/` and adds
   a `### Fixed` entry in that module's changelog. ReleaseGen produces only
   `services/api/v0.2.4`; siblings are untouched.
3. **Coordinated multi-module change.** A PR adds entries in two different
   module changelogs simultaneously. ReleaseGen produces two independent
   releases in the same run, each with its own version, tag, and GitHub
   Release.
4. **Breaking change.** A maintainer documents a `### Changed` item that
   includes the literal phrase **"BREAKING CHANGE"**. ReleaseGen bumps the
   major version.
5. **Manual override.** An on-call engineer triggers the workflow via
   `workflow_dispatch`, supplying an explicit version (e.g. `v2.0.0`) and a
   reason. ReleaseGen ignores the calculated bump, uses the supplied version,
   and appends the override reason and actor to the release notes.
6. **First-ever release.** A brand-new module has a `CHANGELOG.md` with only
   an `## [Unreleased]` section and no prior tags. ReleaseGen treats the
   starting point as `0.0.0`, applies the bump, and creates the module's
   first tag and release.

## 4. Core Concepts

### Changelog as source of truth
The `CHANGELOG.md` file, written in Keep a Changelog format, is the contract
between the developer and ReleaseGen. The developer expresses intent by
choosing a section heading (`### Added`, `### Fixed`, etc.) and writing
human-readable bullets. ReleaseGen interprets that intent.

### Module
A *module* is the directory in which a `CHANGELOG.md` lives. The module name
is the directory path relative to the repository root. A `CHANGELOG.md` at the
repository root has an empty module name and is treated as the "root module".
Every other changelog defines its own module, identified by its full relative
directory path (e.g. `worker`, `services/api`, `pkg/logger`).

### Module-scoped tag
Each module has its own independent version history expressed through tags:
- Root module: `vX.Y.Z` (e.g. `v1.2.3`).
- Sub-module: `<module-path>/vX.Y.Z` (e.g. `services/api/v0.2.0`).

Tags are the canonical record of what has been released. ReleaseGen reads
existing tags to determine each module's current version and writes a new tag
to record the new one.

### Unreleased section
Each changelog has exactly one `## [Unreleased]` section. Everything between
that heading and the most recent versioned heading represents work that has
been merged but not yet released. ReleaseGen consumes this section, decides
the next version from its contents, and rewrites the file so that the
unreleased section becomes a versioned section and a new (empty) unreleased
section takes its place at the top.

### Bump type
The bump type — *major*, *minor*, or *patch* — is computed from the
section headings present in the unreleased block, plus any user-defined
custom change types. The highest-impact match wins.

## 5. Behavior

### 5.1 Discovery

On startup, ReleaseGen scans the working tree of the current commit for every
file named `CHANGELOG.md`. From this set it removes any path under a directory
listed in the `EXCLUDE_DIRS` configuration. For each remaining changelog, it
identifies the module name from the file's directory.

For each module, ReleaseGen finds the most recent existing tag belonging to
that specific module by reading all tags in the repository, parsing the
module prefix from each tag name, and selecting the newest one (by tagger
date) whose commit is reachable from the branch being released. Tags whose
commits are not reachable from the current branch are ignored. Modules with
no prior tag are treated as if they were at version `0.0.0` and are always
considered eligible for release.

ReleaseGen then determines whether each changelog has actually been modified
since its module's most recent tag. Changelogs that have not changed are
silently dropped from the run. The remaining changelogs are the candidates
to release.

### 5.2 Version calculation

For each candidate changelog, ReleaseGen extracts the contents of the
`## [Unreleased]` section and the most recent prior version recorded in the
file. (If no prior version exists in the file, the starting version is
`0.0.0`.)

It then classifies the unreleased content:

- **Major** — A `### Changed` or `### Removed` heading is present *and* the
  literal phrase `BREAKING CHANGE` appears somewhere in the unreleased block.
- **Minor** — Any `### Added`, `### Deprecated`, `### Security` heading is
  present, or a `### Changed` / `### Removed` heading is present together
  with `BREAKING CHANGE` text (already classified as major above), or a
  custom change type is configured at minor.
- **Patch** — Only `### Fixed` entries (or custom change types configured at
  patch) are present.

If a `### Changed` or `### Removed` heading is present but `BREAKING CHANGE`
is **not**, ReleaseGen refuses to proceed and surfaces an explicit error
explaining that those headings are reserved for breaking changes and
suggesting an appropriate alternative section. This is a deliberate
guard-rail against accidental major bumps and against misuse of the
sections.

If the unreleased section exists but contains no recognized change type
(default or custom), ReleaseGen errors out for that module.

If the unreleased section is empty or absent, ReleaseGen treats the module
as having "no changes detected" and skips it without error.

The calculated next version is produced by incrementing the appropriate
component of the current version using SemVer rules.

### 5.3 Custom change types

Operators may extend the default vocabulary by configuring additional section
names mapped to bump types via `CUSTOM_CHANGE_TYPES`. For example,
`documentation:patch` causes a `### Documentation` section to drive a patch
bump, and `performance:minor` causes a `### Performance` section to drive a
minor bump.

Custom types follow the same priority rule as built-in types (major beats
minor beats patch), and a custom *major* mapping still requires the
`BREAKING CHANGE` text in the unreleased section before it will actually
produce a major bump.

### 5.4 Manual version override

If a `MANUAL_VERSION` is supplied (typically via `workflow_dispatch` input),
ReleaseGen uses it verbatim as the next version, bypassing its calculation.
When a manual version is used, ReleaseGen also appends a footer to the
release notes for that release in the form
`Manual release by <actor>: <reason>`, where the actor and the reason are
also supplied by the workflow context. This makes manual interventions
permanently visible in the changelog and the GitHub Release.

A manual version is applied uniformly to every changelog being processed in
the run.

### 5.5 Changelog rewrite

For each module being released, ReleaseGen rewrites the changelog file in
place:

- The existing `## [Unreleased]` heading is preserved at the top, but its
  content is moved out of it, leaving it empty for the next cycle.
- A new versioned section is inserted directly below it. The heading takes
  the form `## [[<release-name>](<release-url>)] - <YYYY-MM-DD>`, where
  `<release-name>` is the module-scoped release identifier (`vX.Y.Z` or
  `<module>/vX.Y.Z`), `<release-url>` deep-links to the GitHub Release that
  is about to be created, and the date is the current UTC date.
- The body of the new section is the original unreleased content (plus the
  manual-release footer if applicable).

### 5.6 Commit, tag, push

For each released module, ReleaseGen stages the rewritten changelog file,
commits it on the current branch with the message
`chore: release version <module>/v<version> (<actor>) [skip ci]`, and pushes
the commit. The `[skip ci]` marker prevents the release commit from
re-triggering the release workflow.

It then creates an annotated tag whose name follows the module-scoped tag
convention (`vX.Y.Z` or `<module>/vX.Y.Z`) pointing at the new commit, and
pushes that tag to the remote. The tagger identity is the standard
GitHub Actions bot.

### 5.7 GitHub Release publication

After the tag is in place, ReleaseGen calls the GitHub API to create a
Release for that tag. The release name is `[<tag-name>] - <YYYY-MM-DD>`, and
the release body is the unreleased section that was promoted (including the
manual override footer, when applicable). This means the GitHub Release, the
changelog entry, and the tag are all consistent with one another and easy to
navigate between.

### 5.8 Multi-module runs

When a single invocation produces releases for several modules, each module is
processed independently and sequentially: discover → bump → rewrite → commit
→ tag → push → publish. Modules that have nothing to release are skipped
with a clear log message. A failure on one module aborts the entire run with
a non-zero exit code; ReleaseGen does not attempt partial recovery or
rollback.

### 5.9 Self-release awareness

When ReleaseGen is releasing itself (i.e. running inside the `c2fo/releasegen`
repository and producing a new version of the `releasegen` module), it emits
the new releasegen version to standard output. This is intended to be
captured by the surrounding workflow so that a downstream step (such as a
container image build) can be conditionally executed only when releasegen
itself was bumped.

## 6. Inputs & Configuration

ReleaseGen is configured entirely through environment variables. There is no
configuration file. This keeps the tool trivial to run as a CI step.

### Required (typically supplied by the CI environment)
- `GITHUB_TOKEN` — Token used to push commits/tags and to create the GitHub
  Release. Must be authorized to bypass branch protection on the release
  branch (a GitHub App token is the supported pattern).
- `GITHUB_REPOSITORY` — `<owner>/<repo>` identifier of the repository being
  released.
- `GITHUB_ACTOR` — User attributed to the run; surfaced in commit messages
  and in the manual-release footer.
- `GITHUB_REF_NAME` — The branch being released. Used to determine which
  tags are reachable and to push to the right ref.

### Optional
- `MANUAL_VERSION` — Explicit version string that overrides the calculated
  bump for every module released in the run.
- `REASON` — Free-text justification appended (along with the actor) to the
  release notes when `MANUAL_VERSION` is set.
- `EXCLUDE_DIRS` — Newline- or comma-separated list of directory prefixes to
  exclude from changelog discovery. Useful to keep vendored or third-party
  changelogs out of the release pipeline.
- `CUSTOM_CHANGE_TYPES` — Newline-separated list of `<heading>:<bump>` pairs
  that extend the default Keep a Changelog vocabulary.
- `DEBUG` — When set to `true`, emits verbose logs about tag discovery,
  module-name extraction, reachability decisions, and which tags were
  accepted or skipped. Intended for diagnosing detection problems in
  complex monorepos.

## 7. Outputs

- **Modified `CHANGELOG.md` files** — Committed back to the release branch.
- **Git commits** — One per released module, on the release branch, marked
  with `[skip ci]`.
- **Git tags** — One per released module, annotated, pushed to the remote.
- **GitHub Releases** — One per released module, with notes lifted from the
  promoted unreleased section.
- **stderr logs** — Grouped using GitHub Actions `::group::` markers for
  readable workflow logs; errors are annotated with `::error::` so they
  surface in the Actions UI.
- **stdout** — Empty in the general case; emits the new releasegen version
  string when ReleaseGen has just released itself in `c2fo/releasegen`.
- **Exit code** — `0` on success (including the "nothing to release" case);
  non-zero on any failure, with the failing error annotated in the logs.

## 8. Constraints, Conventions, and Guard-Rails

- **Keep a Changelog format is mandatory.** Section headings drive behavior;
  free-form changelogs are not supported.
- **Section heading matching is case-insensitive** but the literal phrase
  `BREAKING CHANGE` is matched case-sensitively. This is intentional: the
  developer must opt in to a major bump deliberately.
- **One unreleased section per file.** ReleaseGen extracts the content
  between `## [Unreleased]` and the next versioned section (or end of file
  if none exists).
- **Tags are the version oracle.** The version recorded in the changelog
  file is informational; the tag history is authoritative for determining
  the current version of each module.
- **Reachability matters.** Tags whose commits are not reachable from the
  current branch are ignored when determining a module's current version.
  This prevents tags from feature branches or abandoned histories from
  influencing releases.
- **Atomicity per module, not across modules.** A run that releases three
  modules will release them one at a time. A failure mid-run leaves earlier
  modules already released and later modules unreleased; the operator must
  reconcile by adding a new commit.
- **No version downgrades.** ReleaseGen only ever increments. Manual
  override does not validate that the supplied version is greater than the
  current version; the operator is trusted.
- **`go.mod` is not touched.** A major bump for a Go module does not modify
  the module path. Maintainers are expected to handle major-version
  migrations manually.

## 9. Failure Modes

ReleaseGen aims to fail loudly and clearly. Notable failure conditions:

- **Malformed unreleased section** — A `### Changed` or `### Removed` heading
  without a `BREAKING CHANGE` marker. ReleaseGen errors out and explains the
  rule. No release is created.
- **Unrecognized change type** — Unreleased section contains content that
  matches no built-in or custom heading. ReleaseGen errors out.
- **Unparseable current version** — Existing tag/version cannot be parsed as
  SemVer. ReleaseGen errors out.
- **Git push or tag failure** — Authentication problem, branch protection
  not bypassed, or network failure. ReleaseGen errors out; any earlier
  modules in the same run that already succeeded remain released.
- **GitHub Release API failure** — The tag exists but the Release call
  failed. ReleaseGen errors out; the operator may need to delete the tag
  before retrying or create the Release manually.
- **Empty unreleased section** — *Not* an error. The module is silently
  skipped.

## 10. Distribution and Execution

ReleaseGen is distributed as a single binary packaged in a Docker image. The
expected invocation is from a GitHub Actions workflow that:

1. Generates a short-lived GitHub App token authorized to bypass branch
   protection on the release branch.
2. Checks out the repository at the chosen branch with full history
   (`fetch-depth: 0`) so all tags are available.
3. Runs the ReleaseGen container, mounting the workspace and passing the
   environment variables described in §6.

A manual `workflow_dispatch` entry point is supported, allowing on-call
operators to specify a branch, an explicit version, and a reason for
auditability.

## 11. Future Considerations

The current design is intentionally narrow. Several extensions have been
identified but are out of scope for the present product:

- **Flat tag naming.** Optionally drop the module-path prefix from tags in
  monorepos where teams prefer a flat namespace.
- **Pre-release versions.** First-class support for `-rc`, `-beta`, etc.
- **Authoritative manual version targeting.** A first-class way to fast-
  forward a module to a specific version without going through the
  `MANUAL_VERSION` override path.
- **Automatic source updates** for files that need to track the version
  (e.g. `go.mod` major path, `package.json`, embedded version constants).
- **Pull-request-based releases.** Open a release PR rather than committing
  directly to the release branch, for repositories whose policies forbid
  bot commits even with branch-protection bypass.
- **Richer release notes.** Optional inclusion of contributor lists, PR
  links, or auto-categorization beyond what the changelog already states.
