package customer

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/audit"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/customer"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type CreateInput struct {
	BranchID            uuid.UUID
	FullName            string
	FullNameAR          string
	Phone               string
	Email               string
	Nationality         string
	PassportNo          string
	DateOfBirth         *time.Time
	Preferences         json.RawMessage
	SpecialRequirements string
	Notes               string
	CreatedBy           uuid.UUID
}

type CreateResult struct {
	Customer      *domain.Customer
	DuplicateWarn bool
	Duplicates    []domain.DuplicateMatch
}

type UpdateInput struct {
	ID                  uuid.UUID
	FullName            *string
	FullNameAR          *string
	Phone               *string
	Email               *string
	Nationality         *string
	PassportNo          *string
	DateOfBirth         *time.Time
	ClearDOB            bool
	Preferences         json.RawMessage
	SpecialRequirements *string
	Notes               *string
	ActorID             uuid.UUID
	IP                  string
	UserAgent           string
}

type MergeInput struct {
	SourceID  uuid.UUID // absorbed
	TargetID  uuid.UUID // survivor
	ActorID   uuid.UUID
	IP        string
	UserAgent string
}

type LinkCompanionInput struct {
	CustomerID  uuid.UUID
	CompanionID uuid.UUID
	Relation    string
	Notes       string
	ActorID     uuid.UUID
}

type Service struct {
	repo  domain.Repository
	tx    *tx.Manager
	audit audit.Recorder
}

func NewService(repo domain.Repository, txm *tx.Manager) *Service {
	return &Service{repo: repo, tx: txm}
}

func (s *Service) SetAuditor(a audit.Recorder) { s.audit = a }

func (s *Service) Create(ctx context.Context, in CreateInput) (*CreateResult, error) {
	name := strings.TrimSpace(in.FullName)
	phone := shared.NormalizePhone(in.Phone)
	if name == "" || phone == "" {
		return nil, shared.NewValidation("full_name and phone are required")
	}
	email := shared.NormalizeEmail(in.Email)
	passport := shared.NormalizePassport(in.PassportNo)

	dups, err := s.FindDuplicates(ctx, in.BranchID, name, phone, email, passport)
	if err != nil {
		return nil, err
	}

	prefs := in.Preferences
	if len(prefs) == 0 {
		prefs = json.RawMessage(`{}`)
	}
	now := time.Now().UTC()
	c := &domain.Customer{
		ID: uuid.New(), BranchID: in.BranchID, FullName: name,
		FullNameAR: strings.TrimSpace(in.FullNameAR), Phone: phone, Email: email,
		Nationality: strings.TrimSpace(in.Nationality), PassportNo: passport,
		DateOfBirth: in.DateOfBirth, Preferences: prefs,
		SpecialRequirements: strings.TrimSpace(in.SpecialRequirements),
		Notes: in.Notes, IsActive: true, CreatedBy: in.CreatedBy,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		return s.repo.Create(ctx, c)
	}); err != nil {
		return nil, err
	}
	return &CreateResult{Customer: c, DuplicateWarn: len(dups) > 0, Duplicates: dups}, nil
}

func (s *Service) Update(ctx context.Context, in UpdateInput) (*domain.Customer, error) {
	c, err := s.repo.FindByID(ctx, in.ID)
	if err != nil || c.MergedIntoID != nil {
		return nil, shared.NewNotFound("customer")
	}
	before := snapshot(c)
	if in.FullName != nil {
		c.FullName = strings.TrimSpace(*in.FullName)
	}
	if in.FullNameAR != nil {
		c.FullNameAR = strings.TrimSpace(*in.FullNameAR)
	}
	if in.Phone != nil {
		c.Phone = shared.NormalizePhone(*in.Phone)
	}
	if in.Email != nil {
		c.Email = shared.NormalizeEmail(*in.Email)
	}
	if in.Nationality != nil {
		c.Nationality = strings.TrimSpace(*in.Nationality)
	}
	if in.PassportNo != nil {
		c.PassportNo = shared.NormalizePassport(*in.PassportNo)
	}
	if in.ClearDOB {
		c.DateOfBirth = nil
	} else if in.DateOfBirth != nil {
		c.DateOfBirth = in.DateOfBirth
	}
	if in.Preferences != nil {
		c.Preferences = in.Preferences
	}
	if in.SpecialRequirements != nil {
		c.SpecialRequirements = strings.TrimSpace(*in.SpecialRequirements)
	}
	if in.Notes != nil {
		c.Notes = *in.Notes
	}
	if c.FullName == "" || c.Phone == "" {
		return nil, shared.NewValidation("full_name and phone are required")
	}
	c.UpdatedAt = time.Now().UTC()
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.Update(ctx, c); err != nil {
			return err
		}
		if s.audit != nil {
			id := c.ID
			branch := c.BranchID
			return s.audit.Record(ctx, audit.RecordInput{
				ActorID: in.ActorID, Action: "customer.updated", EntityType: "customer",
				EntityID: &id, BranchID: &branch, Before: before, After: snapshot(c),
				IP: in.IP, UserAgent: in.UserAgent,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*domain.Customer, error) {
	c, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, shared.NewNotFound("customer")
	}
	return c, nil
}

func (s *Service) Search(ctx context.Context, f domain.SearchFilter) ([]domain.Customer, int, error) {
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 20
	}
	items, total, err := s.repo.Search(ctx, f)
	if err != nil {
		return nil, 0, err
	}
	for i := range items {
		items[i].PassportNo = shared.MaskPassport(items[i].PassportNo)
	}
	return items, total, nil
}

func (s *Service) FindDuplicates(ctx context.Context, branchID uuid.UUID, name, phone, email, passport string) ([]domain.DuplicateMatch, error) {
	seen := map[uuid.UUID]*domain.DuplicateMatch{}
	add := func(c *domain.Customer, reason string, score int) {
		if c == nil {
			return
		}
		m, ok := seen[c.ID]
		if !ok {
			cp := *c
			cp.PassportNo = shared.MaskPassport(cp.PassportNo)
			m = &domain.DuplicateMatch{Customer: &cp, Score: score}
			seen[c.ID] = m
		}
		m.Reasons = appendUnique(m.Reasons, reason)
		if score > m.Score {
			m.Score = score
		}
	}

	if phone != "" {
		if c, err := s.repo.FindByPhone(ctx, phone, branchID); err == nil {
			add(c, "phone", 100)
		}
	}
	if email != "" {
		if c, err := s.repo.FindByEmail(ctx, email, branchID); err == nil {
			add(c, "email", 95)
		}
	}
	if passport != "" {
		if c, err := s.repo.FindByPassport(ctx, passport, branchID); err == nil {
			add(c, "passport", 100)
		}
	}
	if name != "" {
		cands, err := s.repo.FindNameCandidates(ctx, name, branchID, 25)
		if err != nil {
			return nil, err
		}
		for i := range cands {
			score := shared.NameSimilarity(name, cands[i].FullName)
			arScore := shared.NameSimilarity(name, cands[i].FullNameAR)
			if arScore > score {
				score = arScore
			}
			if score >= 70 {
				add(&cands[i], "name_fuzzy", score)
			}
		}
	}
	out := make([]domain.DuplicateMatch, 0, len(seen))
	for _, m := range seen {
		out = append(out, *m)
	}
	return out, nil
}

func (s *Service) Merge(ctx context.Context, in MergeInput) (*domain.Customer, error) {
	if in.SourceID == in.TargetID {
		return nil, shared.NewValidation("source and target must differ")
	}
	source, err := s.repo.FindByID(ctx, in.SourceID)
	if err != nil {
		return nil, shared.NewNotFound("source customer")
	}
	target, err := s.repo.FindByID(ctx, in.TargetID)
	if err != nil || target.MergedIntoID != nil {
		return nil, shared.NewNotFound("target customer")
	}
	if source.BranchID != target.BranchID {
		return nil, shared.NewValidation("customers must share branch")
	}
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.ReassignLeads(ctx, in.SourceID, in.TargetID); err != nil {
			return err
		}
		if err := s.repo.ReassignBookings(ctx, in.SourceID, in.TargetID); err != nil {
			return err
		}
		// Move companion links from source → target (best-effort)
		links, _ := s.repo.ListCompanions(ctx, in.SourceID)
		for _, l := range links {
			_ = s.repo.LinkCompanion(ctx, &domain.CompanionLink{
				CustomerID: in.TargetID, CompanionID: l.CompanionID, Relation: l.Relation, Notes: l.Notes,
			})
			_ = s.repo.UnlinkCompanion(ctx, in.SourceID, l.CompanionID)
		}
		if err := s.repo.MarkMerged(ctx, in.SourceID, in.TargetID); err != nil {
			return err
		}
		if s.audit != nil {
			id := in.TargetID
			branch := target.BranchID
			return s.audit.Record(ctx, audit.RecordInput{
				ActorID: in.ActorID, Action: "customer.merged", EntityType: "customer",
				EntityID: &id, BranchID: &branch,
				Before: map[string]any{"source_id": in.SourceID},
				After:  map[string]any{"target_id": in.TargetID},
				IP: in.IP, UserAgent: in.UserAgent,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.repo.FindByID(ctx, in.TargetID)
}

func (s *Service) ListCompanions(ctx context.Context, customerID uuid.UUID) ([]domain.CompanionLink, error) {
	if _, err := s.repo.FindByID(ctx, customerID); err != nil {
		return nil, shared.NewNotFound("customer")
	}
	return s.repo.ListCompanions(ctx, customerID)
}

func (s *Service) LinkCompanion(ctx context.Context, in LinkCompanionInput) (*domain.CompanionLink, error) {
	if in.CustomerID == in.CompanionID {
		return nil, shared.NewValidation("cannot link customer to self")
	}
	if _, err := s.repo.FindByID(ctx, in.CustomerID); err != nil {
		return nil, shared.NewNotFound("customer")
	}
	if _, err := s.repo.FindByID(ctx, in.CompanionID); err != nil {
		return nil, shared.NewNotFound("companion")
	}
	rel := strings.TrimSpace(in.Relation)
	if rel == "" {
		rel = "family"
	}
	link := &domain.CompanionLink{
		ID: uuid.New(), CustomerID: in.CustomerID, CompanionID: in.CompanionID,
		Relation: rel, Notes: in.Notes, CreatedAt: time.Now().UTC(),
	}
	// bidirectional for family graph convenience
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.LinkCompanion(ctx, link); err != nil {
			return err
		}
		return s.repo.LinkCompanion(ctx, &domain.CompanionLink{
			ID: uuid.New(), CustomerID: in.CompanionID, CompanionID: in.CustomerID,
			Relation: rel, Notes: in.Notes, CreatedAt: time.Now().UTC(),
		})
	})
	if err != nil {
		return nil, err
	}
	return link, nil
}

func (s *Service) UnlinkCompanion(ctx context.Context, customerID, companionID uuid.UUID) error {
	return s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.UnlinkCompanion(ctx, customerID, companionID); err != nil {
			return err
		}
		return s.repo.UnlinkCompanion(ctx, companionID, customerID)
	})
}

func (s *Service) Timeline(ctx context.Context, customerID uuid.UUID, limit int) ([]domain.TimelineItem, error) {
	if _, err := s.repo.FindByID(ctx, customerID); err != nil {
		return nil, shared.NewNotFound("customer")
	}
	return s.repo.ListTimeline(ctx, customerID, limit)
}

func snapshot(c *domain.Customer) map[string]any {
	return map[string]any{
		"full_name": c.FullName, "phone": c.Phone, "email": c.Email,
		"passport_no": shared.MaskPassport(c.PassportNo), "nationality": c.Nationality,
	}
}

func appendUnique(xs []string, v string) []string {
	for _, x := range xs {
		if x == v {
			return xs
		}
	}
	return append(xs, v)
}
