package opsai

import (
	"errors"
	"sort"
	"strings"
)

var (
	ErrInvalidInput = errors.New("invalid input")
	// ErrUnavailable: this server runs no chat sessions or no board, so the
	// release engineer cannot be asked.
	ErrUnavailable = errors.New("the release engineer cannot be run on this server")
	// ErrNoReleaseEngineer: no enabled agent holds the release role for the
	// component's area.
	ErrNoReleaseEngineer = errors.New("no release engineer is on the team")

	errStoppedOnQuestion = errors.New("the release engineer stopped on a question; answer it in the chat")
	errTimedOut          = errors.New("the job ran past its time limit and was stopped")
	errQueuedTooLong     = errors.New("the job waited too long for a free slot; start it again")
)

// FieldErrors maps a request key ("environment", "goals") to a
// datasource.Field* code, the data source routes' shape.
type FieldErrors map[string]string

func (f FieldErrors) Error() string {
	keys := make([]string, 0, len(f))
	for k := range f {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+": "+f[k])
	}
	return "invalid request: " + strings.Join(parts, ", ")
}

func (f FieldErrors) Is(target error) bool { return target == ErrInvalidInput }
