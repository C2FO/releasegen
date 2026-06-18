package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/suite"

	"github.com/c2fo/releasegen/internal/config"
	"github.com/c2fo/releasegen/internal/vcs"
)

type ValidateTestSuite struct {
	suite.Suite
	tmpDir string
	log    *slog.Logger
}

func TestValidateTestSuite(t *testing.T) {
	suite.Run(t, new(ValidateTestSuite))
}

func (s *ValidateTestSuite) SetupTest() {
	s.tmpDir = s.T().TempDir()
	// Discard logs so output stays clean while keeping handler shape realistic.
	s.log = slog.New(slog.NewTextHandler(io.Discard, nil))
}

func (s *ValidateTestSuite) writeChangelog(relPath, body string) {
	full := filepath.Join(s.tmpDir, relPath)
	s.Require().NoError(os.MkdirAll(filepath.Dir(full), 0o750))
	s.Require().NoError(os.WriteFile(full, []byte(body), 0o600))
}

func (s *ValidateTestSuite) TestNoChangelogs() {
	cfg := &config.Config{RepoRoot: s.tmpDir}
	s.Require().NoError(validatePaths(cfg, nil, s.log))
}

func (s *ValidateTestSuite) TestAllEmptyUnreleased() {
	s.writeChangelog("CHANGELOG.md", `# Changelog

## [Unreleased]

## [v1.0.0] - 2024-01-01
### Added
- thing
`)
	cfg := &config.Config{RepoRoot: s.tmpDir}
	s.Require().NoError(validatePaths(cfg, []string{"CHANGELOG.md"}, s.log))
}

func (s *ValidateTestSuite) TestValid_StandardHeadings() {
	s.writeChangelog("CHANGELOG.md", `## [Unreleased]
### Added
- new thing
### Fixed
- fixed thing
## [v0.1.0] - 2024-01-01
`)
	cfg := &config.Config{RepoRoot: s.tmpDir}
	s.Require().NoError(validatePaths(cfg, []string{"CHANGELOG.md"}, s.log))
}

func (s *ValidateTestSuite) TestInvalid_ChangedWithoutBreakingMarker() {
	s.writeChangelog("CHANGELOG.md", `## [Unreleased]
### Changed
- changed something without saying it was breaking
## [v0.1.0] - 2024-01-01
`)
	cfg := &config.Config{RepoRoot: s.tmpDir}
	err := validatePaths(cfg, []string{"CHANGELOG.md"}, s.log)
	s.Require().Error(err)
	var cliErr cliError
	s.Require().ErrorAs(err, &cliErr)
	s.Equal(exitChangelogErr, cliErr.code)
	s.Contains(err.Error(), "CHANGELOG.md")
}

func (s *ValidateTestSuite) TestInvalid_UnknownHeading() {
	s.writeChangelog("CHANGELOG.md", `## [Unreleased]
### Whimsy
- not a real heading
## [v0.1.0] - 2024-01-01
`)
	cfg := &config.Config{RepoRoot: s.tmpDir}
	err := validatePaths(cfg, []string{"CHANGELOG.md"}, s.log)
	s.Require().Error(err)
	var cliErr cliError
	s.Require().ErrorAs(err, &cliErr)
	s.Equal(exitChangelogErr, cliErr.code)
}

func (s *ValidateTestSuite) TestCustomHeadingAccepted() {
	s.writeChangelog("CHANGELOG.md", `## [Unreleased]
### Documentation
- improved the docs
## [v0.1.0] - 2024-01-01
`)
	cfg := &config.Config{
		RepoRoot:    s.tmpDir,
		CustomTypes: map[string]config.BumpType{"documentation": config.BumpPatch},
	}
	s.Require().NoError(validatePaths(cfg, []string{"CHANGELOG.md"}, s.log))
}

func (s *ValidateTestSuite) TestBatchesMultipleErrorsWithinOneFile() {
	// One changelog with TWO distinct problems: ### Changed without the
	// BREAKING CHANGE marker AND an unknown ### Whimsy heading. Both must
	// surface in a single validate run.
	s.writeChangelog("CHANGELOG.md", `## [Unreleased]
### Changed
- silently breaking, no marker
### Whimsy
- bogus heading
## [v0.1.0] - 2024-01-01
`)
	cfg := &config.Config{RepoRoot: s.tmpDir}
	err := validatePaths(cfg, []string{"CHANGELOG.md"}, s.log)
	s.Require().Error(err)
	var cliErr cliError
	s.Require().ErrorAs(err, &cliErr)
	s.Equal(exitChangelogErr, cliErr.code)
	s.Contains(err.Error(), "BREAKING CHANGE")
	s.Contains(err.Error(), "Whimsy")
	s.Contains(err.Error(), "2 changelog problem(s)")
}

func (s *ValidateTestSuite) TestBatchesMultipleErrors() {
	// Two changelogs, both broken in different ways. validate must surface
	// BOTH errors, not just the first.
	s.writeChangelog("services/api/CHANGELOG.md", `## [Unreleased]
### Changed
- silently breaking, no marker
## [services/api/v0.1.0] - 2024-01-01
`)
	s.writeChangelog("worker/CHANGELOG.md", `## [Unreleased]
### Whimsy
- bogus heading
## [worker/v0.1.0] - 2024-01-01
`)
	cfg := &config.Config{RepoRoot: s.tmpDir}
	err := validatePaths(cfg, []string{
		"services/api/CHANGELOG.md",
		"worker/CHANGELOG.md",
	}, s.log)
	s.Require().Error(err)
	s.Contains(err.Error(), "services/api/CHANGELOG.md")
	s.Contains(err.Error(), "worker/CHANGELOG.md")
}

func (s *ValidateTestSuite) TestNewValidateCmdRegistered() {
	root := newRootCmd()
	var found *cobra.Command
	for _, sub := range root.Commands() {
		if sub.Name() != "validate" {
			continue
		}
		found = sub
		break
	}
	s.Require().NotNil(found, "validate subcommand must be registered on the root command")
	s.NotNil(found.Flags().Lookup("repo-root"))
	s.NotNil(found.Flags().Lookup("custom-change-types"))
	s.NotNil(found.Flags().Lookup("exclude-dirs"))
	s.NotNil(found.Flags().Lookup("debug"))
	s.NotNil(found.Flags().Lookup("require-changelog-entry"))
	s.NotNil(found.Flags().Lookup("base-ref"))
}

// requireEntryFixture sets up a real git repo with one root module, one
// sub-module ("svc/"), and a base commit that establishes the "before"
// snapshot. The returned closure mutates HEAD per scenario and returns the
// opened vcs.GitRepo plus its discovered changelog paths.
type requireEntryFixture struct {
	dir      string
	wt       *git.Worktree
	sig      *object.Signature
	repoOpen func() *vcs.GitRepo
	suite    *ValidateTestSuite
}

func (s *ValidateTestSuite) newRequireEntryFixture() *requireEntryFixture {
	dir := s.T().TempDir()
	r, err := git.PlainInit(dir, false)
	s.Require().NoError(err)
	wt, err := r.Worktree()
	s.Require().NoError(err)
	sig := &object.Signature{Name: "tester", Email: "t@example.com", When: time.Now()}

	write := func(rel, body string) {
		s.Require().NoError(os.MkdirAll(filepath.Join(dir, filepath.Dir(rel)), 0o750))
		s.Require().NoError(os.WriteFile(filepath.Join(dir, rel), []byte(body), 0o600))
		_, err := wt.Add(rel)
		s.Require().NoError(err)
	}
	// Base state: root + svc module, each with empty [Unreleased].
	write("main.go", "package x\n")
	write("CHANGELOG.md", "# Changelog\n\n## [Unreleased]\n")
	write("svc/foo.go", "package svc\n")
	write("svc/CHANGELOG.md", "# Changelog\n\n## [Unreleased]\n")
	baseHash, err := wt.Commit("base", &git.CommitOptions{Author: sig})
	s.Require().NoError(err)
	_, err = r.CreateTag("base-tag", baseHash, &git.CreateTagOptions{Tagger: sig, Message: "base"})
	s.Require().NoError(err)

	return &requireEntryFixture{
		dir: dir, wt: wt, sig: sig, suite: s,
		repoOpen: func() *vcs.GitRepo {
			g, err := vcs.Open(dir, "main", slog.New(slog.DiscardHandler))
			s.Require().NoError(err)
			return g
		},
	}
}

func (f *requireEntryFixture) write(rel, body string) {
	full := filepath.Join(f.dir, rel)
	f.suite.Require().NoError(os.MkdirAll(filepath.Dir(full), 0o750))
	f.suite.Require().NoError(os.WriteFile(full, []byte(body), 0o600))
	_, err := f.wt.Add(rel)
	f.suite.Require().NoError(err)
}

func (f *requireEntryFixture) commit(msg string) {
	_, err := f.wt.Commit(msg, &git.CommitOptions{Author: f.sig})
	f.suite.Require().NoError(err)
}

func (s *ValidateTestSuite) TestRequireChangelogEntry_FailsWithoutEntry() {
	f := s.newRequireEntryFixture()
	// Modify svc source code but DON'T touch svc's CHANGELOG.md.
	f.write("svc/foo.go", "package svc\n// updated\n")
	f.commit("forgot the changelog")

	repo := f.repoOpen()
	cfg := &config.Config{
		RepoRoot:              f.dir,
		RequireChangelogEntry: true,
		BaseRef:               "base-tag",
	}
	paths := []string{"CHANGELOG.md", "svc/CHANGELOG.md"}
	err := validateAll(context.Background(), cfg, paths, repo, s.log)
	s.Require().Error(err)
	s.Contains(err.Error(), "svc/CHANGELOG.md")
	s.Contains(err.Error(), "[Unreleased] section gained no new lines")
}

func (s *ValidateTestSuite) TestRequireChangelogEntry_PassesWithEntry() {
	f := s.newRequireEntryFixture()
	f.write("svc/foo.go", "package svc\n// updated\n")
	f.write("svc/CHANGELOG.md", "# Changelog\n\n## [Unreleased]\n### Added\n- did the thing\n")
	f.commit("with changelog")

	repo := f.repoOpen()
	cfg := &config.Config{
		RepoRoot:              f.dir,
		RequireChangelogEntry: true,
		BaseRef:               "base-tag",
	}
	paths := []string{"CHANGELOG.md", "svc/CHANGELOG.md"}
	s.NoError(validateAll(context.Background(), cfg, paths, repo, s.log))
}

func (s *ValidateTestSuite) TestRequireChangelogEntry_RootCatchesUnclaimedFiles() {
	f := s.newRequireEntryFixture()
	// Modify a root-level file (not under svc/) without touching the root
	// CHANGELOG.md. The root module should be flagged; svc should not.
	f.write("main.go", "package x\n// updated\n")
	f.commit("root code change, no changelog")

	repo := f.repoOpen()
	cfg := &config.Config{
		RepoRoot:              f.dir,
		RequireChangelogEntry: true,
		BaseRef:               "base-tag",
	}
	paths := []string{"CHANGELOG.md", "svc/CHANGELOG.md"}
	err := validateAll(context.Background(), cfg, paths, repo, s.log)
	s.Require().Error(err)
	s.Contains(err.Error(), "CHANGELOG.md")
	s.NotContains(err.Error(), "svc/CHANGELOG.md")
}

func (s *ValidateTestSuite) TestRequireChangelogEntry_DisabledByDefault() {
	f := s.newRequireEntryFixture()
	f.write("svc/foo.go", "package svc\n// updated\n")
	f.commit("no entry, but check is off")

	repo := f.repoOpen()
	cfg := &config.Config{RepoRoot: f.dir} // RequireChangelogEntry zero value
	paths := []string{"CHANGELOG.md", "svc/CHANGELOG.md"}
	s.NoError(validateAll(context.Background(), cfg, paths, repo, s.log))
}
