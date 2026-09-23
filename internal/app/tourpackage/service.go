package tourpackage

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	bookingdomain "github.com/wodi-crm/wodi-crm-be/internal/domain/booking"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/tourpackage"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

type CreatePackageInput struct {
	BranchID    uuid.UUID
	Code        string
	NameEN      string
	NameAR      string
	Description string
}

type UpdatePackageInput struct {
	ID          uuid.UUID
	Code        *string
	NameEN      *string
	NameAR      *string
	Description *string
	IsActive    *bool
}

type ClonePackageInput struct {
	SourceID uuid.UUID
	BranchID uuid.UUID
	Code     string
	NameEN   string
	NameAR   string
}

type CreateDepartureInput struct {
	PackageID        uuid.UUID
	Code             string
	DepartDate       time.Time
	ReturnDate       time.Time
	CapacityTotal    int
	BasePrice        int64
	Currency         string
	SoftThresholdPct int
	AllowOversell    bool
}

type UpdateDepartureInput struct {
	ID               uuid.UUID
	Code             *string
	DepartDate       *time.Time
	ReturnDate       *time.Time
	CapacityTotal    *int
	BasePrice        *int64
	Currency         *string
	IsActive         *bool
	SoftThresholdPct *int
	AllowOversell    *bool
}

type CloneDepartureInput struct {
	SourceID   uuid.UUID
	Code       string
	DepartDate time.Time
	ReturnDate time.Time
}

type TierInput struct {
	Code      string
	Label     string
	Kind      string
	Amount    int64
	Currency  string
	IsActive  bool
}

// BookingReader is DIP port for readiness / capacity sync (T-050, T-057).
type BookingReader interface {
	ListByDeparture(ctx context.Context, departureID uuid.UUID) ([]bookingdomain.Booking, error)
	CountConfirmedPaxByDeparture(ctx context.Context, departureID uuid.UUID) (int, error)
}

type Service struct {
	repo     domain.Repository
	tx       tx.Runner
	bookings BookingReader
}

func NewService(repo domain.Repository, txm tx.Runner) *Service {
	return &Service{repo: repo, tx: txm}
}

func (s *Service) SetBookingReader(b BookingReader) { s.bookings = b }

func (s *Service) CreatePackage(ctx context.Context, in CreatePackageInput) (*domain.Package, error) {
	code := strings.TrimSpace(in.Code)
	name := strings.TrimSpace(in.NameEN)
	if code == "" || name == "" {
		return nil, shared.NewValidation("code and name_en are required")
	}
	now := time.Now().UTC()
	p := &domain.Package{
		ID: uuid.New(), BranchID: in.BranchID, Code: code, NameEN: name,
		NameAR: strings.TrimSpace(in.NameAR), Description: strings.TrimSpace(in.Description),
		IsActive: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		return s.repo.CreatePackage(ctx, p)
	}); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Service) UpdatePackage(ctx context.Context, in UpdatePackageInput) (*domain.Package, error) {
	p, err := s.repo.FindPackage(ctx, in.ID)
	if err != nil {
		return nil, shared.NewNotFound("package")
	}
	if in.Code != nil {
		p.Code = strings.TrimSpace(*in.Code)
	}
	if in.NameEN != nil {
		p.NameEN = strings.TrimSpace(*in.NameEN)
	}
	if in.NameAR != nil {
		p.NameAR = strings.TrimSpace(*in.NameAR)
	}
	if in.Description != nil {
		p.Description = strings.TrimSpace(*in.Description)
	}
	if in.IsActive != nil {
		p.IsActive = *in.IsActive
	}
	if p.Code == "" || p.NameEN == "" {
		return nil, shared.NewValidation("code and name_en are required")
	}
	p.UpdatedAt = time.Now().UTC()
	if err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		return s.repo.UpdatePackage(ctx, p)
	}); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Service) ClonePackage(ctx context.Context, in ClonePackageInput) (*domain.Package, error) {
	src, err := s.repo.FindPackage(ctx, in.SourceID)
	if err != nil {
		return nil, shared.NewNotFound("package")
	}
	code := strings.TrimSpace(in.Code)
	name := strings.TrimSpace(in.NameEN)
	if code == "" {
		code = src.Code + "-COPY"
	}
	if name == "" {
		name = src.NameEN + " (copy)"
	}
	now := time.Now().UTC()
	p := &domain.Package{
		ID: uuid.New(), BranchID: in.BranchID, Code: code, NameEN: name,
		NameAR: strings.TrimSpace(in.NameAR), Description: src.Description,
		IsActive: true, CreatedAt: now, UpdatedAt: now,
	}
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.CreatePackage(ctx, p); err != nil {
			return err
		}
		tiers, err := s.repo.ListPackageTiers(ctx, src.ID)
		if err != nil {
			return err
		}
		return s.repo.ReplacePackageTiers(ctx, p.ID, tiers)
	})
	return p, err
}

func (s *Service) ListPackages(ctx context.Context, branchID uuid.UUID, activeOnly bool) ([]domain.Package, error) {
	return s.repo.ListPackages(ctx, branchID, activeOnly)
}

func (s *Service) GetPackage(ctx context.Context, id uuid.UUID) (*domain.Package, error) {
	p, err := s.repo.FindPackage(ctx, id)
	if err != nil {
		return nil, shared.NewNotFound("package")
	}
	return p, nil
}

func (s *Service) SetPackageTiers(ctx context.Context, packageID uuid.UUID, inputs []TierInput) ([]domain.PricingTier, error) {
	if _, err := s.repo.FindPackage(ctx, packageID); err != nil {
		return nil, shared.NewNotFound("package")
	}
	tiers, err := normalizeTiers(inputs)
	if err != nil {
		return nil, err
	}
	if err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		return s.repo.ReplacePackageTiers(ctx, packageID, tiers)
	}); err != nil {
		return nil, err
	}
	return s.repo.ListPackageTiers(ctx, packageID)
}

func (s *Service) ListPackageTiers(ctx context.Context, packageID uuid.UUID) ([]domain.PricingTier, error) {
	if _, err := s.repo.FindPackage(ctx, packageID); err != nil {
		return nil, shared.NewNotFound("package")
	}
	return s.repo.ListPackageTiers(ctx, packageID)
}

func (s *Service) CreateDeparture(ctx context.Context, in CreateDepartureInput) (*domain.Departure, error) {
	if _, err := s.repo.FindPackage(ctx, in.PackageID); err != nil {
		return nil, shared.NewNotFound("package")
	}
	if err := domain.ValidateDepartureDates(in.DepartDate, in.ReturnDate); err != nil {
		return nil, err
	}
	code := strings.TrimSpace(in.Code)
	if code == "" {
		return nil, shared.NewValidation("code is required")
	}
	if in.CapacityTotal < 0 {
		return nil, shared.NewValidation("capacity_total must be >= 0")
	}
	currency := in.Currency
	if currency == "" {
		currency = "USD"
	}
	th := in.SoftThresholdPct
	if th <= 0 {
		th = 80
	}
	now := time.Now().UTC()
	d := &domain.Departure{
		ID: uuid.New(), PackageID: in.PackageID, Code: code,
		DepartDate: in.DepartDate, ReturnDate: in.ReturnDate,
		CapacityTotal: in.CapacityTotal, CapacitySold: 0,
		BasePrice: in.BasePrice, Currency: currency, IsActive: true,
		SoftThresholdPct: th, AllowOversell: in.AllowOversell,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.CreateDeparture(ctx, d); err != nil {
			return err
		}
		return s.repo.SnapshotTiersToDeparture(ctx, in.PackageID, d.ID)
	}); err != nil {
		return nil, err
	}
	return d, nil
}

func (s *Service) UpdateDeparture(ctx context.Context, in UpdateDepartureInput) (*domain.Departure, error) {
	d, err := s.repo.FindDeparture(ctx, in.ID)
	if err != nil {
		return nil, shared.NewNotFound("departure")
	}
	if in.Code != nil {
		d.Code = strings.TrimSpace(*in.Code)
	}
	if in.DepartDate != nil {
		d.DepartDate = *in.DepartDate
	}
	if in.ReturnDate != nil {
		d.ReturnDate = *in.ReturnDate
	}
	if err := domain.ValidateDepartureDates(d.DepartDate, d.ReturnDate); err != nil {
		return nil, err
	}
	if in.CapacityTotal != nil {
		if *in.CapacityTotal < d.CapacitySold && !d.AllowOversell {
			return nil, shared.NewValidation("capacity_total cannot be below capacity_sold")
		}
		d.CapacityTotal = *in.CapacityTotal
	}
	if in.BasePrice != nil || in.Currency != nil {
		if d.PricingLocked() {
			return nil, shared.NewInvalidState("departure pricing is locked after sales")
		}
		if in.BasePrice != nil {
			d.BasePrice = *in.BasePrice
		}
		if in.Currency != nil {
			d.Currency = strings.TrimSpace(*in.Currency)
		}
	}
	if in.IsActive != nil {
		d.IsActive = *in.IsActive
	}
	if in.SoftThresholdPct != nil {
		d.SoftThresholdPct = *in.SoftThresholdPct
	}
	if in.AllowOversell != nil {
		d.AllowOversell = *in.AllowOversell
	}
	d.UpdatedAt = time.Now().UTC()
	if err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		return s.repo.UpdateDeparture(ctx, d)
	}); err != nil {
		return nil, err
	}
	return d, nil
}

func (s *Service) CloneDeparture(ctx context.Context, in CloneDepartureInput) (*domain.Departure, error) {
	src, err := s.repo.FindDeparture(ctx, in.SourceID)
	if err != nil {
		return nil, shared.NewNotFound("departure")
	}
	if err := domain.ValidateDepartureDates(in.DepartDate, in.ReturnDate); err != nil {
		return nil, err
	}
	code := strings.TrimSpace(in.Code)
	if code == "" {
		return nil, shared.NewValidation("code is required")
	}
	d := src.Clone(uuid.New(), code, in.DepartDate, in.ReturnDate)
	err = s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if err := s.repo.CreateDeparture(ctx, d); err != nil {
			return err
		}
		tiers, err := s.repo.ListDepartureTiers(ctx, src.ID)
		if err != nil || len(tiers) == 0 {
			return s.repo.SnapshotTiersToDeparture(ctx, src.PackageID, d.ID)
		}
		return s.repo.ReplaceDepartureTiers(ctx, d.ID, tiers)
	})
	return d, err
}

func (s *Service) GetDeparture(ctx context.Context, id uuid.UUID) (*domain.Departure, error) {
	d, err := s.repo.FindDeparture(ctx, id)
	if err != nil {
		return nil, shared.NewNotFound("departure")
	}
	return d, nil
}

func (s *Service) ListDepartures(ctx context.Context, packageID uuid.UUID) ([]domain.Departure, error) {
	return s.repo.ListDepartures(ctx, packageID)
}

func (s *Service) ListDepartureTiers(ctx context.Context, departureID uuid.UUID) ([]domain.PricingTier, error) {
	if _, err := s.repo.FindDeparture(ctx, departureID); err != nil {
		return nil, shared.NewNotFound("departure")
	}
	return s.repo.ListDepartureTiers(ctx, departureID)
}

func (s *Service) CloseSales(ctx context.Context, departureID uuid.UUID, closed bool) (*domain.Departure, error) {
	d, err := s.repo.FindDeparture(ctx, departureID)
	if err != nil {
		return nil, shared.NewNotFound("departure")
	}
	d.SalesClosed = closed
	d.UpdatedAt = time.Now().UTC()
	if err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		return s.repo.UpdateDeparture(ctx, d)
	}); err != nil {
		return nil, err
	}
	return d, nil
}

func (s *Service) MarkFull(ctx context.Context, departureID uuid.UUID) (*domain.Departure, error) {
	d, err := s.repo.FindDeparture(ctx, departureID)
	if err != nil {
		return nil, shared.NewNotFound("departure")
	}
	d.SalesClosed = true
	d.UpdatedAt = time.Now().UTC()
	if err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		return s.repo.UpdateDeparture(ctx, d)
	}); err != nil {
		return nil, err
	}
	return d, nil
}

// RecomputeCapacity syncs capacity_sold from confirmed bookings (T-050).
func (s *Service) RecomputeCapacity(ctx context.Context, departureID uuid.UUID) (*domain.Departure, error) {
	if s.bookings == nil {
		return nil, shared.NewValidation("booking reader not configured")
	}
	sold, err := s.bookings.CountConfirmedPaxByDeparture(ctx, departureID)
	if err != nil {
		return nil, err
	}
	if err := s.repo.UpdateDepartureCapacitySold(ctx, departureID, sold); err != nil {
		return nil, err
	}
	return s.GetDeparture(ctx, departureID)
}

func (s *Service) SyncCapacitySold(ctx context.Context, departureID uuid.UUID, sold int) error {
	if sold < 0 {
		return shared.NewValidation("sold capacity cannot be negative")
	}
	return s.repo.UpdateDepartureCapacitySold(ctx, departureID, sold)
}

func (s *Service) Readiness(ctx context.Context, departureID uuid.UUID) (*domain.Readiness, error) {
	d, err := s.GetDeparture(ctx, departureID)
	if err != nil {
		return nil, err
	}
	r := &domain.Readiness{
		DepartureID:   d.ID,
		CapacityTotal: d.CapacityTotal,
		CapacitySold:  d.CapacitySold,
		Remaining:     d.Remaining(),
		Alert:         d.CapacityAlert(),
		SalesClosed:   d.SalesClosed,
		PricingLocked: d.PricingLocked(),
	}
	if s.bookings == nil {
		return r, nil
	}
	books, err := s.bookings.ListByDeparture(ctx, departureID)
	if err != nil {
		return nil, err
	}
	r.BookingsTotal = len(books)
	for _, b := range books {
		switch b.Status {
		case bookingdomain.StatusDraft:
			r.BookingsDraft++
		case bookingdomain.StatusConfirmed:
			r.BookingsConfirmed++
			r.PaxConfirmed += b.PaxCount
		case bookingdomain.StatusCancelled:
			r.BookingsCancelled++
		}
	}
	return r, nil
}

func normalizeTiers(inputs []TierInput) ([]domain.PricingTier, error) {
	out := make([]domain.PricingTier, 0, len(inputs))
	seen := map[string]struct{}{}
	now := time.Now().UTC()
	for i, in := range inputs {
		code := strings.TrimSpace(in.Code)
		kind := strings.TrimSpace(in.Kind)
		if code == "" || !domain.ValidTierKind(kind) {
			return nil, shared.NewValidation("tier code and kind (room|occupancy|age) are required")
		}
		if _, ok := seen[code]; ok {
			return nil, shared.NewValidation("duplicate tier code: " + code)
		}
		seen[code] = struct{}{}
		cur := strings.TrimSpace(in.Currency)
		if cur == "" {
			cur = "USD"
		}
		out = append(out, domain.PricingTier{
			ID: uuid.New(), Code: code, Label: strings.TrimSpace(in.Label), Kind: kind,
			Amount: in.Amount, Currency: cur, SortOrder: i, IsActive: in.IsActive,
			CreatedAt: now, UpdatedAt: now,
		})
	}
	return out, nil
}
