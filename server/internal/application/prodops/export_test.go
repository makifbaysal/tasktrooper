package prodops

import "time"

func SetClock(s *Service, now func() time.Time) { s.now = now }
