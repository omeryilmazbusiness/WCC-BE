// Package support handles help requests: users submit them, platform admins work them.
package support

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/support"
)

// Inbound is a request as the platform inbox shows it, with who sent it from where.
type Inbound struct {
	domain.Request
	RequesterName  string
	RequesterEmail string
	RequesterRole  string
	CompanyName    string
	BranchName     string
}

// Filter narrows the inbox; an empty Status means every status.
type Filter struct {
	Status domain.Status
	Query  string
	Limit  int
	Offset int
}

// Repository persists requests. Reads for requesters and for the inbox are
// separate so each side gets exactly the data it needs (ISP).
type Repository interface {
	Create(ctx context.Context, r *domain.Request) error
	Get(ctx context.Context, id uuid.UUID) (*domain.Request, error)
	Update(ctx context.Context, r *domain.Request) error
	ListByRequester(ctx context.Context, requesterID uuid.UUID, limit int) ([]domain.Request, error)
	ListInbound(ctx context.Context, f Filter) ([]Inbound, int, error)
	CountByStatus(ctx context.Context) (map[domain.Status]int, error)
}

// RateWindow counts recent events per key (platform/ratelimit satisfies it).
type RateWindow interface {
	Hit(ctx context.Context, key string, window time.Duration) (int, error)
}

// Actor is the authenticated caller.
type Actor struct {
	UserID    uuid.UUID
	BranchID  uuid.UUID
	IP        string
	UserAgent string
}

type Config struct {
	// SubmitPerHour caps new requests per user (spam guard).
	SubmitPerHour int
	Now           func() time.Time
	Logger        *slog.Logger
}

const (
	defaultSubmitPerHour = 5
	mineLimit            = 50
	maxInboxPage         = 100
)

type Service struct {
	repo    Repository
	audit   audit.Recorder
	limiter RateWindow
	cfg     Config
}

func NewService(repo Repository, rec audit.Recorder, limiter RateWindow, cfg Config) *Service {
	if cfg.SubmitPerHour <= 0 {
		cfg.SubmitPerHour = defaultSubmitPerHour
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Service{repo: repo, audit: rec, limiter: limiter, cfg: cfg}
}

// Submit records a new request for the platform team.
func (s *Service) Submit(ctx context.Context, a Actor, d domain.Draft) (*domain.Request, error) {
	r, err := domain.NewRequest(d, a.UserID, a.BranchID, s.cfg.Now())
	if err != nil {
		return nil, err
	}
	if err := s.checkRate(ctx, a.UserID); err != nil {
		return nil, err
	}
	if err := s.repo.Create(ctx, r); err != nil {
		return nil, err
	}
	s.record(ctx, a, r, "support.request_created", map[string]any{"number": r.Number, "title": r.Title})
	return r, nil
}

// Mine lists the caller's own requests, newest first.
func (s *Service) Mine(ctx context.Context, userID uuid.UUID) ([]domain.Request, error) {
	return s.repo.ListByRequester(ctx, userID, mineLimit)
}

// Inbox is one page of requests plus counts per status for the platform team.
type Inbox struct {
	Items  []Inbound
	Total  int
	Counts map[domain.Status]int
}

func (s *Service) Inbox(ctx context.Context, f Filter) (*Inbox, error) {
	if f.Status != "" && !domain.ValidStatus(f.Status) {
		return nil, shared.NewValidation("unknown status")
	}
	if f.Limit <= 0 || f.Limit > maxInboxPage {
		f.Limit = maxInboxPage
	}
	f.Offset = max(0, f.Offset)
	items, total, err := s.repo.ListInbound(ctx, f)
	if err != nil {
		return nil, err
	}
	counts, err := s.repo.CountByStatus(ctx)
	if err != nil {
		return nil, err
	}
	return &Inbox{Items: items, Total: total, Counts: counts}, nil
}

// Update changes a request's status and, when given, the note the requester sees.
func (s *Service) Update(ctx context.Context, a Actor, id uuid.UUID, status domain.Status, note *string) (*domain.Request, error) {
	r, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	before := map[string]any{"status": r.Status, "note": r.AdminNote}
	if err := r.Transition(status, note, a.UserID, s.cfg.Now()); err != nil {
		return nil, err
	}
	if err := s.repo.Update(ctx, r); err != nil {
		return nil, err
	}
	s.record(ctx, a, r, "support.request_updated", map[string]any{"before": before, "status": r.Status, "number": r.Number})
	return r, nil
}

func (s *Service) checkRate(ctx context.Context, userID uuid.UUID) error {
	if s.limiter == nil {
		return nil
	}
	n, err := s.limiter.Hit(ctx, "support:submit:"+userID.String(), time.Hour)
	if err != nil {
		s.cfg.Logger.WarnContext(ctx, "support rate window unavailable", "err", err)
		return nil
	}
	if n > s.cfg.SubmitPerHour {
		return shared.NewRateLimited("too many help requests; try again later", time.Hour)
	}
	return nil
}

// record audits without failing the request: the request itself is the source of truth.
func (s *Service) record(ctx context.Context, a Actor, r *domain.Request, action string, extra map[string]any) {
	if s.audit == nil {
		return
	}
	id, branch := r.ID, r.BranchID
	err := s.audit.Record(ctx, audit.RecordInput{
		ActorID: a.UserID, Action: action, EntityType: "support_request", EntityID: &id, BranchID: &branch,
		IP: a.IP, UserAgent: a.UserAgent, Extra: extra,
	})
	if err != nil {
		s.cfg.Logger.WarnContext(ctx, "support audit failed", "action", action, "err", err)
	}
}
