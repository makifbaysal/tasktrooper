package cloud

import "errors"

var (
	ErrInvalidInput = errors.New("invalid input")
	ErrNotConnected = errors.New("environment is not connected to a cloud account")
)
