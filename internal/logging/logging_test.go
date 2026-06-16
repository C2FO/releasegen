package logging_test

import (
	"bytes"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/c2fo/releasegen/internal/logging"
)

type LoggingTestSuite struct {
	suite.Suite
}

func TestLoggingTestSuite(t *testing.T) {
	suite.Run(t, new(LoggingTestSuite))
}

func (s *LoggingTestSuite) TestErrorEmitsActionsMarkerInCI() {
	var buf bytes.Buffer
	log := logging.New(logging.Options{Writer: &buf, Level: slog.LevelDebug, CI: true})
	log.Error("boom", "module", "foo")
	out := buf.String()
	s.Contains(out, "::error::")
	s.Contains(out, "boom")
	s.Contains(out, "module=foo")
}

func (s *LoggingTestSuite) TestInfoLocal() {
	var buf bytes.Buffer
	log := logging.New(logging.Options{Writer: &buf, Level: slog.LevelDebug, CI: false})
	log.Info("hi", "k", "v")
	out := buf.String()
	s.NotContains(out, "::error::")
	s.Contains(out, "INFO: hi")
	s.Contains(out, "k=v")
}

func (s *LoggingTestSuite) TestLevelFiltering() {
	var buf bytes.Buffer
	log := logging.New(logging.Options{Writer: &buf, Level: slog.LevelWarn})
	log.Debug("hidden")
	log.Info("hidden")
	log.Warn("shown")
	s.NotContains(buf.String(), "hidden")
	s.Contains(buf.String(), "shown")
}

func (s *LoggingTestSuite) TestWithAttrs_AttachesPersistentFields() {
	var buf bytes.Buffer
	base := logging.New(logging.Options{Writer: &buf, Level: slog.LevelDebug})
	scoped := base.With("module", "alpha", "request_id", "abc")
	scoped.Info("processing")
	out := buf.String()
	s.Contains(out, "module=alpha")
	s.Contains(out, "request_id=abc")
	s.Contains(out, "processing")
}

func (s *LoggingTestSuite) TestWithGroup_NamespacesAttrs() {
	var buf bytes.Buffer
	base := logging.New(logging.Options{Writer: &buf, Level: slog.LevelDebug})
	scoped := base.WithGroup("step")
	scoped.Info("done", "name", "release")
	out := buf.String()
	s.Contains(out, "done")
	s.Contains(out, "name=release")
}

func (s *LoggingTestSuite) TestWithAttrs_PreservesCIErrorMarker() {
	var buf bytes.Buffer
	base := logging.New(logging.Options{Writer: &buf, Level: slog.LevelDebug, CI: true})
	scoped := base.With("module", "alpha")
	scoped.Error("kapow")
	out := buf.String()
	s.Contains(out, "::error::")
	s.Contains(out, "kapow")
	s.Contains(out, "module=alpha")
}

func (s *LoggingTestSuite) TestDetectCI() {
	cases := []struct {
		name  string
		value string
		set   bool
		want  bool
	}{
		{name: "unset", set: false, want: false},
		{name: "empty", value: "", set: true, want: false},
		{name: "true", value: "true", set: true, want: true},
		{name: "TRUE mixed case", value: "TRUE", set: true, want: true},
		{name: "True", value: "True", set: true, want: true},
		{name: "false", value: "false", set: true, want: false},
		{name: "1 is not true", value: "1", set: true, want: false},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			if tc.set {
				s.T().Setenv("GITHUB_ACTIONS", tc.value)
			} else {
				s.T().Setenv("GITHUB_ACTIONS", "")
				s.Require().NoError(os.Unsetenv("GITHUB_ACTIONS"))
			}
			s.Equal(tc.want, logging.DetectCI())
		})
	}
}

func (s *LoggingTestSuite) TestGroupMarkers() {
	var buf bytes.Buffer
	logging.Group(&buf, true, "title")
	logging.EndGroup(&buf, true)
	out := buf.String()
	s.Contains(out, "::group::title")
	s.Contains(out, "::endgroup::")

	buf.Reset()
	logging.Group(&buf, false, "title")
	logging.EndGroup(&buf, false)
	s.Contains(buf.String(), "==> title")
	s.NotContains(buf.String(), "::group::")
}
