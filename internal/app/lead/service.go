package lead

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/lead"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/events"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type CreateInput struct {
	BranchID   uuid.UUID
	CustomerID *uuid.UUID
	FullName   string
	Phone      string
	Source     string
	OwnerID    uuid.UUID
	Notes      string
	ActorID    uuid.UUID
	IP         string
	UserAgent  string
}

type ChangeStageInput struct {
	LeadID         uuid.UUID
	To             domain.Stage
	LostReasonCode string
	LostReasonNote string
	Note           string
	ActorID        uuid.UUID
	IP             string
	UserAgent      string
}

type AssignInput struct {
	LeadIDs   []uuid.UUID
	OwnerID   uuid.UUID
	ActorID   uuid.UUID
	IP        string
	UserAgent string
}

type ConvertInput struct {
	LeadID      uuid.UUID
	DepartureID uuid.UUID
	PaxCount    int
	TotalAmount int64
	Currency    string
	ActorID     uuid.UUID
	IP          string
	UserAgent   string
}

type ConvertResult struct {
	Lead      *domain.Lead
	BookingID uuid.UUID
}

type SetNoFollowUpInput struct {
	LeadID     uuid.UUID
	NoFollowUp bool
	ActorID    uuid.UUID
}

// BookingDraftCreator is the DIP port used to convert a won lead into a draft booking (T-037).
type BookingDraftCreator interface {
	CreateDraftFromLead(ctx context.Context, in ConvertBookingInput) (uuid.UUID, error)
}

type ConvertBookingInput struct {
	BranchID    uuid.UUID
	CustomerID  uuid.UUID
	DepartureID uuid.UUID
	LeadID      uuid.UUID
	PaxCount    int
	TotalAmount int64
	Currency    string
	OwnerID     uuid.UUID
}

type Service struct {
	repo     domain.Repository
	tx       tx.Runner
	bus      *events.Bus
	audit    audit.Recorder
	bookings BookingDraftCreator
}

func NewService(repo domain.Repository, txm tx.Runner, bus *events.Bus) *Service {
	return &Service{repo: repo, tx: txm, bus: bus}
}

func (s *Service) SetAuditor(a audit.Recorder)          { s.audit = a }
func (s *Service) SetBookingCreator(b BookingDraftCreator) { s.bookings = b }

func (s *Service) Create(ctx context.Context, in CreateInput) (*domain.Lead, error) {
	name := strings.TrimSpace(in.FullName)
	phone := shared.NormalizePhone(in.Phone)
	if name == "" || phone == "" {
		return nil, shared.NewValidation("full_name and phone are required")
	}
	if in.OwnerID == uuid.Nil {
		return nil, shared.NewValidation("owner_id is required")
	}
	now := time.Now().UTC()
	l := &domain.Lead{
		ID:         uuid.New(),
		BranchID:   in.BranchID,
		CustomerID: in.CustomerID,
		FullName:   name,
		Phone:      phone,
		Source:     strings.TrimSpace(in.Source),
		Stage:      domain.StageNew,
		OwnerID:    in.OwnerID,
		Notes:      in.Notes,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.Create(ctx, l); err != nil {
			return err
		}
		if err := s.repo.AppendStageHistory(ctx, &domain.StageHistory{
			ID: uuid.New(), LeadID: l.ID, FromStage: nil, ToStage: domain.StageNew,
			ChangedBy: in.ActorID, Note: "created", CreatedAt: now,
		}); err != nil {
			return err
		}
		return s.recordAudit(ctx, in.ActorID, "lead.created", l.ID, &l.BranchID, nil, l, in.IP, in.UserAgent)
	})
	if err != nil {
		return nil, err
	}
	s.bus.Publish(ctx, events.Event{Name: events.LeadCreated, Payload: l})
	return l, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*domain.Lead, error) {
	l, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, shared.NewNotFound("lead")
	}
	return l, nil
}

func (s *Service) List(ctx context.Context, f domain.ListFilter) ([]domain.Lead, int, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	return s.repo.List(ctx, f)
}

func (s *Service) History(ctx context.Context, leadID uuid.UUID) ([]domain.StageHistory, error) {
	if _, err := s.repo.FindByID(ctx, leadID); err != nil {
		return nil, shared.NewNotFound("lead")
	}
	return s.repo.ListStageHistory(ctx, leadID)
}

func (s *Service) Analytics(ctx context.Context, branchID uuid.UUID) (*domain.Analytics, error) {
	return s.repo.Analytics(ctx, branchID)
}

func (s *Service) ChangeStage(ctx context.Context, in ChangeStageInput) (*domain.Lead, error) {
	var out *domain.Lead
	var converted bool
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		l, err := s.repo.FindByID(ctx, in.LeadID)
		if err != nil {
			return shared.NewNotFound("lead")
		}
		before := *l
		from := l.Stage
		if err := l.TransitionTo(in.To); err != nil {
			return err
		}
		note := strings.TrimSpace(in.Note)
		if in.To == domain.StageLost {
			code := strings.TrimSpace(in.LostReasonCode)
			if code == "" {
				code = strings.TrimSpace(in.LostReasonNote)
			}
			if !domain.ValidLostReasonCode(code) {
				return shared.NewValidation("lost_reason_code must be one of: " + strings.Join(domain.LostReasonCodes(), ", "))
			}
			free := strings.TrimSpace(in.LostReasonNote)
			if code == domain.LostOther && free == "" && note == "" {
				return shared.NewValidation("lost reason note is required when code is other")
			}
			l.LostReasonCode = code
			if free != "" {
				l.LostReason = free
			} else if note != "" {
				l.LostReason = note
			} else {
				l.LostReason = code
			}
			if note == "" {
				note = "lost:" + code
			}
		} else {
			l.LostReasonCode = ""
			l.LostReason = ""
		}
		if in.To == domain.StageWon || in.To == domain.StageLost {
			l.NoFollowUp = false
		}
		if err := s.repo.Update(ctx, l); err != nil {
			return err
		}
		if err := s.repo.AppendStageHistory(ctx, &domain.StageHistory{
			ID: uuid.New(), LeadID: l.ID, FromStage: &from, ToStage: in.To,
			ChangedBy: in.ActorID, Note: note, CreatedAt: time.Now().UTC(),
		}); err != nil {
			return err
		}
		if err := s.recordAudit(ctx, in.ActorID, "lead.stage_changed", l.ID, &l.BranchID, &before, l, in.IP, in.UserAgent); err != nil {
			return err
		}
		out = l
		converted = in.To == domain.StageWon
		return nil
	})
	if err != nil {
		return nil, err
	}
	if converted {
		s.bus.Publish(ctx, events.Event{Name: events.LeadConverted, Payload: out})
	}
	return out, nil
}

func (s *Service) Assign(ctx context.Context, in AssignInput) ([]domain.Lead, error) {
	if in.OwnerID == uuid.Nil {
		return nil, shared.NewValidation("owner_id is required")
	}
	if len(in.LeadIDs) == 0 {
		return nil, shared.NewValidation("lead_ids required")
	}
	var out []domain.Lead
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		for _, id := range in.LeadIDs {
			l, err := s.repo.FindByID(ctx, id)
			if err != nil {
				return shared.NewNotFound("lead")
			}
			before := *l
			l.OwnerID = in.OwnerID
			l.UpdatedAt = time.Now().UTC()
			if err := s.repo.Update(ctx, l); err != nil {
				return err
			}
			if err := s.recordAudit(ctx, in.ActorID, "lead.assigned", l.ID, &l.BranchID, &before, l, in.IP, in.UserAgent); err != nil {
				return err
			}
			out = append(out, *l)
		}
		return nil
	})
	return out, err
}

func (s *Service) SetNoFollowUp(ctx context.Context, in SetNoFollowUpInput) (*domain.Lead, error) {
	var out *domain.Lead
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		l, err := s.repo.FindByID(ctx, in.LeadID)
		if err != nil {
			return shared.NewNotFound("lead")
		}
		if !l.IsOpen() {
			return shared.NewInvalidState("no_follow_up only applies to open leads")
		}
		l.NoFollowUp = in.NoFollowUp
		l.UpdatedAt = time.Now().UTC()
		if err := s.repo.Update(ctx, l); err != nil {
			return err
		}
		out = l
		return nil
	})
	return out, err
}

func (s *Service) Convert(ctx context.Context, in ConvertInput) (*ConvertResult, error) {
	if s.bookings == nil {
		return nil, shared.NewValidation("booking conversion is not configured")
	}
	if in.DepartureID == uuid.Nil {
		return nil, shared.NewValidation("departure_id is required")
	}
	if in.PaxCount <= 0 {
		in.PaxCount = 1
	}
	l, err := s.repo.FindByID(ctx, in.LeadID)
	if err != nil {
		return nil, shared.NewNotFound("lead")
	}
	if l.CustomerID == nil || *l.CustomerID == uuid.Nil {
		return nil, shared.NewValidation("lead must be linked to a customer before conversion")
	}
	if l.ConvertedBookingID != nil {
		return nil, shared.NewConflict("lead already converted")
	}

	// Move to won if still open (proposal→won, or already won).
	if l.Stage != domain.StageWon {
		l, err = s.ChangeStage(ctx, ChangeStageInput{
			LeadID: in.LeadID, To: domain.StageWon, Note: "converted to booking",
			ActorID: in.ActorID, IP: in.IP, UserAgent: in.UserAgent,
		})
		if err != nil {
			return nil, err
		}
	}

	bookingID, err := s.bookings.CreateDraftFromLead(ctx, ConvertBookingInput{
		BranchID: l.BranchID, CustomerID: *l.CustomerID, DepartureID: in.DepartureID,
		LeadID: l.ID, PaxCount: in.PaxCount, TotalAmount: in.TotalAmount,
		Currency: in.Currency, OwnerID: l.OwnerID,
	})
	if err != nil {
		return nil, err
	}

	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		fresh, err := s.repo.FindByID(ctx, l.ID)
		if err != nil {
			return err
		}
		before := *fresh
		fresh.ConvertedBookingID = &bookingID
		fresh.UpdatedAt = time.Now().UTC()
		if err := s.repo.Update(ctx, fresh); err != nil {
			return err
		}
		if err := s.recordAudit(ctx, in.ActorID, "lead.converted", fresh.ID, &fresh.BranchID, &before, fresh, in.IP, in.UserAgent); err != nil {
			return err
		}
		l = fresh
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &ConvertResult{Lead: l, BookingID: bookingID}, nil
}

func (s *Service) recordAudit(
	ctx context.Context, actor uuid.UUID, action string, entityID uuid.UUID, branch *uuid.UUID,
	before, after any, ip, ua string,
) error {
	if s.audit == nil {
		return nil
	}
	id := entityID
	return s.audit.Record(ctx, audit.RecordInput{
		ActorID: actor, Action: action, EntityType: "lead", EntityID: &id,
		BranchID: branch, Before: before, After: after, IP: ip, UserAgent: ua,
	})
}
