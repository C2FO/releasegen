# ReleaseGen v2 Architecture

This document maps each section of [PRD.md](./PRD.md) to the package(s)
that implement it. Use it as a starting point when navigating the codebase.

## Layout

```
releasegen/
├── cmd/releasegen/        # tiny CLI entrypoint (Cobra)
└── internal/
    ├── config/            # typed Config, env+flag parsing, validation, BumpType
    ├── changelog/         # pure parser, classifier, rewriter, Update
    ├── logging/           # slog handler with GitHub Actions awareness
    ├── vcs/               # Repo interface + go-git implementation (Open, GitRepo)
    ├── forge/             # Releaser interface + GitHub implementation
    ├── discovery/         # CHANGELOG.md walker, exclude rules, module resolution
    └── runner/            # per-module orchestration: discover -> rewrite -> commit/tag/push -> publish
```

The CLI in `cmd/releasegen` is the only place that reads environment
variables and constructs concrete types; the rest of the code is wired
through interfaces (`vcs.Repo`, `forge.Releaser`) for testability.

## PRD section -> package map

| PRD section                         | Package(s) implementing it                                              |
| ----------------------------------- | ----------------------------------------------------------------------- |
| §5.1 Discovery                      | `internal/discovery`, `internal/vcs` (`AllChangelogPaths`, `ReachableTags`, `IsChangelogModifiedSinceTag`) |
| §5.2 Version calculation            | `internal/changelog` (`ExtractUnreleased`, `ExtractCurrentVersion`, `Classify`, `NextVersion`) |
| §5.3 Custom change types            | `internal/config` (`ParseCustomTypes`, `BumpType`); `internal/changelog` (`Classify`) |
| §5.4 Manual version override        | `internal/config` (validation), `internal/changelog/Update` (footer + override), `internal/runner` (wiring) |
| §5.5 Changelog rewrite              | `internal/changelog/Rewrite`                                            |
| §5.6 Commit, tag, push              | `internal/vcs` (`GitRepo.CommitTagAndPush`)                             |
| §5.7 GitHub Release publication     | `internal/forge` (`GitHubReleaser.CreateRelease`)                       |
| §5.8 Multi-module runs              | `internal/runner` (`Runner.Run`)                                        |
| §5.9 Self-release awareness         | `internal/runner` (`Summary.ReleaseGenReleased/Version`); `cmd/releasegen` (prints to stdout) |
| §6 Inputs & configuration           | `internal/config` (`FromEnv`, `Validate`); `cmd/releasegen` (flag overrides) |
| §7 Outputs (logs, exit codes)       | `internal/logging`; `cmd/releasegen` (`exitCodeFor`)                    |
| §8 Constraints / guard-rails        | `internal/changelog` (`ErrIncompleteBreaking`, `ErrUnrecognizedChangeType`) |
| §9 Failure modes                    | All packages return wrapped errors; surfaced via slog at ERROR level    |

## Differences from v1

- Single `package main` with global env-derived vars -> typed `Config` injected from `cmd/releasegen`.
- `panic`/`recover` for ordinary errors -> returned errors with structured exit codes.
- `c2fo/vfs/v7` and `golang.org/x/oauth2` removed; `os` and
  `go-github.WithAuthToken` are sufficient.
- `bumpType string` -> typed `config.BumpType` enum with numeric priority.
- New `--dry-run`, `--summary-file`, `--version`, and per-env-var flag set.
- `internal/vcs` and `internal/forge` are interfaces -> the runner is fully
  unit-testable with fakes (see `internal/runner/runner_test.go`).
- Tag/changelog logic now context-aware (`context.Context` propagated end-to-end).

## Adding a new code-host backend (e.g. GitLab)

1. Implement `forge.Releaser` for GitLab in a new file under `internal/forge`.
2. Branch on a flag/env in `cmd/releasegen` to construct the right releaser.
3. The runner does not need to change.

## Adding a new VCS backend

The `vcs.Repo` interface is small (4 methods) — implement it against your
backend and inject it into `runner.Options`. The runner does not depend on
`go-git` directly.
