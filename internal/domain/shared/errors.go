package shared

import (
	"errors"
	"fmt"
)

// Sentinel / typed domain errors for HTTP mapping (SOLID: stable contracts).
var (
	ErrNotFound      = errors.New("not found")
	ErrConflict      = errors.New("conflict")
	ErrUnauthorized  = errors.New("unauthorized")
	ErrForbidden     = errors.New("forbidden")
	ErrValidation    = errors.New("validation")
	ErrInvalidState  = errors.New("invalid state transition")
	ErrDuplicate     = errors.New("duplicate")
)

// AppError carries a stable code + message for API clients.
type AppError struct {
	Code    string
	Message string
	Err     error
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *AppError) Unwrap() error { return e.Err }

func NewValidation(msg string) *AppError {
	return &AppError{Code: "validation_error", Message: msg, Err: ErrValidation}
}

func NewNotFound(entity string) *AppError {
	return &AppError{Code: "not_found", Message: entity + " not found", Err: ErrNotFound}
}

func NewConflict(msg string) *AppError {
	return &AppError{Code: "conflict", Message: msg, Err: ErrConflict}
}

func NewForbidden(msg string) *AppError {
	return &AppError{Code: "forbidden", Message: msg, Err: ErrForbidden}
}

func NewUnauthorized(msg string) *AppError {
	return &AppError{Code: "unauthorized", Message: msg, Err: ErrUnauthorized}
}

func NewInvalidState(msg string) *AppError {
	return &AppError{Code: "invalid_state", Message: msg, Err: ErrInvalidState}
}
