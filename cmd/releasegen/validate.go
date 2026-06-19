package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/c2fo/releasegen/internal/changelog"
	"github.com/c2fo/releasegen/internal/config"
	"github.com/c2fo/releasegen/internal/discovery"
	"github.com/c2fo/releasegen/internal/logging"
	"github.com/c2fo/releasegen/internal/vcs"
)

// newValidateCmd returns the `releasegen validate` subcommand. It is the
// no-side-effects PR-time check: discover every CHANGELOG.md tracked by the
// repo, parse each [Unreleased] section, and classify it. Any malformed
// section (unknown heading, breaking heading without the BREAKING CHANGE
// marker, no recognized change types) is reported with its file path, and
// the process exits with the changelog error code (2).
//
// Crucially, no module needs to *have* changes for validation to succeed —
// an empty [Unreleased] section is fine. The whole-repo "you didn't add any
// changelog entry" check is left to surrounding CI (e.g. an ensure_changelog
// step) so this command can run safely on any PR, including those that
// legitimately don't touch a changelog.
func newValidateCmd() *cobra.Command {
	var (
		repoRoot              string
		excludeDirs           string
		customTypes           string
		debug                 bool
		requireChangelogEntry bool
		baseRef               string
	)

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate every CHANGELOG.md without writing anything",
		Long: "Validate scans the configured repository for CHANGELOG.md files, " +
			"parses each [Unreleased] section, and reports any malformed " +
			"headings (e.g. ### Changed without the BREAKING CHANGE marker, " +
			"or an unknown heading not declared in CUSTOM_CHANGE_TYPES). " +
			"It does not commit, push, tag, or contact GitHub.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := buildValidateConfig(cmd, validateFlagValues{
				repoRoot:              repoRoot,
				excludeDirs:           excludeDirs,
				customTypes:           customTypes,
				debug:                 debug,
				requireChangelogEntry: requireChangelogEntry,
				baseRef:               baseRef,
			})
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			defer cancel()

			level := slog.LevelInfo
			if cfg.Debug {
				level = slog.LevelDebug
			}
			ci := logging.DetectCI()
			log := logging.New(logging.Options{Writer: os.Stderr, Level: level, CI: ci})

			// We only need the changelog-listing capability of the repo, but
			// vcs.Open requires a branch to scope tag walks. Use HEAD as a
			// stable fallback so validate works in arbitrary checkouts
			// (e.g. detached-HEAD PR checks where GITHUB_REF_NAME is the PR
			// merge ref rather than a real branch).
			branch := cfg.Branch
			if branch == "" {
				branch = "HEAD"
			}
			repo, err := vcs.Open(cfg.RepoRoot, branch, log)
			if err != nil {
				return cliError{code: exitVCSErr, err: err}
			}

			paths, err := repo.AllChangelogPaths(ctx)
			if err != nil {
				return cliError{code: exitVCSErr, err: err}
			}
			paths = discovery.RemoveExcluded(paths, cfg.ExcludeDirs)

			return validateAll(ctx, cfg, paths, repo, log)
		},
	}

	cmd.Flags().StringVar(&repoRoot, "repo-root", "", "path to the git working tree (overrides REPO_ROOT, defaults to \".\")")
	cmd.Flags().StringVar(&excludeDirs, "exclude-dirs", "",
		"comma- or newline-separated directory prefixes to exclude (overrides EXCLUDE_DIRS and any .releasegen.yaml value)")
	cmd.Flags().StringVar(&customTypes, "custom-change-types", "",
		"newline-separated <heading>:<bump> pairs (overrides CUSTOM_CHANGE_TYPES and any .releasegen.yaml value)")
	cmd.Flags().BoolVar(&debug, "debug", false, "verbose logging (overrides DEBUG env var)")
	cmd.Flags().BoolVar(&requireChangelogEntry, "require-changelog-entry", false,
		"fail when a module's non-CHANGELOG files changed but its [Unreleased] section gained no new lines vs --base-ref "+
			"(overrides RELEASEGEN_REQUIRE_CHANGELOG_ENTRY and .releasegen.yaml validate.require_changelog_entry)")
	cmd.Flags().StringVar(&baseRef, "base-ref", "",
		"git revision to diff against for --require-changelog-entry "+
			"(defaults to origin/$GITHUB_BASE_REF on PR runs, else origin/main; "+
			"overrides RELEASEGEN_BASE_REF and .releasegen.yaml validate.base_ref)")
	return cmd
}

// validateFlagValues is the closure-captured flag state passed into
// buildValidateConfig. Kept as a tiny struct so the helper signature does
// not balloon with positional bool/string parameters.
type validateFlagValues struct {
	repoRoot              string
	excludeDirs           string
	customTypes           string
	debug                 bool
	requireChangelogEntry bool
	baseRef               string
}

// buildValidateConfig assembles the fully-resolved Config for the validate
// subcommand: env -> file -> flag, with the cliError wrapping callers
// expect. Extracting it keeps RunE thin and well under the cyclo-budget.
func buildValidateConfig(cmd *cobra.Command, fv validateFlagValues) (*config.Config, error) {
	cfg, err := config.FromEnv()
	if err != nil {
		return nil, cliError{code: exitConfigErr, err: err}
	}
	if fv.repoRoot != "" {
		cfg.RepoRoot = fv.repoRoot
	}
	fc, _, err := config.LoadFile(cfg.RepoRoot)
	if err != nil {
		return nil, cliError{code: exitConfigErr, err: err}
	}
	if err := config.ApplyFile(cfg, fc); err != nil {
		return nil, cliError{code: exitConfigErr, err: err}
	}
	if fv.excludeDirs != "" {
		cfg.ExcludeDirs = config.ParseExcludeDirs(fv.excludeDirs)
	}
	if fv.customTypes != "" {
		parsed, err := config.ParseCustomTypes(fv.customTypes)
		if err != nil {
			return nil, cliError{code: exitConfigErr, err: fmt.Errorf("--custom-change-types: %w", err)}
		}
		cfg.CustomTypes = parsed
	}
	if fv.debug {
		cfg.Debug = true
	}
	// Flags win unconditionally over file/env, but only when the user
	// actually passed them; cobra exposes that via Changed.
	if cmd.Flags().Changed("require-changelog-entry") {
		cfg.RequireChangelogEntry = fv.requireChangelogEntry
	}
	if cmd.Flags().Changed("base-ref") {
		cfg.BaseRef = fv.baseRef
	}
	if err := cfg.Validate(); err != nil {
		return nil, cliError{code: exitConfigErr, err: err}
	}
	return cfg, nil
}

// validateAll orchestrates both validation phases: the content check (every
// [Unreleased] section is well-formed) and, when enabled, the diff-aware
// "you can't merge code without a changelog entry" check. Problems from both
// phases are batched into a single error so the operator sees everything in
// one CI run.
func validateAll(
	ctx context.Context,
	cfg *config.Config,
	paths []string,
	repo *vcs.GitRepo,
	log *slog.Logger,
) error {
	contentProblems := collectContentProblems(cfg, paths, log)

	var entryProblems []changelogProblem
	if cfg.RequireChangelogEntry {
		base := cfg.BaseRef
		if base == "" {
			base = config.DefaultBaseRef()
		}
		log.Debug("require_changelog_entry enabled", "base_ref", base)
		var err error
		entryProblems, err = collectEntryProblems(ctx, paths, repo, base, log)
		if err != nil {
			return cliError{code: exitVCSErr, err: err}
		}
	}

	problems := append(contentProblems, entryProblems...) //nolint:gocritic // intentional new slice
	return reportProblems(cfg, problems, paths, log)
}

// collectContentProblems is the pure (no-I/O-except-reads) per-file
// validation that catches malformed [Unreleased] sections. Kept exported to
// tests via validatePaths below.
func collectContentProblems(cfg *config.Config, paths []string, log *slog.Logger) []changelogProblem {
	if len(paths) == 0 {
		log.Warn("no changelog files found", "repo_root", cfg.RepoRoot)
		return nil
	}

	var problems []changelogProblem
	for _, p := range paths {
		abs, err := filepath.Abs(filepath.Join(cfg.RepoRoot, p))
		if err != nil {
			problems = append(problems, changelogProblem{
				path: p,
				err:  fmt.Errorf("resolve path: %w", err),
			})
			continue
		}
		content, err := os.ReadFile(abs) //nolint:gosec // path comes from repo discovery
		if err != nil {
			problems = append(problems, changelogProblem{
				path: p,
				err:  fmt.Errorf("read: %w", err),
			})
			continue
		}
		section := changelog.ExtractUnreleased(string(content))
		if section == "" {
			// Empty [Unreleased] is fine here; the diff-aware
			// require_changelog_entry check enforces presence separately.
			log.Debug("no unreleased changes", "path", p)
			continue
		}
		for _, err := range changelog.ValidateSection(section, cfg.CustomTypes) {
			problems = append(problems, changelogProblem{path: p, err: err})
		}
	}
	return problems
}

// validatePaths is kept as a thin shim for existing tests that exercise the
// pure content-validation path. It returns the same batched cliError shape
// as the full command. Production code should use validateAll instead.
func validatePaths(cfg *config.Config, paths []string, log *slog.Logger) error {
	return reportProblems(cfg, collectContentProblems(cfg, paths, log), paths, log)
}

// collectEntryProblems implements the "you can't merge code without a
// changelog entry" guard. It groups every changed file into the deepest
// module that owns it (modules are defined by where CHANGELOG.md lives,
// with the root changelog catching everything not claimed by a sub-module
// changelog) and, for each module whose non-CHANGELOG files changed,
// requires that module's [Unreleased] section to have gained content vs the
// base ref. Problems are returned, not raised, so the caller can batch
// them with content-validation problems in one report.
func collectEntryProblems(
	ctx context.Context,
	paths []string,
	repo *vcs.GitRepo,
	base string,
	log *slog.Logger,
) ([]changelogProblem, error) {
	changed, err := repo.ChangedFiles(ctx, base)
	if err != nil {
		return nil, err
	}
	log.Debug("changed files vs base", "base_ref", base, "count", len(changed))
	if len(changed) == 0 {
		return nil, nil
	}

	// modulePrefixes maps "submodule/" -> "submodule/CHANGELOG.md" (and "" -> "CHANGELOG.md"
	// when a root changelog exists). The empty prefix is the catch-all and
	// must always be matched LAST so deeper modules win.
	modulePrefixes := make(map[string]string, len(paths))
	hasRoot := false
	for _, cl := range paths {
		dir := filepath.ToSlash(filepath.Dir(cl))
		if dir == "." || dir == "" {
			modulePrefixes[""] = cl
			hasRoot = true
			continue
		}
		modulePrefixes[dir+"/"] = cl
	}

	// For each module, track whether a non-CHANGELOG file changed.
	type moduleState struct {
		changelog       string
		nonChangelogHit bool
	}
	states := make(map[string]*moduleState, len(modulePrefixes))
	for prefix, cl := range modulePrefixes {
		states[prefix] = &moduleState{changelog: cl}
	}

	for _, f := range changed {
		f = filepath.ToSlash(f)
		owner := assignModule(f, modulePrefixes, hasRoot)
		st, ok := states[owner]
		if !ok {
			// File outside any known module (only possible when no root
			// changelog exists). Skip — there is nothing for us to require.
			continue
		}
		if f != st.changelog {
			st.nonChangelogHit = true
		}
	}

	var problems []changelogProblem
	for prefix, st := range states {
		if !st.nonChangelogHit {
			continue
		}
		gained, err := unreleasedGained(ctx, repo, base, st.changelog)
		if err != nil {
			problems = append(problems, changelogProblem{
				path: st.changelog,
				err:  fmt.Errorf("compare [Unreleased] vs %s: %w", base, err),
			})
			continue
		}
		if gained {
			continue
		}
		problems = append(problems, changelogProblem{
			path: st.changelog,
			err: fmt.Errorf(
				"module %q has non-CHANGELOG changes vs %s but its [Unreleased] section gained no new lines",
				prefixOrRoot(prefix), base,
			),
		})
	}
	return problems, nil
}

// assignModule returns the prefix of the deepest module that owns f. When
// no sub-module matches and a root changelog exists, it returns "" (the
// root). When no root changelog exists, files outside any sub-module are
// reported as belonging to "<unowned>" so the caller can skip them.
func assignModule(f string, prefixes map[string]string, hasRoot bool) string {
	best := ""
	bestLen := -1
	for prefix := range prefixes {
		if prefix == "" {
			continue
		}
		if strings.HasPrefix(f, prefix) && len(prefix) > bestLen {
			best = prefix
			bestLen = len(prefix)
		}
	}
	if bestLen >= 0 {
		return best
	}
	if hasRoot {
		return ""
	}
	return "<unowned>"
}

// unreleasedGained reports whether the [Unreleased] section of changelogPath
// has more non-whitespace lines in the *next commit* than it did at base.
// A net-zero or shrinking change counts as "no new content," which catches
// both the "didn't touch the changelog" case and the (rarer) "edited a
// versioned section but not [Unreleased]" case.
//
// We deliberately read the index (staged) version rather than the working
// tree: when this runs as a pre-commit hook, only what is staged will be
// committed, and we want validation to predict the post-commit state. An
// unstaged worktree edit to CHANGELOG.md will not survive the commit, so
// it must not satisfy this check — otherwise developers can silently
// commit code without their changelog updates and only discover the gap
// in CI.
func unreleasedGained(
	ctx context.Context,
	repo *vcs.GitRepo,
	base, changelogPath string,
) (bool, error) {
	baseBody, err := repo.FileAtRef(ctx, base, changelogPath)
	if err != nil {
		return false, err
	}
	headBody, err := repo.FileAtIndex(ctx, changelogPath)
	if err != nil {
		return false, fmt.Errorf("read %s from index: %w", changelogPath, err)
	}
	headLines := countNonWhitespaceLines(changelog.ExtractUnreleased(headBody))
	baseLines := countNonWhitespaceLines(changelog.ExtractUnreleased(baseBody))
	return headLines > baseLines, nil
}

func countNonWhitespaceLines(s string) int {
	if s == "" {
		return 0
	}
	n := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

func prefixOrRoot(prefix string) string {
	if prefix == "" {
		return "."
	}
	return strings.TrimSuffix(prefix, "/")
}

// reportProblems folds the (possibly empty) slice of problems into the
// success-or-cliError shape both validate paths share.
func reportProblems(
	cfg *config.Config,
	problems []changelogProblem,
	paths []string,
	log *slog.Logger,
) error {
	if len(problems) == 0 {
		log.Info("changelog validation passed",
			"repo_root", cfg.RepoRoot,
			"changelogs_found", len(paths),
		)
		return nil
	}
	parts := make([]string, 0, len(problems))
	for _, pr := range problems {
		log.Error("changelog validation failed", "path", pr.path, "err", pr.err.Error())
		parts = append(parts, fmt.Sprintf("  %s: %s", pr.path, pr.err.Error()))
	}
	return cliError{
		code: exitChangelogErr,
		err: fmt.Errorf("%d changelog problem(s):\n%s",
			len(problems),
			strings.Join(parts, "\n"),
		),
	}
}

type changelogProblem struct {
	path string
	err  error
}
