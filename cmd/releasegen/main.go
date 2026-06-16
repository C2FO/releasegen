// Command releasegen is the entry point for the changelog-driven release tool.
//
// It is intentionally tiny: parse flags + env, build dependencies, hand off
// to internal/runner, translate the result into an exit code, and print the
// "self-release" version on stdout for downstream workflow steps.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/c2fo/releasegen/internal/changelog"
	"github.com/c2fo/releasegen/internal/config"
	"github.com/c2fo/releasegen/internal/forge"
	"github.com/c2fo/releasegen/internal/logging"
	"github.com/c2fo/releasegen/internal/runner"
	"github.com/c2fo/releasegen/internal/vcs"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

// Exit codes. Stable across runs so CI can branch on them.
const (
	exitOK           = 0
	exitConfigErr    = 1
	exitChangelogErr = 2
	exitVCSErr       = 3
	exitForgeErr     = 4
	exitInternal     = 10
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(exitCodeFor(err))
	}
}

func newRootCmd() *cobra.Command {
	var (
		repoRoot      string
		dryRun        bool
		debug         bool
		summaryFile   string
		manualVersion string
		reason        string
		excludeDirs   string
		customTypes   string
		ownerRepo     string
		actor         string
		branch        string
		token         string
		showVersion   bool
	)

	cmd := &cobra.Command{
		Use:          "releasegen",
		Short:        "Changelog-driven release tool for monorepos",
		Long:         "Releasegen promotes each CHANGELOG.md's [Unreleased] section into a tagged GitHub release. See README.md for details.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if showVersion {
				fmt.Println(version)
				return nil
			}

			cfg, err := config.FromEnv()
			if err != nil {
				return cliError{code: exitConfigErr, err: err}
			}

			if err := applyFlagOverrides(cfg, flagOverrides{
				repoRoot:      repoRoot,
				dryRun:        dryRun,
				debug:         debug,
				summaryFile:   summaryFile,
				manualVersion: manualVersion,
				reason:        reason,
				excludeDirs:   excludeDirs,
				customTypes:   customTypes,
				ownerRepo:     ownerRepo,
				actor:         actor,
				branch:        branch,
				token:         token,
			}); err != nil {
				return cliError{code: exitConfigErr, err: err}
			}

			if err := cfg.Validate(); err != nil {
				return cliError{code: exitConfigErr, err: err}
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
			defer cancel()

			level := slog.LevelInfo
			if cfg.Debug {
				level = slog.LevelDebug
			}
			ci := logging.DetectCI()
			log := logging.New(logging.Options{Writer: os.Stderr, Level: level, CI: ci})

			repo, err := vcs.Open(cfg.RepoRoot, cfg.Branch, log)
			if err != nil {
				return cliError{code: exitVCSErr, err: err}
			}
			releaser := forge.NewGitHubReleaser(cfg.Token)

			r := runner.New(runner.Options{
				Config:   cfg,
				Repo:     repo,
				Releaser: releaser,
				Logger:   log,
				CI:       ci,
				Stderr:   os.Stderr,
			})

			summary, err := r.Run(ctx)
			if summary != nil && summary.ReleaseGenReleased {
				fmt.Println(summary.ReleaseGenVersion)
			}
			return err
		},
	}

	cmd.Flags().BoolVar(&showVersion, "version", false, "print version and exit")
	cmd.Flags().StringVar(&repoRoot, "repo-root", "", "path to the git working tree (overrides REPO_ROOT, defaults to \".\")")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "compute and print actions without committing, pushing, tagging, or publishing")
	cmd.Flags().BoolVar(&debug, "debug", false, "verbose tag/discovery logging (overrides DEBUG env var)")
	cmd.Flags().StringVar(&summaryFile, "summary-file", "", "if set, write a JSON summary of the run to this path (overrides SUMMARY_FILE)")
	cmd.Flags().StringVar(&manualVersion, "manual-version", "", "explicit version override (overrides MANUAL_VERSION)")
	cmd.Flags().StringVar(&reason, "reason", "", "reason for a manual release (overrides REASON)")
	cmd.Flags().StringVar(&excludeDirs, "exclude-dirs", "",
		"comma- or newline-separated directory prefixes to exclude (overrides EXCLUDE_DIRS)")
	cmd.Flags().StringVar(&customTypes, "custom-change-types", "", "newline-separated <heading>:<bump> pairs (overrides CUSTOM_CHANGE_TYPES)")
	cmd.Flags().StringVar(&ownerRepo, "repository", "", "<owner>/<repo> (overrides GITHUB_REPOSITORY)")
	cmd.Flags().StringVar(&actor, "actor", "", "GitHub actor (overrides GITHUB_ACTOR)")
	cmd.Flags().StringVar(&branch, "branch", "", "release branch (overrides GITHUB_REF_NAME)")
	cmd.Flags().StringVar(&token, "token", "", "GitHub token (overrides GITHUB_TOKEN)")

	return cmd
}

type flagOverrides struct {
	repoRoot      string
	dryRun        bool
	debug         bool
	summaryFile   string
	manualVersion string
	reason        string
	excludeDirs   string
	customTypes   string
	ownerRepo     string
	actor         string
	branch        string
	token         string
}

func applyFlagOverrides(cfg *config.Config, f flagOverrides) error {
	if f.repoRoot != "" {
		cfg.RepoRoot = f.repoRoot
	}
	if f.dryRun {
		cfg.DryRun = true
	}
	if f.debug {
		cfg.Debug = true
	}
	if f.summaryFile != "" {
		cfg.SummaryFile = f.summaryFile
	}
	if f.manualVersion != "" {
		cfg.ManualVersion = f.manualVersion
	}
	if f.reason != "" {
		cfg.Reason = f.reason
	}
	if f.excludeDirs != "" {
		cfg.ExcludeDirs = config.ParseExcludeDirs(f.excludeDirs)
	}
	if f.customTypes != "" {
		parsed, err := config.ParseCustomTypes(f.customTypes)
		if err != nil {
			return fmt.Errorf("--custom-change-types: %w", err)
		}
		cfg.CustomTypes = parsed
	}
	if f.ownerRepo != "" {
		cfg.OwnerRepo = f.ownerRepo
	}
	if f.actor != "" {
		cfg.Actor = f.actor
	}
	if f.branch != "" {
		cfg.Branch = f.branch
	}
	if f.token != "" {
		cfg.Token = f.token
	}
	return nil
}

// cliError wraps an error with an explicit exit code.
type cliError struct {
	code int
	err  error
}

func (c cliError) Error() string { return c.err.Error() }
func (c cliError) Unwrap() error { return c.err }

func exitCodeFor(err error) int {
	if err == nil {
		return exitOK
	}
	var c cliError
	if errors.As(err, &c) {
		return c.code
	}
	switch {
	case errors.Is(err, changelog.ErrUnrecognizedChangeType),
		errors.Is(err, changelog.ErrIncompleteBreaking):
		return exitChangelogErr
	case errors.Is(err, vcs.ErrVCS):
		return exitVCSErr
	case errors.Is(err, forge.ErrForge):
		return exitForgeErr
	default:
		return exitInternal
	}
}
