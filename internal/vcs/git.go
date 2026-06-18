package vcs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

const changelogFileName = "CHANGELOG.md"

// GitRepo is a Repo implementation backed by go-git operating on an on-disk
// repository.
type GitRepo struct {
	repo   *git.Repository
	branch string
	log    *slog.Logger
}

// Open opens the git repository at the given path and returns a GitRepo
// configured for the named release branch.
func Open(repoPath, branch string, log *slog.Logger) (*GitRepo, error) {
	if log == nil {
		log = slog.Default()
	}
	r, err := git.PlainOpen(repoPath)
	if err != nil {
		return nil, fmt.Errorf("%w: open repository at %q: %w", ErrVCS, repoPath, err)
	}
	return &GitRepo{repo: r, branch: branch, log: log}, nil
}

// AllChangelogPaths walks HEAD's tree and returns every CHANGELOG.md path.
func (g *GitRepo) AllChangelogPaths(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	headRef, err := g.repo.Head()
	if err != nil {
		return nil, fmt.Errorf("%w: resolve HEAD: %w", ErrVCS, err)
	}
	headCommit, err := g.repo.CommitObject(headRef.Hash())
	if err != nil {
		return nil, fmt.Errorf("%w: load HEAD commit: %w", ErrVCS, err)
	}
	tree, err := headCommit.Tree()
	if err != nil {
		return nil, fmt.Errorf("%w: load HEAD tree: %w", ErrVCS, err)
	}

	var paths []string
	err = tree.Files().ForEach(func(f *object.File) error {
		// Match only files literally named CHANGELOG.md so we do not pick up
		// neighbors like MYCHANGELOG.md or release-CHANGELOG.md. go-git
		// reports tree entries with forward slashes regardless of host OS,
		// so path.Base (not filepath.Base) is the right tool.
		if path.Base(f.Name) == changelogFileName {
			paths = append(paths, f.Name)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: walk HEAD tree: %w", ErrVCS, err)
	}
	return paths, nil
}

// ReachableTags returns all tags whose target commits are ancestors of the
// configured release branch tip.
func (g *GitRepo) ReachableTags(ctx context.Context) ([]TagInfo, error) {
	branchRef, err := g.repo.Reference(plumbing.NewBranchReferenceName(g.branch), true)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve branch %q: %w", ErrVCS, g.branch, err)
	}
	branchCommit, err := g.repo.CommitObject(branchRef.Hash())
	if err != nil {
		return nil, fmt.Errorf("%w: load branch commit: %w", ErrVCS, err)
	}

	tagsIter, err := g.repo.Tags()
	if err != nil {
		return nil, fmt.Errorf("%w: iterate tags: %w", ErrVCS, err)
	}

	var tags []TagInfo
	err = tagsIter.ForEach(func(ref *plumbing.Reference) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		tagName := ref.Name().Short()
		moduleName := extractModuleName(tagName)

		hash := ref.Hash()
		var date int64

		if obj, err := g.repo.TagObject(hash); err == nil {
			hash = obj.Target
			date = obj.Tagger.When.Unix()
		} else {
			c, err := g.repo.CommitObject(ref.Hash())
			if err != nil {
				g.log.Debug("skipping tag, cannot resolve commit", "tag", tagName, "err", err)
				return nil
			}
			date = c.Committer.When.Unix()
		}

		commit, err := g.repo.CommitObject(hash)
		if err != nil {
			g.log.Debug("skipping tag, cannot load commit", "tag", tagName, "err", err)
			return nil
		}
		ancestor, err := commit.IsAncestor(branchCommit)
		if err != nil {
			g.log.Debug("skipping tag, ancestor check failed", "tag", tagName, "err", err)
			return nil
		}
		if !ancestor {
			g.log.Debug("skipping tag, not reachable from branch", "tag", tagName, "branch", g.branch)
			return nil
		}

		tags = append(tags, TagInfo{
			Name:       tagName,
			ModuleName: moduleName,
			Date:       date,
			Hash:       hash,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return tags, nil
}

// IsChangelogModifiedSinceTag returns true if changelogPath was changed
// between the commit referenced by tagName and HEAD. When tagName is empty
// the function returns true (first release).
func (g *GitRepo) IsChangelogModifiedSinceTag(ctx context.Context, changelogPath, tagName string) (bool, error) {
	if tagName == "" {
		return true, nil
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	ref, err := g.repo.Reference(plumbing.NewTagReferenceName(tagName), true)
	if err != nil {
		return false, fmt.Errorf("%w: resolve tag %q: %w", ErrVCS, tagName, err)
	}

	tagCommit, err := g.repo.CommitObject(ref.Hash())
	if err != nil {
		tagObj, err := g.repo.TagObject(ref.Hash())
		if err != nil {
			return false, fmt.Errorf("%w: resolve commit for tag %q: %w", ErrVCS, tagName, err)
		}
		tagCommit, err = g.repo.CommitObject(tagObj.Target)
		if err != nil {
			return false, fmt.Errorf("%w: resolve commit for annotated tag %q: %w", ErrVCS, tagName, err)
		}
	}

	headRef, err := g.repo.Head()
	if err != nil {
		return false, fmt.Errorf("%w: resolve HEAD: %w", ErrVCS, err)
	}
	headCommit, err := g.repo.CommitObject(headRef.Hash())
	if err != nil {
		return false, fmt.Errorf("%w: load HEAD commit: %w", ErrVCS, err)
	}

	// Compare only the changelog blob between the two commit trees instead of
	// computing a full repo-wide patch. This is O(tree lookup) rather than
	// O(repo size), which matters on large repositories. A differing hash
	// (including the file being added or removed, represented by the zero
	// hash) means the changelog changed between the tag and HEAD.
	tagHash, err := changelogBlobHash(tagCommit, changelogPath)
	if err != nil {
		return false, fmt.Errorf("%w: read %s at tag %q: %w", ErrVCS, changelogPath, tagName, err)
	}
	headHash, err := changelogBlobHash(headCommit, changelogPath)
	if err != nil {
		return false, fmt.Errorf("%w: read %s at HEAD: %w", ErrVCS, changelogPath, err)
	}
	return tagHash != headHash, nil
}

// changelogBlobHash returns the blob hash of changelogPath within the given
// commit's tree. When the file does not exist in that tree it returns the
// zero hash (so an added or removed file registers as a change).
func changelogBlobHash(c *object.Commit, changelogPath string) (plumbing.Hash, error) {
	tree, err := c.Tree()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	entry, err := tree.FindEntry(changelogPath)
	if err != nil {
		if errors.Is(err, object.ErrEntryNotFound) || errors.Is(err, object.ErrDirectoryNotFound) {
			return plumbing.ZeroHash, nil
		}
		return plumbing.ZeroHash, err
	}
	return entry.Hash, nil
}

// ChangedFiles returns the set of file paths that differ between the tree at
// baseRef and the tree at HEAD. The comparison is a two-dot diff (HEAD vs
// baseRef directly), matching the behavior of `git diff <base>` rather than
// the merge-base "three-dot" form. For PR-time validation this is precise
// enough and avoids a merge-base computation that go-git makes awkward.
//
// baseRef may be any revision spec go-git can resolve (branch name, tag,
// remote-tracking ref like "origin/main", or a raw hash). When baseRef does
// not resolve, an error wrapping ErrVCS is returned so the caller can map it
// to a useful CI message ("did you fetch with depth 0?").
func (g *GitRepo) ChangedFiles(ctx context.Context, baseRef string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	baseCommit, err := g.commitForRev(baseRef)
	if err != nil {
		return nil, err
	}
	headRef, err := g.repo.Head()
	if err != nil {
		return nil, fmt.Errorf("%w: resolve HEAD: %w", ErrVCS, err)
	}
	headCommit, err := g.repo.CommitObject(headRef.Hash())
	if err != nil {
		return nil, fmt.Errorf("%w: load HEAD commit: %w", ErrVCS, err)
	}
	baseTree, err := baseCommit.Tree()
	if err != nil {
		return nil, fmt.Errorf("%w: load tree at %q: %w", ErrVCS, baseRef, err)
	}
	headTree, err := headCommit.Tree()
	if err != nil {
		return nil, fmt.Errorf("%w: load HEAD tree: %w", ErrVCS, err)
	}
	changes, err := baseTree.Diff(headTree)
	if err != nil {
		return nil, fmt.Errorf("%w: diff %q..HEAD: %w", ErrVCS, baseRef, err)
	}
	// Use a map to deduplicate when a path appears as both From and To
	// (e.g. a rename).
	seen := make(map[string]struct{}, len(changes))
	for _, c := range changes {
		if c.From.Name != "" {
			seen[c.From.Name] = struct{}{}
		}
		if c.To.Name != "" {
			seen[c.To.Name] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

// FileAtRef returns the contents of the file at filePath as it appears in
// the tree of ref's commit. When the file does not exist in that tree the
// empty string is returned with a nil error — callers can treat this as
// "the file was added in HEAD." Other errors (unresolvable ref, unreadable
// blob) are wrapped with ErrVCS.
func (g *GitRepo) FileAtRef(ctx context.Context, ref, filePath string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	commit, err := g.commitForRev(ref)
	if err != nil {
		return "", err
	}
	tree, err := commit.Tree()
	if err != nil {
		return "", fmt.Errorf("%w: load tree at %q: %w", ErrVCS, ref, err)
	}
	f, err := tree.File(filePath)
	if err != nil {
		if errors.Is(err, object.ErrFileNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("%w: find %s at %q: %w", ErrVCS, filePath, ref, err)
	}
	body, err := f.Contents()
	if err != nil {
		return "", fmt.Errorf("%w: read %s at %q: %w", ErrVCS, filePath, ref, err)
	}
	return body, nil
}

// commitForRev resolves an arbitrary revision spec (branch, tag, remote
// tracking ref, hash) to its underlying commit. The error path covers the
// common CI failure mode of "you forgot to fetch with depth 0" by including
// the original ref in the wrapped message.
func (g *GitRepo) commitForRev(rev string) (*object.Commit, error) {
	hash, err := g.repo.ResolveRevision(plumbing.Revision(rev))
	if err != nil {
		return nil, fmt.Errorf("%w: resolve %q: %w", ErrVCS, rev, err)
	}
	commit, err := g.repo.CommitObject(*hash)
	if err != nil {
		return nil, fmt.Errorf("%w: load commit for %q: %w", ErrVCS, rev, err)
	}
	return commit, nil
}

// CommitTagAndPush stages, commits, pushes, tags, and pushes the tag for a
// single module release. Errors are wrapped with the failing step name so
// the caller can decide on recovery.
func (g *GitRepo) CommitTagAndPush(ctx context.Context, opts CommitTagPushOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	wt, err := g.repo.Worktree()
	if err != nil {
		return fmt.Errorf("%w: worktree: %w", ErrVCS, err)
	}
	if _, err := wt.Add(opts.ChangelogPath); err != nil {
		return fmt.Errorf("%w: git add %s: %w", ErrVCS, opts.ChangelogPath, err)
	}

	commitMsg := fmt.Sprintf(
		"chore: release version %s/v%s (%s) [skip ci]",
		opts.ModuleName, opts.Version, opts.Actor,
	)
	if opts.ModuleName == "" {
		commitMsg = fmt.Sprintf(
			"chore: release version v%s (%s) [skip ci]",
			opts.Version, opts.Actor,
		)
	}

	sig := &object.Signature{
		Name:  "github-actions[bot]",
		Email: "41898282+github-actions[bot]@users.noreply.github.com",
		When:  time.Now(),
	}
	if _, err := wt.Commit(commitMsg, &git.CommitOptions{Author: sig}); err != nil {
		return fmt.Errorf("%w: git commit: %w", ErrVCS, err)
	}

	headRef, err := g.repo.Head()
	if err != nil {
		return fmt.Errorf("%w: resolve HEAD after commit: %w", ErrVCS, err)
	}

	auth := &http.BasicAuth{
		Username: "github-actions[bot]",
		Password: opts.Token,
	}
	if err := g.repo.PushContext(ctx, &git.PushOptions{Auth: auth}); err != nil {
		return fmt.Errorf("%w: git push: %w", ErrVCS, scrubURL(err, opts.Token))
	}

	tagName := opts.ModuleName + "/v" + opts.Version
	if opts.ModuleName == "" {
		tagName = "v" + opts.Version
	}
	if _, err := g.repo.CreateTag(tagName, headRef.Hash(), &git.CreateTagOptions{
		Tagger:  sig,
		Message: tagName,
	}); err != nil {
		return fmt.Errorf("%w: create tag %s: %w", ErrVCS, tagName, err)
	}
	if err := g.repo.PushContext(ctx, &git.PushOptions{
		Auth:     auth,
		RefSpecs: []config.RefSpec{config.RefSpec("refs/tags/" + tagName + ":refs/tags/" + tagName)},
	}); err != nil {
		return fmt.Errorf("%w: push tag %s: %w", ErrVCS, tagName, scrubURL(err, opts.Token))
	}
	return nil
}

// extractModuleName returns "" for "v1.2.3", "mod" for "mod/v1.2.3",
// "a/b" for "a/b/v1.2.3", and "" for anything that does not contain "/v".
func extractModuleName(tagName string) string {
	if idx := strings.LastIndex(tagName, "/v"); idx != -1 {
		return tagName[:idx]
	}
	return ""
}

// scrubURL removes the bearer token from go-git error messages that may
// embed the remote URL (which contains the basic-auth credentials).
func scrubURL(err error, token string) error {
	if err == nil || token == "" {
		return err
	}
	msg := err.Error()
	if !strings.Contains(msg, token) {
		return err
	}
	return fmt.Errorf("%s", strings.ReplaceAll(msg, token, "***"))
}

// LatestTagForModule returns the most recent tag in tags belonging to the
// named module (empty name = root). It returns "" when no tag is found.
func LatestTagForModule(tags []TagInfo, moduleName string) string {
	var matching []TagInfo
	for _, t := range tags {
		if t.ModuleName == moduleName {
			matching = append(matching, t)
		}
	}
	if len(matching) == 0 {
		return ""
	}
	sort.Slice(matching, func(i, j int) bool { return matching[i].Date > matching[j].Date })
	return matching[0].Name
}
