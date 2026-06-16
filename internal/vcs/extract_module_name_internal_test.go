package vcs

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type ExtractModuleNameTestSuite struct {
	suite.Suite
}

func TestExtractModuleNameTestSuite(t *testing.T) {
	suite.Run(t, new(ExtractModuleNameTestSuite))
}

func (s *ExtractModuleNameTestSuite) TestExtractModuleName() {
	cases := []struct {
		name string
		tag  string
		want string
	}{
		{name: "root tag", tag: "v1.2.3", want: ""},
		{name: "root tag without v prefix is not a release tag", tag: "1.2.3", want: ""},
		{name: "single-segment module", tag: "mod/v1.2.3", want: "mod"},
		{name: "multi-segment module", tag: "a/b/v1.2.3", want: "a/b"},
		{name: "deeply nested module", tag: "contrib/backend/dropbox/v0.1.0", want: "contrib/backend/dropbox"},
		{name: "module name containing v", tag: "vfsevents/v2.0.0", want: "vfsevents"},
		{name: "no slash v separator", tag: "release-2026-01-01", want: ""},
		{name: "empty string", tag: "", want: ""},
		{name: "module name with trailing slash before v", tag: "mod/v", want: "mod"},
		{name: "pre-release qualifier", tag: "mod/v1.2.3-rc.1", want: "mod"},
		{name: "build metadata", tag: "mod/v1.2.3+build.5", want: "mod"},
		{name: "module path containing a v segment uses last /v", tag: "vendor/v2/v1.0.0", want: "vendor/v2"},
	}
	for _, tc := range cases {
		s.Run(tc.name, func() {
			s.Equal(tc.want, extractModuleName(tc.tag))
		})
	}
}
