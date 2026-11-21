package vcs_test

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

	// Pre-commit-style scenario: stage a new edit to an existing file
	// AND drop an untracked + an unstaged-modified file alongside.
	// Only the *staged* change must appear in ChangedFiles: unstaged
	// worktree edits and untracked scratch files won't be in the next
	// commit, so reporting them would cause callers like
	// `validate --require-changelog-entry` to flag modules for local
	// dirt the developer never intended to commit.
	s.Require().NoError(os.WriteFile(filepath.Join(dir, "main.go"), []byte("package x\n// staged but not committed\n"), 0o600))
	_, err = wt.Add("main.go")
	s.Require().NoError(err)
	s.Require().NoError(os.WriteFile(filepath.Join(dir, "untracked.go"), []byte("package x\n"), 0o600))
	// Don't `git add` untracked.go — it stays untracked.
	s.Require().NoError(os.WriteFile(filepath.Join(dir, "submodule", "foo.go"), []byte("package submodule\n// unstaged edit\n"), 0o600))
	// Don't `git add` submodule/foo.go's new edit — the staged version
	// is still the committed one, the worktree just has extra dirt.

	changed, err = g.ChangedFiles(ctx, "base-tag")
	s.Require().NoError(err)
	s.Contains(changed, "main.go", "staged modification must be reported")
	s.Contains(changed, "submodule/foo.go", "previously committed change must still be reported (tree diff)")
	s.NotContains(changed, "untracked.go", "untracked files are not staged and must not be reported")
	// submodule/foo.go appears via the tree diff above, but its unstaged
	// worktree edit on top of HEAD must not be what brings it in — that's
	// already covered by the tree-diff assertion. Nothing else to check.
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

// TestOpen_LinkedWorktree pins that a linked worktree, whose .git is a file
// pointing at <main>/.git/worktrees/<name>, resolves HEAD, refs and the index
// through the shared common dir just like a normal checkout.
func (s *VCSTestSuite) TestOpen_LinkedWorktree() {
	// Keep git commands inside the temp repository even when this test runs
	// from a git hook or under a developer's global config.
	for _, k := range []string{"GIT_DIR", "GIT_INDEX_FILE", "GIT_WORK_TREE", "GIT_PREFIX", "GIT_COMMON_DIR"} {
		s.T().Setenv(k, "")
		s.Require().NoError(os.Unsetenv(k))
	}
	s.T().Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	s.T().Setenv("GIT_CONFIG_NOSYSTEM", "1")

	main := s.T().TempDir()
	runGit := func(dir string, args ...string) {
		cmd := exec.Command("git", args...) //nolint:gosec // G204: fixed binary, test-controlled args.
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		s.Require().NoErrorf(err, "git %v: %s", args, out)
	}
	runGit(main, "init", "-b", "main")
	runGit(main, "config", "user.email", "t@example.com")
	runGit(main, "config", "user.name", "tester")
	runGit(main, "config", "commit.gpgsign", "false")
	s.Require().NoError(os.WriteFile(filepath.Join(main, "CHANGELOG.md"), []byte("# Changelog\n"), 0o600))
	runGit(main, "add", "CHANGELOG.md")
	runGit(main, "commit", "-m", "base")

	linked := filepath.Join(s.T().TempDir(), "wt")
	runGit(main, "worktree", "add", "-b", "feat", linked)
	s.Require().NoError(os.WriteFile(filepath.Join(linked, "CHANGELOG.md"), []byte("# Changelog\nstaged\n"), 0o600))
	runGit(linked, "add", "CHANGELOG.md")

	g, err := vcs.Open(linked, "main", slog.New(slog.DiscardHandler))
	s.Require().NoError(err)
	ctx := context.Background()

	paths, err := g.AllChangelogPaths(ctx)
	s.Require().NoError(err, "HEAD must resolve through the worktree's common dir")
	s.Equal([]string{"CHANGELOG.md"}, paths)

	staged, err := g.FileAtIndex(ctx, "CHANGELOG.md")
	s.Require().NoError(err)
	s.Equal("# Changelog\nstaged\n", staged, "the worktree's own index must be read")
}

// TestOpen_RelativeWorktreesExtension pins that a repository declaring
// extensions.relativeworktrees, which git writes into the shared config for
// relative worktree links, still opens from the main checkout and from linked
// worktrees whose .git file uses an absolute or a relative gitdir, and that
// the worktree's own index is read.
func (s *VCSTestSuite) TestOpen_RelativeWorktreesExtension() {
	main, runGit := s.newRepoWithEnv()
	absWT := filepath.Join(s.T().TempDir(), "abs")
	relWT := filepath.Join(s.T().TempDir(), "rel")
	runGit(main, "worktree", "add", "-b", "abs", absWT)
	runGit(main, "worktree", "add", "-b", "rel", relWT)

	// git rewrites every worktree's link as relative once the extension is
	// enabled, so restore an absolute pointer for absWT to cover both forms.
	repair := exec.Command("git", "worktree", "repair", "--relative-paths")
	repair.Dir = main
	if out, err := repair.CombinedOutput(); err != nil {
		s.T().Skipf("installed git does not support worktree repair --relative-paths: %s", out)
	}
	absGitDir := filepath.Join(main, ".git", "worktrees", "abs")
	s.Require().NoError(os.WriteFile(filepath.Join(absWT, ".git"), []byte("gitdir: "+absGitDir+"\n"), 0o600))
	s.assertGitdirPointer(absWT, true)
	s.assertGitdirPointer(relWT, false)

	// Nest the symlink at a different depth than relWT so a lexical ".." walk
	// from the alias lands somewhere other than the physical one.
	aliasDir := filepath.Join(s.T().TempDir(), "nested", "deeper")
	s.Require().NoError(os.MkdirAll(aliasDir, 0o750))
	symlinked := filepath.Join(aliasDir, "alias")
	s.Require().NoError(os.Symlink(relWT, symlinked))

	for name, dir := range map[string]string{
		"main checkout":                        main,
		"absolute linked worktree":             absWT,
		"relative linked worktree":             relWT,
		"relative linked worktree via symlink": symlinked,
	} {
		s.Run(name, func() {
			s.Require().NoError(os.WriteFile(filepath.Join(dir, "CHANGELOG.md"), []byte("# Changelog\nstaged "+name+"\n"), 0o600))
			runGit(dir, "add", "CHANGELOG.md")

			g, err := vcs.Open(dir, "main", slog.New(slog.DiscardHandler))
			s.Require().NoError(err)
			ctx := context.Background()
			paths, err := g.AllChangelogPaths(ctx)
			s.Require().NoError(err)
			s.Equal([]string{"CHANGELOG.md"}, paths)
			staged, err := g.FileAtIndex(ctx, "CHANGELOG.md")
			s.Require().NoError(err)
			s.Equal("# Changelog\nstaged "+name+"\n", staged)
		})
	}
}

// assertGitdirPointer checks whether the worktree's .git file points at its
// git directory with an absolute or a relative path.
func (s *VCSTestSuite) assertGitdirPointer(worktree string, absolute bool) {
	b, err := os.ReadFile(filepath.Join(worktree, ".git")) //nolint:gosec // G304: test-controlled path.
	s.Require().NoError(err)
	target := strings.TrimSpace(strings.TrimPrefix(strings.SplitN(string(b), "\n", 2)[0], "gitdir:"))
	s.Equal(absolute, filepath.IsAbs(target), "unexpected gitdir pointer %q in %s", target, worktree)
}

// TestOpen_OtherUnknownExtensionStillFails pins that hiding
// relativeworktrees does not loosen go-git's check for any other extension,
// and that the original extension error is reported.
func (s *VCSTestSuite) TestOpen_OtherUnknownExtensionStillFails() {
	main, runGit := s.newRepoWithEnv()
	runGit(main, "config", "core.repositoryformatversion", "1")
	runGit(main, "config", "extensions.relativeWorktrees", "true")
	runGit(main, "config", "extensions.somethingElse", "true")

	_, err := vcs.Open(main, "main", slog.New(slog.DiscardHandler))
	s.Require().Error(err)
	s.Require().ErrorContains(err, "somethingelse")
	s.Require().ErrorContains(err, "relativeworktrees, somethingelse", "the original error must be reported")
}

// TestOpen_RelativeWorktreesExtensionKeyCase pins that the extension is
// hidden whatever case the config key is written in.
func (s *VCSTestSuite) TestOpen_RelativeWorktreesExtensionKeyCase() {
	main, runGit := s.newRepoWithEnv()
	runGit(main, "config", "core.repositoryformatversion", "1")
	runGit(main, "config", "extensions.relativeWorktrees", "true")

	g, err := vcs.Open(main, "main", slog.New(slog.DiscardHandler))
	s.Require().NoError(err)
	paths, err := g.AllChangelogPaths(context.Background())
	s.Require().NoError(err)
	s.Equal([]string{"CHANGELOG.md"}, paths)
}

// newRepoWithEnv creates a repository with one committed CHANGELOG.md, scrubs
// the git environment so commands stay inside it even under a hook or a
// developer's global config, and returns the repo path and a git runner.
func (s *VCSTestSuite) newRepoWithEnv() (string, func(dir string, args ...string)) {
	for _, k := range []string{"GIT_DIR", "GIT_INDEX_FILE", "GIT_WORK_TREE", "GIT_PREFIX", "GIT_COMMON_DIR"} {
		s.T().Setenv(k, "")
		s.Require().NoError(os.Unsetenv(k))
	}
	s.T().Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	s.T().Setenv("GIT_CONFIG_NOSYSTEM", "1")

	main := s.T().TempDir()
	runGit := func(dir string, args ...string) {
		cmd := exec.Command("git", args...) //nolint:gosec // G204: fixed binary, test-controlled args.
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		s.Require().NoErrorf(err, "git %v: %s", args, out)
	}
	runGit(main, "init", "-b", "main")
	runGit(main, "config", "user.email", "t@example.com")
	runGit(main, "config", "user.name", "tester")
	runGit(main, "config", "commit.gpgsign", "false")
	s.Require().NoError(os.WriteFile(filepath.Join(main, "CHANGELOG.md"), []byte("# Changelog\n"), 0o600))
	runGit(main, "add", "CHANGELOG.md")
	runGit(main, "commit", "-m", "base")
	return main, runGit
}
