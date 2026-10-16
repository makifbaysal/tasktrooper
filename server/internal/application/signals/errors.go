package signals

import "errors"

var (
	ErrInvalidInput = errors.New("invalid input")
	// ErrConfirmed is dismissing the confirmed binding: that is a delete.
	ErrConfirmed = errors.New("the binding is confirmed; delete it instead")

	errPanicked = errors.New("reading the signal failed")
)
