// Package runner orchestrates the per-module release pipeline:
// discover -> rewrite -> commit/tag/push -> publish.
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/c2fo/releasegen/internal/changelog"
	"github.com/c2fo/releasegen/internal/config"
	"github.com/c2fo/releasegen/internal/discovery"
	"github.com/c2fo/releasegen/internal/forge"
	"github.com/c2fo/releasegen/internal/logging"
	"github.com/c2fo/releasegen/internal/vcs"
)

// Status describes the outcome for a single module.
type Status string

// Module pipeline outcomes.
const (
	StatusReleased Status = "released"
	StatusSkipped  Status = "skipped"
	StatusFailed   Status = "failed"
	StatusDryRun   Status = "dry-run"
)

// ModuleResult captures the outcome of a single module's pipeline.
type ModuleResult struct {
	Module        string `json:"module"`
	ChangelogPath string `json:"changelog_path"`
	Status        Status `json:"status"`
	NextVersion   string `json:"next_version,omitempty"`
	ReleaseName   string `json:"release_name,omitempty"`
	Bump          string `json:"bump,omitempty"`
	Manual        bool   `json:"manual,omitempty"`
	Error         string `json:"error,omitempty"`
}

// Summary is the aggregated outcome of an entire run.
type Summary struct {
	Modules            []ModuleResult `json:"modules"`
	StartedAt          time.Time      `json:"started_at"`
	FinishedAt         time.Time      `json:"finished_at"`
	ReleaseGenVersion  string         `json:"releasegen_version,omitempty"`
	ReleaseGenReleased bool           `json:"releasegen_released"`
}

// Runner orchestrates a single invocation.
type Runner struct {
	cfg        *config.Config
	repo       vcs.Repo
	releaser   forge.Releaser
	discoverer *discovery.Discoverer
	log        *slog.Logger
	now        func() time.Time
	ci         bool
	stderr     io.Writer
}

// Options bundles Runner dependencies.
type Options struct {
	Config   *config.Config
	Repo     vcs.Repo
	Releaser forge.Releaser
	Logger   *slog.Logger
	Now      func() time.Time
	CI       bool
	Stderr   io.Writer // for raw GitHub Actions group markers
}

// New constructs a Runner. Config, Repo, and Releaser are required. Logger,
// Now, and Stderr are optional and default to a stderr logger, time.Now, and
// os.Stderr respectively, so a zero-valued Logger never causes a nil panic.
func New(opts Options) *Runner {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	if opts.Logger == nil {
		opts.Logger = logging.New(logging.Options{Writer: opts.Stderr})
	}
	return &Runner{
		cfg:        opts.Config,
		repo:       opts.Repo,
		releaser:   opts.Releaser,
		discoverer: discovery.New(opts.Repo, opts.Config.ExcludeDirs),
		log:        opts.Logger,
		now:        opts.Now,
		ci:         opts.CI,
		stderr:     opts.Stderr,
	}
}

// Run executes the pipeline. It returns a Summary even when an error
// occurs partway through, so the caller can write a summary file or
// surface partial-success state.
//
// When SummaryFile is configured, it is written exactly once on every exit
// path (success, per-module failure, discovery failure, context cancel) so
// downstream automation can rely on its presence to inspect the run.
func (r *Runner) Run(ctx context.Context) (*Summary, error) {
	summary := &Summary{StartedAt: r.now()}
	defer func() {
		summary.FinishedAt = r.now()
		if r.cfg.SummaryFile == "" {
			return
		}
		if err := writeSummary(r.cfg.SummaryFile, summary); err != nil {
			r.log.Warn("failed to write summary file", "path", r.cfg.SummaryFile, "err", err.Error())
		}
	}()

	logging.Group(r.stderr, r.ci, "Discovering modified changelog files")
	candidates, err := r.discoverer.Find(ctx)
	logging.EndGroup(r.stderr, r.ci)
	if err != nil {
		return summary, fmt.Errorf("discover: %w", err)
	}
	r.log.Info("discovered changelogs", "count", len(candidates))

	for _, c := range candidates {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return summary, ctxErr
		}
		res, modErr := r.processModule(ctx, c)
		summary.Modules = append(summary.Modules, res)
		if modErr != nil {
			return summary, fmt.Errorf("module %s: %w", res.Module, modErr)
		}
		if r.isSelfRelease(c.ModuleName) && res.Status == StatusReleased {
			summary.ReleaseGenReleased = true
			summary.ReleaseGenVersion = res.NextVersion
		}
	}

	return summary, nil
}

// processModule runs the per-module pipeline. The error returned via
// res.err preserves wrapping (errors.Is/As work) while res.Error is the
// human-readable form for the JSON summary.
func (r *Runner) processModule(ctx context.Context, c discovery.Candidate) (ModuleResult, error) {
	now := r.now()
	res := ModuleResult{
		Module:        c.ModuleName,
		ChangelogPath: c.Path,
	}

	logging.Group(r.stderr, r.ci, "Handling "+c.Path)
	defer logging.EndGroup(r.stderr, r.ci)

	abs, err := filepath.Abs(filepath.Join(r.cfg.RepoRoot, c.Path))
	if err != nil {
		return r.fail(res, fmt.Errorf("absolute path: %w", err))
	}

	content, err := os.ReadFile(abs) //nolint:gosec // path comes from repo discovery
	if err != nil {
		return r.fail(res, fmt.Errorf("read changelog: %w", err))
	}

	upd, err := changelog.Update(changelog.UpdateRequest{
		Content:       string(content),
		ModuleName:    c.ModuleName,
		OwnerRepo:     r.cfg.OwnerRepo,
		CustomTypes:   r.cfg.CustomTypes,
		ManualVersion: strings.TrimPrefix(r.cfg.ManualVersion, "v"),
		ManualReason:  r.cfg.Reason,
		Actor:         r.cfg.Actor,
		Now:           now,
	})
	if errors.Is(err, changelog.ErrNoChangesDetected) {
		res.Status = StatusSkipped
		r.log.Info("skipping module, no changes detected", "module", c.ModuleName)
		return res, nil
	}
	if err != nil {
		return r.fail(res, err)
	}

	res.NextVersion = upd.NextVersion
	res.Bump = upd.Bump.String()
	res.Manual = upd.Manual
	res.ReleaseName = changelog.ReleaseName(c.ModuleName, upd.NextVersion)

	if r.cfg.DryRun {
		r.log.Info(
			"dry-run: would release",
			"module", c.ModuleName,
			"next_version", upd.NextVersion,
			"bump", upd.Bump.String(),
		)
		res.Status = StatusDryRun
		return res, nil
	}

	if err := os.WriteFile(abs, []byte(upd.NewContent), 0o600); err != nil {
		return r.fail(res, fmt.Errorf("write changelog: %w", err))
	}

	if err := r.repo.CommitTagAndPush(ctx, vcs.CommitTagPushOptions{
		ChangelogPath: c.Path,
		ModuleName:    c.ModuleName,
		Version:       upd.NextVersion,
		Actor:         r.cfg.Actor,
		Token:         r.cfg.Token,
	}); err != nil {
		return r.fail(res, err)
	}

	if err := r.releaser.CreateRelease(ctx, forge.CreateReleaseOptions{
		Owner:   r.cfg.Owner(),
		Repo:    r.cfg.Repo(),
		TagName: res.ReleaseName,
		Name:    fmt.Sprintf("[%s] - %s", res.ReleaseName, now.Format("2006-01-02")),
		Body:    upd.UnreleasedSection,
	}); err != nil {
		return r.fail(res, err)
	}

	res.Status = StatusReleased
	r.log.Info(
		"released",
		"module", c.ModuleName,
		"version", upd.NextVersion,
		"release", res.ReleaseName,
	)
	return res, nil
}

// fail records a failure on the result and returns the wrapped error so the
// caller can propagate it (preserving errors.Is/As targets).
func (r *Runner) fail(res ModuleResult, err error) (ModuleResult, error) {
	res.Status = StatusFailed
	res.Error = err.Error()
	return res, err
}

// isSelfRelease reports whether the named module is the releasegen module
// inside the configured self-release repository.
func (r *Runner) isSelfRelease(module string) bool {
	if r.cfg.SelfReleaseModule == "" || r.cfg.SelfReleaseRepo == "" {
		return false
	}
	return module == r.cfg.SelfReleaseModule &&
		strings.EqualFold(r.cfg.OwnerRepo, r.cfg.SelfReleaseRepo)
}

func writeSummary(path string, s *Summary) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
