package vcs

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/suite"
)

type ScrubURLTestSuite struct {
	suite.Suite
}

func TestScrubURLTestSuite(t *testing.T) {
	suite.Run(t, new(ScrubURLTestSuite))
}

func (s *ScrubURLTestSuite) TestNilError() {
	s.NoError(scrubURL(nil, "tok"))
}

func (s *ScrubURLTestSuite) TestEmptyTokenIsPassThrough() {
	in := errors.New("https://x:tok@example.com failed")
	s.Equal(in, scrubURL(in, ""))
}

func (s *ScrubURLTestSuite) TestTokenAbsent_ReturnsOriginal() {
	in := errors.New("plain failure with no secrets")
	s.Equal(in, scrubURL(in, "tok"))
}

func (s *ScrubURLTestSuite) TestTokenScrubbed() {
	in := errors.New("auth failed for https://x:supersecret@example.com/repo.git")
	out := scrubURL(in, "supersecret")
	s.Require().Error(out)
	s.NotContains(out.Error(), "supersecret")
	s.Contains(out.Error(), "***")
}
