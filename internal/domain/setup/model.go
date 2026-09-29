// Package setup models the GM first-run onboarding of a company: profile
// and branches, staff profiles, AI provider and messaging channels.
package setup

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

type StepKey string

const (
	StepCompany  StepKey = "company"
	StepStaff    StepKey = "staff"
	StepAI       StepKey = "ai"
	StepChannels StepKey = "channels"
)

// Steps is the fixed onboarding order.
var Steps = []StepKey{StepCompany, StepStaff, StepAI, StepChannels}

func ParseStep(s string) (StepKey, error) {
	for _, k := range Steps {
		if string(k) == s {
			return k, nil
		}
	}
	return "", shared.NewValidation("unknown setup step")
}

type StepStatus string

const (
	StatusPending StepStatus = "pending"
	StatusDone    StepStatus = "done"
	StatusSkipped StepStatus = "skipped"
)

// State is the persisted onboarding progress of one company.
type State struct {
	CompanyID         uuid.UUID
	CompanyDoneAt     *time.Time
	StaffDoneAt       *time.Time
	AISkippedAt       *time.Time
	ChannelsSkippedAt *time.Time
	DismissedAt       *time.Time
	CompletedAt       *time.Time
	CompletedBy       *uuid.UUID
	UpdatedAt         time.Time
}

// Facts are live readiness signals owned by other modules.
type Facts struct {
	StaffCount        int
	AIReady           bool
	ChannelsConnected int
}

type StepView struct {
	Key    StepKey
	Status StepStatus
	Count  int
}

type Progress struct {
	Steps     []StepView
	Next      StepKey // empty when every step is done or skipped
	DoneCount int
	Completed bool
	// FullyDone means every step is done, none skipped: the dashboard keeps
	// offering to continue setup until then.
	FullyDone bool
	// Required means the GM should land on the setup screen after sign-in.
	Required bool
}

func stamp(at *time.Time, now time.Time) *time.Time {
	if at != nil {
		return at
	}
	return &now
}

// Evaluate merges stored progress with live facts. AI and channels count as
// done as soon as they are configured, whether or not the GM passed the step.
func Evaluate(s State, f Facts) Progress {
	status := func(done bool, skipped *time.Time) StepStatus {
		switch {
		case done:
			return StatusDone
		case skipped != nil:
			return StatusSkipped
		default:
			return StatusPending
		}
	}
	p := Progress{
		Steps: []StepView{
			{Key: StepCompany, Status: status(s.CompanyDoneAt != nil, nil)},
			{Key: StepStaff, Status: status(s.StaffDoneAt != nil, nil), Count: f.StaffCount},
			{Key: StepAI, Status: status(f.AIReady, s.AISkippedAt)},
			{Key: StepChannels, Status: status(f.ChannelsConnected > 0, s.ChannelsSkippedAt), Count: f.ChannelsConnected},
		},
		Completed: s.CompletedAt != nil,
	}
	for _, st := range p.Steps {
		if st.Status == StatusDone {
			p.DoneCount++
		}
		if st.Status == StatusPending && p.Next == "" {
			p.Next = st.Key
		}
	}
	p.Required = !p.Completed && s.DismissedAt == nil
	p.FullyDone = p.DoneCount == len(p.Steps)
	return p
}

// Advance records that the GM passed a step. Company is completed by saving
// the profile; AI and channels pass on their own once configured and need
// skip=true otherwise.
func (s *State) Advance(step StepKey, skip bool, f Facts, now time.Time) error {
	switch step {
	case StepCompany:
		if s.CompanyDoneAt == nil {
			return shared.NewValidation("save the company profile to finish this step")
		}
	case StepStaff:
		s.StaffDoneAt = stamp(s.StaffDoneAt, now)
	case StepAI:
		if !f.AIReady {
			if !skip {
				return shared.NewValidation("ai_not_ready: connect a provider or skip the step")
			}
			s.AISkippedAt = stamp(s.AISkippedAt, now)
		}
	case StepChannels:
		if f.ChannelsConnected == 0 {
			if !skip {
				return shared.NewValidation("channels_not_ready: connect a channel or skip the step")
			}
			s.ChannelsSkippedAt = stamp(s.ChannelsSkippedAt, now)
		}
	default:
		return shared.NewValidation("unknown setup step")
	}
	s.UpdatedAt = now
	return nil
}

// Complete closes onboarding. The company profile is the only hard
// requirement; any step still pending is recorded as skipped.
func (s *State) Complete(f Facts, actor uuid.UUID, now time.Time) error {
	if s.CompanyDoneAt == nil {
		return shared.NewValidation("company_required: save the company profile first")
	}
	if s.CompletedAt != nil {
		return nil
	}
	s.StaffDoneAt = stamp(s.StaffDoneAt, now)
	if !f.AIReady {
		s.AISkippedAt = stamp(s.AISkippedAt, now)
	}
	if f.ChannelsConnected == 0 {
		s.ChannelsSkippedAt = stamp(s.ChannelsSkippedAt, now)
	}
	s.CompletedAt = &now
	s.CompletedBy = &actor
	s.UpdatedAt = now
	return nil
}

// Dismiss postpones onboarding so sign-in lands on the dashboard; the setup
// screen stays reachable.
func (s *State) Dismiss(now time.Time) {
	if s.CompletedAt != nil {
		return
	}
	s.DismissedAt = stamp(s.DismissedAt, now)
	s.UpdatedAt = now
}

// Repository persists onboarding state (DIP).
type Repository interface {
	// Lock serializes setup changes of a company until the transaction ends.
	Lock(ctx context.Context, companyID uuid.UUID) error
	// GetState returns an empty state for companies that never started setup.
	GetState(ctx context.Context, companyID uuid.UUID) (*State, error)
	SaveState(ctx context.Context, s *State) error
}
