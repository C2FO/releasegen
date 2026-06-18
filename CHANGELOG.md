# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
