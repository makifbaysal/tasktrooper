package cloud_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/cloud"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestGroupErrorsEmptyInput(t *testing.T) {
	require.Nil(t, cloud.GroupErrors(nil, time.Now()))
}

func TestGroupErrorsGroupsMessagesDifferingOnlyByIDs(t *testing.T) {
	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	entries := []domain.RuntimeLogEntry{
		{Timestamp: since.Add(time.Minute), Severity: domain.LogError, Message: `failed to load user 123e4567-e89b-12d3-a456-426614174000: not found`},
		{Timestamp: since.Add(2 * time.Minute), Severity: domain.LogError, Message: `failed to load user 98765432-e89b-12d3-a456-426614174999: not found`},
	}

	groups := cloud.GroupErrors(entries, since)
	require.Len(t, groups, 1)
	require.Equal(t, 2, groups[0].Count)
}

func TestGroupErrorsSeparatesDifferentMessages(t *testing.T) {
	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	entries := []domain.RuntimeLogEntry{
		{Timestamp: since.Add(time.Minute), Severity: domain.LogError, Message: "database connection refused"},
		{Timestamp: since.Add(2 * time.Minute), Severity: domain.LogError, Message: "out of memory"},
	}

	groups := cloud.GroupErrors(entries, since)
	require.Len(t, groups, 2)
}

func TestGroupErrorsSortsByCountThenRecency(t *testing.T) {
	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	entries := []domain.RuntimeLogEntry{
		{Timestamp: since.Add(time.Minute), Message: "rare error"},
		{Timestamp: since.Add(2 * time.Minute), Message: "common error"},
		{Timestamp: since.Add(3 * time.Minute), Message: "common error"},
		{Timestamp: since.Add(4 * time.Minute), Message: "common error"},
	}

	groups := cloud.GroupErrors(entries, since)
	require.Len(t, groups, 2)
	require.Equal(t, 3, groups[0].Count)
	require.Equal(t, 1, groups[1].Count)
}

func TestGroupErrorsMarksSecondHalfOfWindowAsNew(t *testing.T) {
	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	windowEnd := since.Add(10 * time.Minute)
	entries := []domain.RuntimeLogEntry{
		{Timestamp: since.Add(time.Minute), Message: "old-ish error"},
		{Timestamp: windowEnd, Message: "brand new error"},
	}

	groups := cloud.GroupErrors(entries, since)
	byMessage := map[string]domain.RuntimeErrorGroup{}
	for _, g := range groups {
		byMessage[g.Message] = g
	}
	require.False(t, byMessage["old-ish error"].New)
	require.True(t, byMessage["brand new error"].New)
}

func TestGroupErrorsSampleCapsAt20Lines(t *testing.T) {
	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var message string
	for i := 0; i < 30; i++ {
		if i > 0 {
			message += "\n"
		}
		message += "line"
	}
	groups := cloud.GroupErrors([]domain.RuntimeLogEntry{{Timestamp: since.Add(time.Minute), Message: message}}, since)
	require.Len(t, groups, 1)
	require.LessOrEqual(t, len(splitLines(groups[0].Sample)), 20)
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i, r := range s {
		if r == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	lines = append(lines, s[start:])
	return lines
}
