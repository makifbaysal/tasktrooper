package antigravity

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInvokeSubagentMapsToSubagent(t *testing.T) {
	assert.Equal(t, "subagent", ledgerToolName("invoke_subagent"))
}
