package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidReleaseVersionAcceptsAnOrdinarySemver(t *testing.T) {
	assert.NoError(t, ValidReleaseVersion("1.2.3"))
}

func TestValidReleaseVersionEnforcesGitRefRules(t *testing.T) {
	for _, v := range []string{
		"1..2",
		"1.2.",
		"1.2.lock",
		"refs@{up}",
		"1.2//3",
	} {
		assert.Error(t, ValidReleaseVersion(v), v)
	}
}
