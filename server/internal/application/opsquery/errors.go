package opsquery

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidInput = errors.New("invalid input")

	errPanicked = errors.New("reading the environment failed")
)

// TooManyEnvironmentsError is a logs query over more environments than one
// call reads, with no component filter to cut it down: the caller picks
// components instead of getting an arbitrary subset.
type TooManyEnvironmentsError struct {
	Matched int
	Max     int
}

func (e *TooManyEnvironmentsError) Error() string {
	return fmt.Sprintf("%d environments match, at most %d can be read at once: pick components", e.Matched, e.Max)
}
