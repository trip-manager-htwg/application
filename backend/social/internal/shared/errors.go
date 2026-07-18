package shared

import "errors"

var (
	ErrNotFound     = errors.New("not found")
	ErrInternal     = errors.New("internal error")
	ErrConflict     = errors.New("conflict")
	ErrInvalidInput = errors.New("invalid input")
	ErrForbidden    = errors.New("forbidden")
)
