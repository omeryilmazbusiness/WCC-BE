package shared

import (
	"errors"
	"fmt"
	"time"
)

// Sentinel / typed domain errors for HTTP mapping (SOLID: stable contracts).
var (
	ErrNotFound     = errors.New("not found")
	ErrConflict     = errors.New("conflict")
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
	ErrValidation   = errors.New("validation")
	ErrInvalidState = errors.New("invalid state transition")
	ErrDuplicate    = errors.New("duplicate")
	ErrLocked       = errors.New("locked")
	ErrRateLimited  = errors.New("rate limited")
	ErrUnavailable  = errors.New("unavailable")
)

// AppError carries a stable code + message for API clients.
type AppError struct {
	Code    string
	Message string
	Err     error
	// RetryAfter tells clients when a locked/throttled request may be retried.
	RetryAfter time.Duration
	// Details is machine-readable context rendered as error.details.
	Details map[string]any
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

func NewLocked(msg string, retryAfter time.Duration) *AppError {
	return &AppError{Code: "account_locked", Message: msg, Err: ErrLocked, RetryAfter: retryAfter}
}

// NewUnauthorizedCode is a 401 with a specific code telling the client how
// to recover (e.g. refresh vs. sign in again).
func NewUnauthorizedCode(code, msg string) *AppError {
	return &AppError{Code: code, Message: msg, Err: ErrUnauthorized}
}

func NewUnavailable(msg string, retryAfter time.Duration) *AppError {
	return &AppError{Code: "service_unavailable", Message: msg, Err: ErrUnavailable, RetryAfter: retryAfter}
}

func NewRateLimited(msg string, retryAfter time.Duration) *AppError {
	return &AppError{Code: "rate_limited", Message: msg, Err: ErrRateLimited, RetryAfter: retryAfter}
}
