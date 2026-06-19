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

	// Modify the submodule changelog and add a brand-new module changelog
	// that did not exist at the v1.0.0 tag.
	writeFile("submodule/CHANGELOG.md", "## [Unreleased]\n### Added\n- new\n")
	writeFile("newmod/CHANGELOG.md", "## [Unreleased]\n### Added\n- first\n")
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
	s.ElementsMatch([]string{"CHANGELOG.md", "submodule/CHANGELOG.md", "newmod/CHANGELOG.md"}, paths)

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

	// A changelog that did not exist at the tag but exists at HEAD counts as
	// modified (absent blob -> zero hash differs from the real blob).
	addedMod, err := g.IsChangelogModifiedSinceTag(ctx, "newmod/CHANGELOG.md", "v1.0.0")
	s.Require().NoError(err)
	s.True(addedMod, "newmod changelog was added after v1.0.0")

	// A changelog that exists in neither tree is unchanged (both zero hash).
	absent, err := g.IsChangelogModifiedSinceTag(ctx, "nonexistent/CHANGELOG.md", "v1.0.0")
	s.Require().NoError(err)
	s.False(absent, "a path absent from both trees is not a modification")

	// Initial release case (no tag) returns true.
	first, err := g.IsChangelogModifiedSinceTag(ctx, "submodule/CHANGELOG.md", "")
	s.Require().NoError(err)
	s.True(first)
}

func (s *VCSTestSuite) TestChangedFilesAndFileAtRef() {
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

	// Base: one source file, one changelog.
	writeFile("main.go", "package x\n")
	writeFile("CHANGELOG.md", "# Changelog\n\n## [Unreleased]\n")
	baseHash, err := wt.Commit("base", &git.CommitOptions{Author: sig})
	s.Require().NoError(err)
	// Tag the base commit so we have a stable revspec to diff against.
	_, err = repo.CreateTag("base-tag", baseHash, &git.CreateTagOptions{Tagger: sig, Message: "base"})
	s.Require().NoError(err)

	// HEAD: modify the source file and add a new file under a submodule.
	writeFile("main.go", "package x\n// changed\n")
	writeFile("submodule/foo.go", "package submodule\n")
	_, err = wt.Commit("work", &git.CommitOptions{Author: sig})
	s.Require().NoError(err)

	g, err := vcs.Open(dir, "main", slog.New(slog.DiscardHandler))
	s.Require().NoError(err)
	ctx := context.Background()

	changed, err := g.ChangedFiles(ctx, "base-tag")
	s.Require().NoError(err)
	s.ElementsMatch([]string{"main.go", "submodule/foo.go"}, changed)

	// FileAtRef returns the base version of main.go (no "// changed" line).
	atBase, err := g.FileAtRef(ctx, "base-tag", "main.go")
	s.Require().NoError(err)
	s.Equal("package x\n", atBase)

	// A file that didn't exist at base resolves to empty, not an error.
	atBaseNew, err := g.FileAtRef(ctx, "base-tag", "submodule/foo.go")
	s.Require().NoError(err)
	s.Empty(atBaseNew)

	// An unresolvable ref produces an ErrVCS-wrapped error.
	_, err = g.ChangedFiles(ctx, "no-such-ref")
	s.Require().Error(err)
	s.Require().ErrorIs(err, vcs.ErrVCS)

	// Pre-commit-style scenario: stage a brand-new file and modify an
	// existing one WITHOUT committing. ChangedFiles must still report
	// them — without this, prenup-style pre-commit validation would
	// silently pass on a developer who'd staged code without updating
	// the changelog.
	s.Require().NoError(os.WriteFile(filepath.Join(dir, "main.go"), []byte("package x\n// staged but not committed\n"), 0o600))
	_, err = wt.Add("main.go")
	s.Require().NoError(err)
	s.Require().NoError(os.WriteFile(filepath.Join(dir, "untracked.go"), []byte("package x\n"), 0o600))
	// Don't `git add` untracked.go — it stays untracked.

	changed, err = g.ChangedFiles(ctx, "base-tag")
	s.Require().NoError(err)
	s.Contains(changed, "main.go", "staged modification must be reported")
	s.Contains(changed, "submodule/foo.go", "previously committed change must still be reported")
	s.Contains(changed, "untracked.go", "untracked file (likely about to be added) must be reported")
}

// TestFileAtIndex covers the three states FileAtIndex must disambiguate
// to keep pre-commit validation honest:
//
//  1. A staged modification — the index hash points at the new blob, so
//     FileAtIndex must return the *staged* (next-commit) content, not the
//     working-tree content and not the HEAD content.
//  2. An unstaged worktree edit on an otherwise-tracked file — the index
//     still points at HEAD's blob, so FileAtIndex must return the HEAD
//     content. This is the bug that prompted the index-aware lookup:
//     `unreleasedGained` previously read the worktree, which let a
//     developer satisfy --require-changelog-entry by editing
//     CHANGELOG.md and forgetting to `git add`.
//  3. An untracked or staged-for-deletion path — not in the index, so
//     FileAtIndex must return the empty string with no error.
func (s *VCSTestSuite) TestFileAtIndex() {
	dir := s.T().TempDir()
	repo, err := git.PlainInit(dir, false)
	s.Require().NoError(err)
	wt, err := repo.Worktree()
	s.Require().NoError(err)
	sig := &object.Signature{Name: "tester", Email: "t@example.com", When: time.Now()}

	writeAndAdd := func(rel, body string) {
		s.Require().NoError(os.MkdirAll(filepath.Join(dir, filepath.Dir(rel)), 0o750))
		s.Require().NoError(os.WriteFile(filepath.Join(dir, rel), []byte(body), 0o600))
		_, err := wt.Add(rel)
		s.Require().NoError(err)
	}

	writeAndAdd("staged.go", "package x\n// committed body\n")
	writeAndAdd("worktree.go", "package x\n// committed body\n")
	_, err = wt.Commit("base", &git.CommitOptions{Author: sig})
	s.Require().NoError(err)

	// (1) Modify staged.go AND re-stage it. (2) Modify worktree.go but
	// leave it unstaged. (3) Drop a fresh untracked file in.
	s.Require().NoError(os.WriteFile(filepath.Join(dir, "staged.go"), []byte("package x\n// staged update\n"), 0o600))
	_, err = wt.Add("staged.go")
	s.Require().NoError(err)
	s.Require().NoError(os.WriteFile(filepath.Join(dir, "worktree.go"), []byte("package x\n// worktree-only edit\n"), 0o600))
	s.Require().NoError(os.WriteFile(filepath.Join(dir, "untracked.go"), []byte("package x\n"), 0o600))

	g, err := vcs.Open(dir, "main", slog.New(slog.DiscardHandler))
	s.Require().NoError(err)
	ctx := context.Background()

	staged, err := g.FileAtIndex(ctx, "staged.go")
	s.Require().NoError(err)
	s.Equal("package x\n// staged update\n", staged, "FileAtIndex must return the staged blob, not HEAD or the worktree")

	unstaged, err := g.FileAtIndex(ctx, "worktree.go")
	s.Require().NoError(err)
	s.Equal("package x\n// committed body\n", unstaged, "unstaged worktree edits must NOT leak through FileAtIndex")

	missing, err := g.FileAtIndex(ctx, "untracked.go")
	s.Require().NoError(err)
	s.Empty(missing, "untracked files are absent from the index and must return empty, not error")
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
