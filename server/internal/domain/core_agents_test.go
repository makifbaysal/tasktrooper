package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestIsCoreAgentSlug(t *testing.T) {
	for _, slug := range domain.CoreAgentSlugs {
		assert.True(t, domain.IsCoreAgentSlug(slug), slug)
	}
	assert.Len(t, domain.CoreAgentSlugs, 5)
	assert.False(t, domain.IsCoreAgentSlug("backend-developer"))
	assert.False(t, domain.IsCoreAgentSlug(""))
}
