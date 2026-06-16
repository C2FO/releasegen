// Package logging provides a slog logger configured for releasegen.
//
// Two output modes are supported:
//
//   - GitHub Actions mode (the default when GITHUB_ACTIONS=true is set):
//     records at ERROR level are prefixed with "::error::" so they surface
//     in the Actions UI, and the Group / EndGroup helpers emit the
//     "::group::" / "::endgroup::" markers around per-module work.
//   - Local mode: a plain text handler suitable for terminal use.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// Options controls New.
type Options struct {
	// Writer is the destination for log records. If nil, os.Stderr is used.
	Writer io.Writer
	// Level is the minimum log level. Defaults to LevelInfo.
	Level slog.Level
	// CI, when true, formats records for GitHub Actions (::error::,
	// ::group::, ::endgroup:: markers). New does not auto-detect this;
	// callers should set it explicitly, typically via DetectCI().
	CI bool
}

// New constructs a *slog.Logger using the supplied options.
func New(opts Options) *slog.Logger {
	if opts.Writer == nil {
		opts.Writer = os.Stderr
	}
	handler := &actionsHandler{
		w:     opts.Writer,
		level: opts.Level,
		ci:    opts.CI,
	}
	return slog.New(handler)
}

// DetectCI returns true when running inside GitHub Actions.
func DetectCI() bool {
	return strings.EqualFold(os.Getenv("GITHUB_ACTIONS"), "true")
}

// Group prints a "::group::" marker (in CI mode) or a section header (locally).
// Write errors are intentionally ignored: log helpers must not fail the run.
func Group(w io.Writer, ci bool, title string) {
	if ci {
		_, _ = fmt.Fprintf(w, "::group::%s\n", title)
	} else {
		_, _ = fmt.Fprintf(w, "==> %s\n", title)
	}
}

// EndGroup prints the matching "::endgroup::" marker. Write errors are
// intentionally ignored: log helpers must not fail the run.
func EndGroup(w io.Writer, ci bool) {
	if ci {
		_, _ = fmt.Fprintln(w, "::endgroup::")
	}
}

// actionsHandler is a small slog.Handler that emits a single line per record
// and prefixes errors with "::error::" when running in GitHub Actions.
type actionsHandler struct {
	w     io.Writer
	level slog.Level
	ci    bool
	attrs []slog.Attr
	group string
}

func (h *actionsHandler) Enabled(_ context.Context, lvl slog.Level) bool {
	return lvl >= h.level
}

func (h *actionsHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	if h.ci && r.Level >= slog.LevelError {
		b.WriteString("::error::")
	} else {
		b.WriteString(r.Level.String())
		b.WriteString(": ")
	}
	b.WriteString(r.Message)
	for _, a := range h.attrs {
		appendAttr(&b, a, h.group)
	}
	r.Attrs(func(a slog.Attr) bool {
		appendAttr(&b, a, h.group)
		return true
	})
	b.WriteByte('\n')
	_, err := io.WriteString(h.w, b.String())
	return err
}

func (h *actionsHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	cloned := *h
	cloned.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &cloned
}

func (h *actionsHandler) WithGroup(name string) slog.Handler {
	cloned := *h
	if h.group == "" {
		cloned.group = name
	} else {
		cloned.group = h.group + "." + name
	}
	return &cloned
}

func appendAttr(b *strings.Builder, a slog.Attr, group string) {
	if a.Equal(slog.Attr{}) {
		return
	}
	b.WriteString(" ")
	if group != "" {
		b.WriteString(group)
		b.WriteByte('.')
	}
	b.WriteString(a.Key)
	b.WriteString("=")
	b.WriteString(a.Value.String())
}
