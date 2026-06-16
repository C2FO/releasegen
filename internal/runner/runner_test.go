package runner_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/c2fo/releasegen/internal/config"
	"github.com/c2fo/releasegen/internal/forge"
	forgemocks "github.com/c2fo/releasegen/internal/forge/mocks"
	"github.com/c2fo/releasegen/internal/runner"
	"github.com/c2fo/releasegen/internal/vcs"
	vcsmocks "github.com/c2fo/releasegen/internal/vcs/mocks"
)

type RunnerTestSuite struct {
	suite.Suite
	tmpDir   string
	repo     *vcsmocks.Repo
	releaser *forgemocks.Releaser
	cfg      *config.Config
	now      time.Time
}

func TestRunnerTestSuite(t *testing.T) {
	suite.Run(t, new(RunnerTestSuite))
}

func (s *RunnerTestSuite) SetupTest() {
	s.tmpDir = s.T().TempDir()
	s.now = time.Date(2026, 4, 19, 0, 0, 0, 0, time.UTC)
	s.repo = vcsmocks.NewRepo(s.T())
	s.releaser = forgemocks.NewReleaser(s.T())
	s.cfg = &config.Config{
		Token:             "tok",
		OwnerRepo:         "owner/repo",
		Actor:             "tester",
		Branch:            "main",
		RepoRoot:          s.tmpDir,
		SelfReleaseModule: "releasegen",
		SelfReleaseRepo:   "c2fo/releasegen",
	}
}

// stageChangelog creates a changelog under tmpDir at relPath and registers
// the discovery expectations on the mock repo. It always reports the file
// as modified since the (absent) latest tag, since the runner-level tests
// always exercise the "needs release" path; the no-changes branch is
// covered by changelog-package tests.
func (s *RunnerTestSuite) stageChangelog(relPath, body string) {
	full := filepath.Join(s.tmpDir, relPath)
	s.Require().NoError(os.MkdirAll(filepath.Dir(full), 0o750))
	s.Require().NoError(os.WriteFile(full, []byte(body), 0o600))
	s.repo.EXPECT().AllChangelogPaths(mock.Anything).Return([]string{relPath}, nil).Once()
	s.repo.EXPECT().ReachableTags(mock.Anything).Return(nil, nil).Once()
	s.repo.EXPECT().
		IsChangelogModifiedSinceTag(mock.Anything, relPath, "").
		Return(true, nil).Once()
}

func (s *RunnerTestSuite) newRunner() *runner.Runner {
	return runner.New(runner.Options{
		Config:   s.cfg,
		Repo:     s.repo,
		Releaser: s.releaser,
		Logger:   slog.New(slog.DiscardHandler),
		Now:      func() time.Time { return s.now },
		Stderr:   nil,
	})
}

func (s *RunnerTestSuite) TestHappyPath_SingleModuleReleased() {
	s.stageChangelog("CHANGELOG.md", `## [Unreleased]
### Added
- new feature

## [v1.0.0] - 2024-01-01
`)
	s.repo.EXPECT().CommitTagAndPush(mock.Anything, mock.MatchedBy(func(o vcs.CommitTagPushOptions) bool {
		return o.Version == "1.1.0" && o.ChangelogPath == "CHANGELOG.md"
	})).Return(nil).Once()
	s.releaser.EXPECT().CreateRelease(mock.Anything, mock.MatchedBy(func(o forge.CreateReleaseOptions) bool {
		return o.TagName == "v1.1.0" && o.Owner == "owner" && o.Repo == "repo"
	})).Return(nil).Once()

	sum, err := s.newRunner().Run(context.Background())
	s.Require().NoError(err)
	s.Require().Len(sum.Modules, 1)
	res := sum.Modules[0]
	s.Equal(runner.StatusReleased, res.Status)
	s.Equal("1.1.0", res.NextVersion)
	s.Equal("v1.1.0", res.ReleaseName)
}

func (s *RunnerTestSuite) TestNewDefaultsLoggerWhenNil() {
	s.stageChangelog("CHANGELOG.md", `## [Unreleased]
### Added
- new feature

## [v1.0.0] - 2024-01-01
`)
	s.repo.EXPECT().CommitTagAndPush(mock.Anything, mock.Anything).Return(nil).Once()
	s.releaser.EXPECT().CreateRelease(mock.Anything, mock.Anything).Return(nil).Once()

	// A nil Logger must not cause a nil-pointer panic; New should default it.
	r := runner.New(runner.Options{
		Config:   s.cfg,
		Repo:     s.repo,
		Releaser: s.releaser,
		Logger:   nil,
		Now:      func() time.Time { return s.now },
		Stderr:   io.Discard,
	})
	sum, err := r.Run(context.Background())
	s.Require().NoError(err)
	s.Require().Len(sum.Modules, 1)
	s.Equal(runner.StatusReleased, sum.Modules[0].Status)
}

func (s *RunnerTestSuite) TestSkipsModuleWithEmptyUnreleased() {
	s.stageChangelog("sub/CHANGELOG.md", `## [Unreleased]

## [sub/v0.1.0] - 2024-01-01
`)

	sum, err := s.newRunner().Run(context.Background())
	s.Require().NoError(err)
	s.Require().Len(sum.Modules, 1)
	s.Equal(runner.StatusSkipped, sum.Modules[0].Status)
}

func (s *RunnerTestSuite) TestDryRunDoesNotCommitOrPublish() {
	s.cfg.DryRun = true
	s.stageChangelog("CHANGELOG.md", `## [Unreleased]
### Fixed
- bug
## [v1.0.0] - 2024-01-01
`)

	sum, err := s.newRunner().Run(context.Background())
	s.Require().NoError(err)
	s.Equal(runner.StatusDryRun, sum.Modules[0].Status)
	s.Equal("1.0.1", sum.Modules[0].NextVersion)

	got, err := os.ReadFile(filepath.Join(s.tmpDir, "CHANGELOG.md"))
	s.Require().NoError(err)
	s.Contains(string(got), "## [Unreleased]\n### Fixed")
}

func (s *RunnerTestSuite) TestForgeFailureBubblesAsModuleFailure() {
	s.stageChangelog("CHANGELOG.md", `## [Unreleased]
### Added
- x
## [v1.0.0] - 2024-01-01
`)
	s.repo.EXPECT().CommitTagAndPush(mock.Anything, mock.Anything).Return(nil).Once()
	s.releaser.EXPECT().CreateRelease(mock.Anything, mock.Anything).
		Return(errors.New("api down")).Once()

	sum, err := s.newRunner().Run(context.Background())
	s.Require().Error(err)
	s.Require().Len(sum.Modules, 1)
	s.Equal(runner.StatusFailed, sum.Modules[0].Status)
	s.Contains(sum.Modules[0].Error, "api down")
}

func (s *RunnerTestSuite) TestManualVersionIsHonored() {
	s.cfg.ManualVersion = "v9.9.9"
	s.cfg.Reason = "hotfix"
	s.stageChangelog("CHANGELOG.md", `## [Unreleased]
### Added
- x
## [v1.0.0] - 2024-01-01
`)
	s.repo.EXPECT().CommitTagAndPush(mock.Anything, mock.Anything).Return(nil).Once()
	s.releaser.EXPECT().CreateRelease(mock.Anything, mock.Anything).Return(nil).Once()

	sum, err := s.newRunner().Run(context.Background())
	s.Require().NoError(err)
	s.Equal("9.9.9", sum.Modules[0].NextVersion)
	s.True(sum.Modules[0].Manual)
}

func (s *RunnerTestSuite) TestSummaryFileWritten() {
	out := filepath.Join(s.tmpDir, "summary.json")
	s.cfg.SummaryFile = out
	s.stageChangelog("CHANGELOG.md", `## [Unreleased]
### Fixed
- x
## [v1.0.0] - 2024-01-01
`)
	s.repo.EXPECT().CommitTagAndPush(mock.Anything, mock.Anything).Return(nil).Once()
	s.releaser.EXPECT().CreateRelease(mock.Anything, mock.Anything).Return(nil).Once()

	_, err := s.newRunner().Run(context.Background())
	s.Require().NoError(err)
	data, err := os.ReadFile(out) //nolint:gosec // path written by the same test
	s.Require().NoError(err)
	s.Contains(string(data), `"next_version": "1.0.1"`)
}

func (s *RunnerTestSuite) TestReleaseGenSelfReleaseTracked() {
	s.cfg.OwnerRepo = "c2fo/releasegen"
	s.stageChangelog("releasegen/CHANGELOG.md", `## [Unreleased]
### Added
- x
## [releasegen/v1.0.0] - 2024-01-01
`)
	s.repo.EXPECT().CommitTagAndPush(mock.Anything, mock.Anything).Return(nil).Once()
	s.releaser.EXPECT().CreateRelease(mock.Anything, mock.Anything).Return(nil).Once()

	sum, err := s.newRunner().Run(context.Background())
	s.Require().NoError(err)
	s.True(sum.ReleaseGenReleased)
	s.Equal("1.1.0", sum.ReleaseGenVersion)
}
