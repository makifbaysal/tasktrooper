package projectmodel

import "errors"

var (
	ErrInvalidInput = errors.New("invalid input")
	ErrConflict     = errors.New("conflict")
	ErrNoteLocked   = errors.New("note is locked")
)
