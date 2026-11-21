package vcs

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/go-git/go-git/v5/storage/filesystem/dotgit"
)

// relativeWorktreesExtension is the repository extension git records when
// worktree links are stored as relative paths. It only changes how the
// .git file and worktrees/<name>/gitdir files spell their targets, which
// go-git already resolves, so it does not affect the reads releasegen does.
const relativeWorktreesExtension = "relativeworktrees"

// openRepository opens the repository at repoPath, following a linked
// worktree's commondir. go-git rejects repositories that declare an extension
// it does not know, including relativeworktrees; when that is the only reason
// the open failed, it retries with that extension hidden from go-git's check.
func openRepository(repoPath string) (*git.Repository, error) {
	r, err := git.PlainOpenWithOptions(repoPath, &git.PlainOpenOptions{EnableDotGitCommonDir: true})
	if err == nil || !isExtensionError(err) {
		return r, err
	}

	r, fallbackErr := openIgnoringRelativeWorktrees(repoPath)
	if fallbackErr != nil {
		return nil, fmt.Errorf("%w (retry without %s: %w)", err, relativeWorktreesExtension, fallbackErr)
	}
	return r, nil
}

func isExtensionError(err error) bool {
	return errors.Is(err, git.ErrUnknownExtension) ||
		errors.Is(err, git.ErrUnsupportedExtensionRepositoryFormatVersion)
}

// openIgnoringRelativeWorktrees mirrors go-git's PlainOpenWithOptions for a
// path that is the worktree root (no parent detection, no bare repositories),
// but opens the storage
// through a storer that hides the relativeworktrees extension.
func openIgnoringRelativeWorktrees(repoPath string) (*git.Repository, error) {
	root, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, fmt.Errorf("resolving %q: %w", repoPath, err)
	}
	wt := osfs.New(root)

	dot, err := dotGitFilesystem(root)
	if err != nil {
		return nil, err
	}

	repositoryFs := dot
	common, err := commonDirFilesystem(dot)
	if err != nil {
		return nil, err
	}
	if common != nil {
		repositoryFs = dotgit.NewRepositoryFilesystem(dot, common)
	}

	s := &extensionHidingStorer{Storage: filesystem.NewStorage(repositoryFs, cache.NewObjectLRUDefault())}
	return git.Open(s, wt)
}

// dotGitFilesystem returns the git directory for the checkout at root,
// following a .git file's gitdir pointer (absolute or relative).
func dotGitFilesystem(root string) (billy.Filesystem, error) {
	dotGit := filepath.Join(root, git.GitDirName)
	fi, err := os.Stat(dotGit)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, git.ErrRepositoryNotExists
		}
		return nil, err
	}
	if fi.IsDir() {
		return osfs.New(dotGit), nil
	}

	b, err := os.ReadFile(dotGit) //nolint:gosec // G304: path is the .git entry of the repository being opened.
	if err != nil {
		return nil, err
	}
	const prefix = "gitdir: "
	line, _, _ := strings.Cut(string(b), "\n")
	if !strings.HasPrefix(line, prefix) {
		return nil, fmt.Errorf(".git file has no %s prefix", prefix)
	}
	gitdir := strings.TrimSpace(line[len(prefix):])
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(root, gitdir)
	}
	return osfs.New(gitdir), nil
}

// commonDirFilesystem returns the shared git directory named by dot's
// commondir file, or nil when dot is already the common directory.
func commonDirFilesystem(dot billy.Filesystem) (billy.Filesystem, error) {
	f, err := dot.Open("commondir")
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(f)
	_ = f.Close()
	if err != nil {
		return nil, err
	}
	p := strings.TrimSpace(string(b))
	if p == "" {
		return nil, nil
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(dot.Root(), p)
	}
	if _, err := os.Stat(p); err != nil {
		if os.IsNotExist(err) {
			return nil, git.ErrRepositoryIncomplete
		}
		return nil, err
	}
	return osfs.New(p), nil
}

// extensionHidingStorer wraps go-git's filesystem storage and removes the
// relativeworktrees extension from the config go-git validates.
type extensionHidingStorer struct {
	*filesystem.Storage
}

// Config returns the repository config without the relativeworktrees
// extension. The filesystem storage decodes a fresh config on every call, so
// editing the result does not change what is stored.
func (s *extensionHidingStorer) Config() (*config.Config, error) {
	cfg, err := s.Storage.Config()
	if err != nil {
		return nil, err
	}
	if cfg.Raw != nil && cfg.Raw.HasSection("extensions") {
		cfg.Raw.Section("extensions").RemoveOption(relativeWorktreesExtension)
	}
	return cfg, nil
}
