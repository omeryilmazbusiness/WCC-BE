package company

import (
	"errors"
	"testing"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

func TestNormalizeIdleLogout(t *testing.T) {
	for _, n := range []int{0, 5, 15, 120} {
		got, err := NormalizeIdleLogout(n)
		if err != nil || got != n {
			t.Fatalf("%d: got %d %v", n, got, err)
		}
	}
	for _, n := range []int{-1, 1, 4, 121} {
		_, err := NormalizeIdleLogout(n)
		var app *shared.AppError
		if !errors.As(err, &app) || app.Details["idle_logout_minutes"] == nil {
			t.Fatalf("%d: want field error, got %v", n, err)
		}
	}
}
