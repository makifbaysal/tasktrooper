package domain_test

import (
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestSpanCountsForSpeed(t *testing.T) {
	require.True(t, domain.SpanCountsForSpeed(domain.TaskColumnInProgress))
	require.True(t, domain.SpanCountsForSpeed(domain.TaskColumnCodeReview))
	require.True(t, domain.SpanCountsForSpeed(domain.TaskColumnReadyForQA))
	require.True(t, domain.SpanCountsForSpeed(domain.TaskColumnInQA))

	require.False(t, domain.SpanCountsForSpeed(domain.TaskColumnBlocked))
	require.False(t, domain.SpanCountsForSpeed(domain.TaskColumnHumanUAT))
	require.False(t, domain.SpanCountsForSpeed(domain.TaskColumnAnalizReview))
	require.False(t, domain.SpanCountsForSpeed(domain.TaskColumnBacklog))
	require.False(t, domain.SpanCountsForSpeed(domain.TaskColumnTodo))
	require.False(t, domain.SpanCountsForSpeed(domain.TaskColumnNeedRevision))
	require.False(t, domain.SpanCountsForSpeed(domain.TaskColumnDone))
	require.False(t, domain.SpanCountsForSpeed(domain.TaskColumnReleased))
}

func TestReviewEscapeIsHeavierThanAHumanRejection(t *testing.T) {
	require.Less(t, domain.ScoreDeltaReviewEscape, domain.ScoreDeltaHumanUATFailed)
}
