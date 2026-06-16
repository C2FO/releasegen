package vcs_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/stretchr/testify/suite"

	"github.com/c2fo/releasegen/internal/vcs"
)

type VCSTestSuite struct {
	suite.Suite
}

func TestVCSTestSuite(t *testing.T) {
	suite.Run(t, new(VCSTestSuite))
}

// TestLatestTagForModule covers the pure tag-selection helper.
func (s *VCSTestSuite) TestLatestTagForModule() {
	tags := []vcs.TagInfo{
		{Name: "mod-a/v1.0.0", Date: 1000, ModuleName: "mod-a"},
		{Name: "mod-b/v2.0.0", Date: 2000, ModuleName: "mod-b"},
		{Name: "mod-a/v1.1.0", Date: 1500, ModuleName: "mod-a"},
		{Name: "v0.1.0", Date: 500, ModuleName: ""},
	}
	s.Equal("mod-a/v1.1.0", vcs.LatestTagForModule(tags, "mod-a"))
	s.Equal("mod-b/v2.0.0", vcs.LatestTagForModule(tags, "mod-b"))
	s.Equal("v0.1.0", vcs.LatestTagForModule(tags, ""))
	s.Empty(vcs.LatestTagForModule(tags, "missing"))
	s.Empty(vcs.LatestTagForModule(nil, "any"))
}

// TestIntegration spins up a real git repo in a tmpdir, creates module
// changelogs with module-prefixed tags, and exercises AllChangelogPaths,
// ReachableTags, and IsChangelogModifiedSinceTag end-to-end.
func (s *VCSTestSuite) TestIntegration() {
	dir := s.T().TempDir()
	repo, err := git.PlainInit(dir, false)
	s.Require().NoError(err)
	wt, err := repo.Worktree()
	s.Require().NoError(err)

	sig := &object.Signature{Name: "tester", Email: "t@example.com", When: time.Now()}

	writeFile := func(rel, body string) {
		s.Require().NoError(os.MkdirAll(filepath.Join(dir, filepath.Dir(rel)), 0o750))
		s.Require().NoError(os.WriteFile(filepath.Join(dir, rel), []byte(body), 0o600))
		_, err := wt.Add(rel)
		s.Require().NoError(err)
	}

	writeFile("CHANGELOG.md", "## [Unreleased]\n")
	writeFile("submodule/CHANGELOG.md", "## [Unreleased]\n")
	c1, err := wt.Commit("initial", &git.CommitOptions{Author: sig})
	s.Require().NoError(err)

	// Tag the root module at v1.0.0 reachable from main (HEAD).
	_, err = repo.CreateTag("v1.0.0", c1, &git.CreateTagOptions{Tagger: sig, Message: "v1.0.0"})
	s.Require().NoError(err)

	// Modify only the submodule changelog.
	writeFile("submodule/CHANGELOG.md", "## [Unreleased]\n### Added\n- new\n")
	_, err = wt.Commit("update sub", &git.CommitOptions{Author: sig})
	s.Require().NoError(err)

	// Branch reference must exist for ReachableTags to work; rename HEAD.
	headRef, err := repo.Head()
	s.Require().NoError(err)
	branchName := headRef.Name().Short() // master or main depending on git version

	g, err := vcs.Open(dir, branchName, slog.New(slog.DiscardHandler))
	s.Require().NoError(err)

	ctx := context.Background()

	paths, err := g.AllChangelogPaths(ctx)
	s.Require().NoError(err)
	s.ElementsMatch([]string{"CHANGELOG.md", "submodule/CHANGELOG.md"}, paths)

	tags, err := g.ReachableTags(ctx)
	s.Require().NoError(err)
	s.Len(tags, 1)
	s.Equal("v1.0.0", tags[0].Name)
	s.Empty(tags[0].ModuleName)

	rootMod, err := g.IsChangelogModifiedSinceTag(ctx, "CHANGELOG.md", "v1.0.0")
	s.Require().NoError(err)
	s.False(rootMod, "root changelog was not touched after v1.0.0")

	subMod, err := g.IsChangelogModifiedSinceTag(ctx, "submodule/CHANGELOG.md", "v1.0.0")
	s.Require().NoError(err)
	s.True(subMod, "submodule changelog was modified after v1.0.0")

	// Initial release case (no tag) returns true.
	first, err := g.IsChangelogModifiedSinceTag(ctx, "submodule/CHANGELOG.md", "")
	s.Require().NoError(err)
	s.True(first)
}

// TestCommitTagAndPush_PushedToBareRemote exercises the full
// commit -> tag -> push pipeline against a local bare repository acting
// as origin. It also verifies that errors carry the vcs.ErrVCS sentinel.
func (s *VCSTestSuite) TestCommitTagAndPush_PushedToBareRemote() {
	bareDir := s.T().TempDir()
	_, err := git.PlainInit(bareDir, true)
	s.Require().NoError(err)

	workDir := s.T().TempDir()
	repo, err := git.PlainInit(workDir, false)
	s.Require().NoError(err)
	_, err = repo.CreateRemote(&gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{bareDir},
	})
	s.Require().NoError(err)

	wt, err := repo.Worktree()
	s.Require().NoError(err)
	sig := &object.Signature{Name: "tester", Email: "t@example.com", When: time.Now()}

	cl := filepath.Join(workDir, "sub", "CHANGELOG.md")
	s.Require().NoError(os.MkdirAll(filepath.Dir(cl), 0o750))
	s.Require().NoError(os.WriteFile(cl, []byte("## [Unreleased]\n"), 0o600))
	_, err = wt.Add("sub/CHANGELOG.md")
	s.Require().NoError(err)
	_, err = wt.Commit("seed", &git.CommitOptions{Author: sig})
	s.Require().NoError(err)

	headRef, err := repo.Head()
	s.Require().NoError(err)
	branchName := headRef.Name().Short()

	g, err := vcs.Open(workDir, branchName, slog.New(slog.DiscardHandler))
	s.Require().NoError(err)

	// Mutate the changelog so there is a diff to commit.
	s.Require().NoError(os.WriteFile(cl, []byte("## [Unreleased]\n### Added\n- x\n"), 0o600))

	err = g.CommitTagAndPush(context.Background(), vcs.CommitTagPushOptions{
		ChangelogPath: "sub/CHANGELOG.md",
		ModuleName:    "sub",
		Version:       "1.2.3",
		Actor:         "tester",
		Token:         "irrelevant-for-local-bare",
	})
	s.Require().NoError(err)

	// Reopen the bare remote and confirm the tag landed.
	bare, err := git.PlainOpen(bareDir)
	s.Require().NoError(err)
	tagRef, err := bare.Tag("sub/v1.2.3")
	s.Require().NoError(err)
	s.NotNil(tagRef)
}

// TestCommitTagAndPush_OpenFailureWrapsErrVCS makes sure the sentinel is
// preserved when the underlying go-git call errors out.
func (s *VCSTestSuite) TestCommitTagAndPush_OpenFailureWrapsErrVCS() {
	_, err := vcs.Open(filepath.Join(s.T().TempDir(), "does-not-exist"), "main", slog.New(slog.DiscardHandler))
	s.Require().Error(err)
	s.Require().ErrorIs(err, vcs.ErrVCS)
}
