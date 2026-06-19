# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed
- `releasegen validate --require-changelog-entry` now folds staged, unstaged,
  and untracked worktree changes into its diff against the base ref. Without
  this, the check was a no-op under pre-commit hooks (e.g. prenup), because
  HEAD is still the parent commit at that moment and a tree-only diff sees
  nothing. Local pre-commit checks now catch missing `[Unreleased]` entries
  just as reliably as CI does on a pushed commit.
- `releasegen validate --require-changelog-entry` now compares the
  `[Unreleased]` section as it appears in the **git index** (the staged
  view of the next commit) rather than the working tree. Previously, a
  developer who edited `CHANGELOG.md` but forgot to `git add` it would
  see the pre-commit check pass — the worktree had the new entry — even
  though the commit being created did not, leaving the gap to be caught
  only in CI. The check now predicts the post-commit state correctly:
  unstaged changelog edits no longer satisfy the requirement, and
  `git commit -a` keeps working because git stages worktree changes
  before pre-commit hooks run.

## [[v1.2.0](https://github.com/C2FO/releasegen/releases/tag/v1.2.0)] - 2026-06-18
### Added
- New `releasegen validate` subcommand. Parses every `## [Unreleased]` section
  in the repository and reports **every** malformed heading it finds — both
  across files and within a single file — in a single batched report. Detects
  `### Changed` / `### Removed` without the `BREAKING CHANGE` marker as well
  as any heading not declared in `custom_change_types`, naming the offending
  heading in each error. Needs no GitHub token, performs no I/O beyond reads,
  and exits `0` on success / `2` on any validation failure — intended as a
  required PR-time check before the release workflow.
- New `.releasegen.yaml` (also `.releasegen.yml`) config file at the repo root.
  Supports `custom_change_types`, `exclude_dirs`, a `validate:` block (see
  below), and the advanced `self_release_module` / `self_release_repo`
  overrides. Precedence is flags > env > file > built-in defaults; unknown
  keys cause a configuration error so typos surface early. Both `validate`
  and the existing release path read it, so repo-shape options no longer
  have to be duplicated across workflows.
- New optional `--require-changelog-entry` mode for `releasegen validate`
  (also exposed as `validate.require_changelog_entry: true` in
  `.releasegen.yaml` and `RELEASEGEN_REQUIRE_CHANGELOG_ENTRY=true` in the
  environment). When enabled, validate fails any PR whose non-`CHANGELOG.md`
  files changed vs the base ref but whose `[Unreleased]` section gained no
  new lines. Modules are scoped by where `CHANGELOG.md` lives, with the
  root changelog catching every file not claimed by a submodule changelog.
  The base ref is configurable via `--base-ref` / `RELEASEGEN_BASE_REF` /
  `validate.base_ref` and defaults to `origin/` on GitHub
  Actions pull-request runs, else `origin/main`. Subsumes the prior
  external `ensure_changelog` workflow, which has been removed from this
  repository in favor of the validate-driven check.

### Fixed
- Classifier no longer treats `###` heading references in prose as real
  markdown headings. The internal `heading3Prefix` regex is now anchored
  to the start of a line (multiline mode), and fenced code blocks are
  stripped from `[Unreleased]` bodies before classification. Before this
  fix, an `### Added` entry whose body documented `### Changed` / `### Removed`
  in an inline code span — and happened to mention `BREAKING CHANGE` while
  describing the marker rule — would be misclassified as a major bump.
  `ValidateSection` benefits from the same protection.

## [[v1.1.1](https://github.com/C2FO/releasegen/releases/tag/v1.1.1)] - 2026-06-18
### Documentation
- Documented the classic-branch-protection setup gap in the GitHub App Setup
  section: the app must be added to both the "Allow specified actors to bypass
  required pull requests" list and the "Restrict who can push" allowlist, or
  the push fails with `protected branch hook declined`. Rulesets only need a
  single bypass entry.

## [[v1.1.0](https://github.com/C2FO/releasegen/releases/tag/v1.1.0)] - 2026-06-18
### Security
- Update dependenices to resoolve dependabot security alerts

## [[v1.0.1](https://github.com/C2FO/releasegen/releases/tag/v1.0.1)] - 2026-06-17
### Fixed
- Added a step that computes a lowercased image name once using bash parameter expansion

## [[v1.0.0](https://github.com/C2FO/releasegen/releases/tag/v1.0.0)] - 2026-06-17
### Fixed
- Self-release detection now recognizes releasegen running from the repository
  root. `RELEASEGEN_SELF_MODULE` defaults to the root module (empty path) and the
  feature is gated on `RELEASEGEN_SELF_REPO`, so the released version is printed
  to stdout and downstream steps (e.g. the Docker build/push) are no longer skipped.
### Changed
- **BREAKING CHANGE** - No change, just bumping to v1.0.0.

## [[v0.1.0](https://github.com/C2FO/releasegen/releases/tag/v0.1.0)] - 2026-06-16
### Added
- Initial release of the project.
