package response

import (
	"encoding/json"
	"errors"
	"net/http"

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
}

func JSON(w http.ResponseWriter, status int, data any) {
	write(w, status, envelope{Data: data})
}

func JSONMeta(w http.ResponseWriter, status int, data any, meta map[string]any) {
	write(w, status, envelope{Data: data, Meta: meta})
}

func Error(w http.ResponseWriter, err error) {
	status, code, msg := mapError(err)
	write(w, status, envelope{Error: &errorBody{Code: code, Message: msg}})
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
		}
	}
	switch {
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
