package changelog

import (
	"fmt"
	"strings"
	"time"

	"github.com/c2fo/releasegen/internal/config"
)

// UpdateRequest describes a single changelog update.
type UpdateRequest struct {
	Content       string
	ModuleName    string
	OwnerRepo     string
	CustomTypes   map[string]config.BumpType
	ManualVersion string // empty means "calculate from content"
	ManualReason  string
	Actor         string
	Now           time.Time
}

// UpdateResult is the outcome of a successful Update.
type UpdateResult struct {
	NextVersion       string // bare semver, e.g. "1.2.3"
	UnreleasedSection string // body promoted into the new versioned section (incl. manual footer)
	NewContent        string // full rewritten changelog body
	Bump              config.BumpType
	Manual            bool
}

// Update performs the full pure transformation:
// extract -> classify -> bump -> (optional override) -> rewrite.
// It returns ErrNoChangesDetected when the unreleased section is empty.
func Update(req UpdateRequest) (UpdateResult, error) {
	unreleased := ExtractUnreleased(req.Content)
	if unreleased == "" {
		return UpdateResult{}, ErrNoChangesDetected
	}

	currentVersion := ExtractCurrentVersion(req.Content)

	bump, err := Classify(unreleased, req.CustomTypes)
	if err != nil {
		return UpdateResult{}, err
	}

	nextVersion, err := NextVersion(currentVersion, bump)
	if err != nil {
		return UpdateResult{}, fmt.Errorf("unable to determine next semantic version: %w", err)
	}

	manual := req.ManualVersion != ""
	promoted := unreleased
	if manual {
		nextVersion = req.ManualVersion
		footer := "Manual release by " + req.Actor
		if strings.TrimSpace(req.ManualReason) != "" {
			footer += ": " + req.ManualReason
		}
		promoted = unreleased + "\n\n" + footer
	}

	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}

	newContent := Rewrite(req.Content, RewriteOptions{
		ModuleName:   req.ModuleName,
		NextVersion:  nextVersion,
		OwnerRepo:    req.OwnerRepo,
		MatchSection: unreleased,
		PromoteAs:    promoted,
		Now:          now,
	})

	return UpdateResult{
		NextVersion:       nextVersion,
		UnreleasedSection: promoted,
		NewContent:        newContent,
		Bump:              bump,
		Manual:            manual,
	}, nil
}
