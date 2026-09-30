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
		{lead.StageProposal, lead.StagePaid, true},
		{lead.StageProposal, lead.StageWon, false},
		{lead.StagePaid, lead.StageWon, true},
		{lead.StagePaid, lead.StageLost, true},
		{lead.StagePaid, lead.StageProposal, false},
		{lead.StageWon, lead.StageLost, false},
	}
	for _, tc := range cases {
		if got := lead.CanTransition(tc.from, tc.to); got != tc.ok {
			t.Fatalf("%s→%s got %v want %v", tc.from, tc.to, got, tc.ok)
		}
	}
}

func TestConversionPath(t *testing.T) {
	cases := []struct {
		from lead.Stage
		path []lead.Stage
		ok   bool
	}{
		{lead.StageProposal, []lead.Stage{lead.StagePaid, lead.StageWon}, true},
		{lead.StagePaid, []lead.Stage{lead.StageWon}, true},
		{lead.StageWon, nil, true},
		{lead.StageQualified, nil, false},
		{lead.StageLost, nil, false},
	}
	for _, tc := range cases {
		path, ok := lead.ConversionPath(tc.from)
		if ok != tc.ok || len(path) != len(tc.path) {
			t.Fatalf("%s: path %v ok %v", tc.from, path, ok)
		}
		from := tc.from
		for i, to := range path {
			if to != tc.path[i] || !lead.CanTransition(from, to) {
				t.Fatalf("%s: step %d %s not allowed", tc.from, i, to)
			}
			from = to
		}
	}
}
