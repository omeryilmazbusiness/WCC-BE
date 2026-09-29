package response

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/access"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type envelope struct {
	Data  any            `json:"data,omitempty"`
	Error *errorBody     `json:"error,omitempty"`
	Meta  map[string]any `json:"meta,omitempty"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	// RetryAfter is seconds until retry for 423/429/503 responses.
	RetryAfter int            `json:"retry_after,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
}

func JSON(w http.ResponseWriter, status int, data any) {
	write(w, status, envelope{Data: data})
}

func JSONMeta(w http.ResponseWriter, status int, data any, meta map[string]any) {
	write(w, status, envelope{Data: data, Meta: meta})
}

func Error(w http.ResponseWriter, err error) {
	status, code, msg := mapError(err)
	if status == http.StatusInternalServerError {
		slog.Error("unhandled request error", "err", err)
	}
	body := &errorBody{Code: code, Message: msg}
	var app *shared.AppError
	if errors.As(err, &app) && app.RetryAfter > 0 {
		secs := int((app.RetryAfter + time.Second - 1) / time.Second)
		body.RetryAfter = secs
		w.Header().Set("Retry-After", strconv.Itoa(secs))
	}
	if app != nil && len(app.Details) > 0 {
		body.Details = app.Details
	}
	write(w, status, envelope{Error: body})
}

func write(w http.ResponseWriter, status int, body envelope) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func mapError(err error) (status int, code, msg string) {
	var app *shared.AppError
	if errors.As(err, &app) {
		msg = app.Message
		code = app.Code
		switch {
		case errors.Is(app.Err, shared.ErrNotFound):
			return http.StatusNotFound, code, msg
		case errors.Is(app.Err, shared.ErrValidation):
			return http.StatusBadRequest, code, msg
		case errors.Is(app.Err, shared.ErrUnauthorized):
			return http.StatusUnauthorized, code, msg
		case errors.Is(app.Err, shared.ErrForbidden):
			return http.StatusForbidden, code, msg
		case errors.Is(app.Err, shared.ErrConflict), errors.Is(app.Err, shared.ErrDuplicate):
			return http.StatusConflict, code, msg
		case errors.Is(app.Err, shared.ErrInvalidState):
			return http.StatusUnprocessableEntity, code, msg
		case errors.Is(app.Err, shared.ErrLocked):
			return http.StatusLocked, code, msg
		case errors.Is(app.Err, shared.ErrRateLimited):
			return http.StatusTooManyRequests, code, msg
		case errors.Is(app.Err, shared.ErrUnavailable):
			return http.StatusServiceUnavailable, code, msg
		}
	}
	switch {
	case errors.Is(err, access.ErrNoScope), errors.Is(err, access.ErrBranchForbidden):
		return http.StatusForbidden, "forbidden", "outside access scope"
	case errors.Is(err, shared.ErrNotFound):
		return http.StatusNotFound, "not_found", err.Error()
	case errors.Is(err, shared.ErrUnauthorized):
		return http.StatusUnauthorized, "unauthorized", err.Error()
	case errors.Is(err, shared.ErrForbidden):
		return http.StatusForbidden, "forbidden", err.Error()
	case errors.Is(err, shared.ErrValidation):
		return http.StatusBadRequest, "validation_error", err.Error()
	default:
		return http.StatusInternalServerError, "internal_error", "internal server error"
	}
}
