package changelog

import (
	"os"
	"testing"

	"github.com/c2fo/releasegen/internal/config"
)

// TestClassify_AgainstActualRepoChangelog is a sanity check that runs the
// repo's own CHANGELOG.md through Classify and asserts it does not produce
// a major bump from prose. Guards against regression of the inline-code
// false-match that bumped v1.1.1 to v2.0.0.
func TestClassify_AgainstActualRepoChangelog(t *testing.T) {
	data, err := os.ReadFile("../../CHANGELOG.md")
	if err != nil {
		t.Skipf("no CHANGELOG.md at repo root: %v", err)
	}
	section := ExtractUnreleased(string(data))
	if section == "" {
		t.Skip("repo [Unreleased] section is empty")
	}
	custom := map[string]config.BumpType{"documentation": config.BumpPatch}
	bump, err := Classify(section, custom)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if bump == config.BumpMajor {
		t.Fatalf("real repo [Unreleased] classified as MAJOR; section:\n%s", section)
	}
	t.Logf("repo [Unreleased] classifies as %v (correct: not major)", bump)
}
