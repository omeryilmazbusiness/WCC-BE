package setup

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestEvaluateFreshCompanyRequiresSetup(t *testing.T) {
	p := Evaluate(State{}, Facts{})
	if !p.Required || p.Completed || p.Next != StepCompany || p.DoneCount != 0 {
		t.Fatalf("fresh company: %+v", p)
	}
}

func TestEvaluateDerivesLiveReadiness(t *testing.T) {
	now := time.Now()
	s := State{CompanyDoneAt: &now}
	p := Evaluate(s, Facts{StaffCount: 3, AIReady: true, ChannelsConnected: 2})
	want := map[StepKey]StepStatus{StepCompany: StatusDone, StepStaff: StatusPending, StepAI: StatusDone, StepChannels: StatusDone}
	for _, st := range p.Steps {
		if st.Status != want[st.Key] {
			t.Errorf("%s: got %s want %s", st.Key, st.Status, want[st.Key])
		}
	}
	if p.Next != StepStaff || p.DoneCount != 3 || p.FullyDone {
		t.Fatalf("next/done: %+v", p)
	}
}

func TestFullyDoneNeedsEveryStepDoneNotSkipped(t *testing.T) {
	now := time.Now()
	s := State{CompanyDoneAt: &now, StaffDoneAt: &now, ChannelsSkippedAt: &now, CompletedAt: &now}
	p := Evaluate(s, Facts{AIReady: true})
	if !p.Completed || p.FullyDone || p.Required {
		t.Fatalf("skipped channels keep setup open on the dashboard: %+v", p)
	}
	p = Evaluate(s, Facts{AIReady: true, ChannelsConnected: 1})
	if !p.FullyDone || p.DoneCount != 4 {
		t.Fatalf("every step done: %+v", p)
	}
}

func TestAdvanceRequiresReadinessOrSkip(t *testing.T) {
	now := time.Now()
	s := State{}
	if err := s.Advance(StepCompany, false, Facts{}, now); err == nil {
		t.Fatal("company must be saved, not advanced")
	}
	if err := s.Advance(StepAI, false, Facts{}, now); err == nil {
		t.Fatal("ai without provider must require skip")
	}
	if err := s.Advance(StepAI, true, Facts{}, now); err != nil || s.AISkippedAt == nil {
		t.Fatalf("ai skip: %v", err)
	}
	if err := s.Advance(StepChannels, false, Facts{ChannelsConnected: 1}, now); err != nil || s.ChannelsSkippedAt != nil {
		t.Fatalf("connected channels pass without skip: %v", err)
	}
	if err := s.Advance(StepStaff, false, Facts{}, now); err != nil || s.StaffDoneAt == nil {
		t.Fatalf("staff: %v", err)
	}
}

func TestCompleteNeedsCompanyAndSkipsPending(t *testing.T) {
	now := time.Now()
	actor := uuid.New()
	s := State{}
	if err := s.Complete(Facts{}, actor, now); err == nil {
		t.Fatal("complete without company")
	}
	s.CompanyDoneAt = &now
	if err := s.Complete(Facts{AIReady: true}, actor, now); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if s.CompletedAt == nil || *s.CompletedBy != actor || s.AISkippedAt != nil || s.ChannelsSkippedAt == nil || s.StaffDoneAt == nil {
		t.Fatalf("complete state: %+v", s)
	}
	p := Evaluate(s, Facts{AIReady: true})
	if p.Required || !p.Completed || p.Next != "" {
		t.Fatalf("completed progress: %+v", p)
	}
}

func TestDismissStopsRedirectButNotAfterCompletion(t *testing.T) {
	now := time.Now()
	s := State{}
	s.Dismiss(now)
	if Evaluate(s, Facts{}).Required {
		t.Fatal("dismissed setup must not be required")
	}
	done := State{CompletedAt: &now}
	done.Dismiss(now)
	if done.DismissedAt != nil {
		t.Fatal("completed setup is not dismissed")
	}
}

func TestParseStep(t *testing.T) {
	if k, err := ParseStep("channels"); err != nil || k != StepChannels {
		t.Fatalf("parse: %v %v", k, err)
	}
	if _, err := ParseStep("billing"); err == nil {
		t.Fatal("unknown step accepted")
	}
}
