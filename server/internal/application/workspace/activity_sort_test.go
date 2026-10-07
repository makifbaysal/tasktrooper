package workspace

import (
	"math/rand"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type ActivitySortSuite struct {
	suite.Suite
	base time.Time
}

func TestActivitySortSuite(t *testing.T) {
	suite.Run(t, new(ActivitySortSuite))
}

func (s *ActivitySortSuite) SetupTest() {
	s.base = time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
}

func quadraticActivitySort(items []domain.ActivityItem) {
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j].CreatedAt.After(items[i].CreatedAt) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}

func (s *ActivitySortSuite) item(offsetSeconds int) domain.ActivityItem {
	return domain.ActivityItem{ID: uuid.New(), CreatedAt: s.base.Add(time.Duration(offsetSeconds) * time.Second)}
}

func (s *ActivitySortSuite) TestMatchesThePreviousOrderWhenTimesAreDistinct() {
	rng := rand.New(rand.NewSource(7))
	for round := 0; round < 50; round++ {
		offsets := rng.Perm(120)[:rng.Intn(120)]
		items := make([]domain.ActivityItem, len(offsets))
		for i, off := range offsets {
			items[i] = s.item(off)
		}
		want := append([]domain.ActivityItem(nil), items...)
		quadraticActivitySort(want)

		sortActivityItems(items)

		s.Equal(want, items)
	}
}

func (s *ActivitySortSuite) TestNewestFirstAndTiesKeepInputOrder() {
	event := s.item(10)
	run := s.item(10)
	run.CreatedAt = event.CreatedAt
	older := s.item(5)
	newest := s.item(20)
	items := []domain.ActivityItem{older, event, newest, run}

	sortActivityItems(items)

	s.Equal([]domain.ActivityItem{newest, event, run, older}, items)
}
