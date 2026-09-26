package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestQAHoldsGetTaskPreviewAndImplementersDoNot(t *testing.T) {
	assert.Contains(t, qaToolPolicy().AllowTools, "get_task_preview")
	assert.NotContains(t, developerToolPolicy().AllowTools, "get_task_preview")
	assert.NotContains(t, releaseEngineerToolPolicy().AllowTools, "get_task_preview")
}
