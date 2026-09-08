package lead_test

import (
	"errors"
	"testing"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/lead"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

func TestLeadTransitions(t *testing.T) {
	l := &lead.Lead{Stage: lead.StageNew}
	if err := l.TransitionTo(lead.StageContacted); err != nil {
		t.Fatalf("new→contacted: %v", err)
	}
	err := l.TransitionTo(lead.StageWon)
	if err == nil {
		t.Fatal("expected invalid jump contacted→won")
	}
	var app *shared.AppError
	if !errors.As(err, &app) || !errors.Is(app.Err, shared.ErrInvalidState) {
		t.Fatalf("want invalid_state, got %v", err)
	}
	if err := l.TransitionTo(lead.StageQualified); err != nil {
		t.Fatalf("contacted→qualified: %v", err)
	}
}

func TestCanTransitionTable(t *testing.T) {
	cases := []struct {
		from, to lead.Stage
		ok       bool
	}{
		{lead.StageNew, lead.StageContacted, true},
		{lead.StageNew, lead.StageWon, false},
		{lead.StageProposal, lead.StageLost, true},
		{lead.StageWon, lead.StageLost, false},
	}
	for _, tc := range cases {
		if got := lead.CanTransition(tc.from, tc.to); got != tc.ok {
			t.Fatalf("%s→%s got %v want %v", tc.from, tc.to, got, tc.ok)
		}
	}
}
