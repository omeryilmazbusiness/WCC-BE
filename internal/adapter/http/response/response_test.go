package response_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wodi-crm/wodi-crm-be/internal/adapter/http/response"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
)

func TestBookingTransitionErrors(t *testing.T) {
	b := &booking.Booking{Status: booking.StatusDraft}
	_, guardErr := b.Transition(booking.TransitionInput{To: booking.StatusConfirmed, Actor: booking.ActorUser})
	_, edgeErr := b.Transition(booking.TransitionInput{To: booking.StatusCompleted, Actor: booking.ActorUser})

	cases := []struct {
		err    error
		status int
		code   string
		guards bool
	}{
		{guardErr, http.StatusUnprocessableEntity, booking.CodeGuardFailed, true},
		{edgeErr, http.StatusConflict, booking.CodeInvalidTransition, false},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		response.Error(rec, c.err)
		var body struct {
			Error struct {
				Code    string `json:"code"`
				Details struct {
					Guards []string `json:"guards"`
				} `json:"details"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if rec.Code != c.status || body.Error.Code != c.code || (len(body.Error.Details.Guards) > 0) != c.guards {
			t.Fatalf("%v: %d %s", c.err, rec.Code, rec.Body.String())
		}
	}
}
