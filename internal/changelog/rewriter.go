package changelog

import (
	"fmt"
	"net/url"
	"regexp"
	"time"
)

// RewriteOptions controls Rewrite. The new versioned heading links back to
// the GitHub Release URL derived from OwnerRepo, ModuleName, and
// NextVersion via ReleaseURL.
type RewriteOptions struct {
	ModuleName  string
	NextVersion string // bare semver, e.g. "1.2.3"
	OwnerRepo   string // "<owner>/<repo>"
	// MatchSection is the unreleased body to match in the source content
	// (i.e. the text that was originally extracted by ExtractUnreleased).
	MatchSection string
	// PromoteAs is the text to emit under the new versioned heading. It may
	// differ from MatchSection when, e.g., a manual-release footer is added.
	// When empty, MatchSection is used.
	PromoteAs string
	Now       time.Time
}

// ReleaseName returns the module-scoped release identifier
// ("vX.Y.Z" or "<module>/vX.Y.Z").
func ReleaseName(moduleName, version string) string {
	if moduleName == "" {
		return "v" + version
	}
	return fmt.Sprintf("%s/v%s", moduleName, version)
}

// ReleaseURL returns the GitHub Release URL for a given module/version.
func ReleaseURL(ownerRepo, moduleName, version string) string {
	return fmt.Sprintf(
		"https://github.com/%s/releases/tag/%s",
		ownerRepo,
		url.PathEscape(ReleaseName(moduleName, version)),
	)
}

// Rewrite returns a new changelog body that promotes the unreleased section
// to a versioned section. The original `## [Unreleased]` heading is preserved
// at the top with an empty body, and a new versioned heading is inserted
// directly below it.
func Rewrite(content string, opts RewriteOptions) string {
	promoted := opts.PromoteAs
	if promoted == "" {
		promoted = opts.MatchSection
	}
	pattern := regexp.MustCompile(`(?i)##\s*\[Unreleased\]\s*` + regexp.QuoteMeta(opts.MatchSection))
	releaseName := ReleaseName(opts.ModuleName, opts.NextVersion)
	releaseURL := ReleaseURL(opts.OwnerRepo, opts.ModuleName, opts.NextVersion)
	nowDate := opts.Now.UTC().Format("2006-01-02")
	replacement := fmt.Sprintf(
		"## [Unreleased]\n\n## [[%s](%s)] - %s\n%s",
		releaseName, releaseURL, nowDate, promoted,
	)
	return pattern.ReplaceAllString(content, replacement)
}
