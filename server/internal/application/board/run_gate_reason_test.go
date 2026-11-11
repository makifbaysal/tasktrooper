package board

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
)

func TestRunGateRejectionReasons_AllSatisfyIsRunGateRejection(t *testing.T) {
	reasons := []string{
		ungroundedAnalysisReason,
		ungroundedQAReason,
		noUIEvidenceReason,
	}
	for _, reason := range reasons {
		assert.True(t, domain.IsRunGateRejection(reason), reason)
	}
}
