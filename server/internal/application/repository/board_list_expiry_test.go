package repository

import (
	"testing"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestBoardListExpiry(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	older, newer := now.Add(-48*time.Hour), now.Add(-time.Hour)
	cases := []struct {
		name   string
		tasks  []domain.BoardTask
		want   time.Time
		wantOK bool
	}{
		{name: "no released task never expires", tasks: []domain.BoardTask{{Column: domain.TaskColumnTodo, UpdatedAt: older}}},
		{
			name: "the earliest released task decides",
			tasks: []domain.BoardTask{
				{Column: domain.TaskColumnReleased, ColumnEnteredAt: &newer},
				{Column: domain.TaskColumnReleased, ColumnEnteredAt: &older},
			},
			want: older.Add(domain.ReleasedBoardWindow), wantOK: true,
		},
		{
			name:  "a released task with no span falls back to its last write",
			tasks: []domain.BoardTask{{Column: domain.TaskColumnReleased, UpdatedAt: newer}},
			want:  newer.Add(domain.ReleasedBoardWindow), wantOK: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := BoardListExpiry(tc.tasks)
			if ok != tc.wantOK || !got.Equal(tc.want) {
				t.Fatalf("BoardListExpiry = %v, %v; want %v, %v", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}
