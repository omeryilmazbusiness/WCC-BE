package notification

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/notification"
	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
	"github.com/wodi-crm/wodi-crm-be/internal/platform/tx"
)

// UserRef is a minimal identity projection (ISP — avoid full User coupling).
type UserRef struct {
	ID    uuid.UUID
	Email string
	Role  string
}

// UserDirectory resolves recipients for escalation (DIP).
type UserDirectory interface {
	ListActiveByRoles(ctx context.Context, branchID uuid.UUID, roles []string) ([]UserRef, error)
	FindByID(ctx context.Context, id uuid.UUID) (*UserRef, error)
}

// ExternalNotifier delivers optional email/push (T-176). In-app is mandatory and separate.
type ExternalNotifier interface {
	SendEmail(ctx context.Context, toEmail, title, body string) error
	SendPush(ctx context.Context, userID uuid.UUID, title, body string) error
}

// EmitInput creates or bumps an in-app notification for one recipient.
type EmitInput struct {
	BranchID        uuid.UUID
	RecipientUserID uuid.UUID
	Kind            string
	Title           string
	Body            string
	EntityType      string
	EntityID        *uuid.UUID
	HrefHint        string
	Meta            map[string]any
	Severity        domain.Severity // optional override
}

type Service struct {
	repo   domain.Repository
	tx     tx.Runner
	users  UserDirectory
	ext    ExternalNotifier
	log    *slog.Logger
}

func NewService(repo domain.Repository, txm tx.Runner, log *slog.Logger) *Service {
	return &Service{repo: repo, tx: txm, log: log, ext: NopExternal{}}
}

func (s *Service) SetUserDirectory(d UserDirectory) { s.users = d }
func (s *Service) SetExternal(n ExternalNotifier) {
	if n != nil {
		s.ext = n
	}
}

// Emit upserts by group_key for groupable kinds (T-173 + T-175).
func (s *Service) Emit(ctx context.Context, in EmitInput) (*domain.Notification, error) {
	if in.BranchID == uuid.Nil || in.RecipientUserID == uuid.Nil || strings.TrimSpace(in.Kind) == "" {
		return nil, shared.NewValidation("branch_id, recipient_user_id and kind are required")
	}
	rule := domain.MatchRule(in.Kind)
	severity := in.Severity
	title := strings.TrimSpace(in.Title)
	href := strings.TrimSpace(in.HrefHint)
	entityType := strings.TrimSpace(in.EntityType)
	if rule != nil {
		if !domain.ValidSeverity(severity) {
			severity = rule.Severity
		}
		if title == "" {
			title = rule.DefaultTitle
		}
		if href == "" {
			href = rule.DefaultHref
		}
		if entityType == "" {
			entityType = rule.EntityType
		}
	}
	if !domain.ValidSeverity(severity) {
		severity = domain.SeverityInfo
	}
	if title == "" {
		title = in.Kind
	}
	groupKey := domain.BuildGroupKey(in.Kind, in.BranchID, entityType, in.EntityID)
	meta, _ := json.Marshal(in.Meta)
	if len(meta) == 0 {
		meta = []byte(`{}`)
	}

	var out *domain.Notification
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		if groupKey != "" {
			existing, err := s.repo.FindOpenByGroup(ctx, in.RecipientUserID, groupKey)
			if err != nil {
				return err
			}
			if existing != nil {
				existing.BumpOccurrence(title, strings.TrimSpace(in.Body), severity)
				existing.HrefHint = href
				existing.MetaJSON = meta
				if err := s.repo.Update(ctx, existing); err != nil {
					return err
				}
				out = existing
				return nil
			}
		}
		now := time.Now().UTC()
		n := &domain.Notification{
			ID: uuid.New(), BranchID: in.BranchID, RecipientUserID: in.RecipientUserID,
			Kind: in.Kind, Severity: severity, Title: title, Body: strings.TrimSpace(in.Body),
			EntityType: entityType, EntityID: in.EntityID, GroupKey: groupKey,
			OccurrenceCount: 1, Status: domain.StatusOpen, HrefHint: href, MetaJSON: meta,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := s.repo.Create(ctx, n); err != nil {
			return err
		}
		out = n
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.deliverExternal(ctx, out)
	return out, nil
}

// EmitToRoles fans out to active users in roles for a branch.
func (s *Service) EmitToRoles(ctx context.Context, branchID uuid.UUID, roles []string, in EmitInput) (int, error) {
	if s.users == nil || len(roles) == 0 {
		return 0, nil
	}
	users, err := s.users.ListActiveByRoles(ctx, branchID, roles)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, u := range users {
		in.BranchID = branchID
		in.RecipientUserID = u.ID
		if _, err := s.Emit(ctx, in); err != nil {
			if s.log != nil {
				s.log.Error("notification emit failed", "user", u.ID, "error", err)
			}
			continue
		}
		n++
	}
	return n, nil
}

func (s *Service) deliverExternal(ctx context.Context, n *domain.Notification) {
	if n == nil || s.ext == nil {
		return
	}
	prefs, err := s.GetPreferences(ctx, n.RecipientUserID)
	if err != nil || prefs == nil {
		return
	}
	title := domain.DisplayTitle(*n)
	if prefs.EmailEnabled && s.users != nil {
		if u, err := s.users.FindByID(ctx, n.RecipientUserID); err == nil && u != nil && u.Email != "" {
			_ = s.ext.SendEmail(ctx, u.Email, title, n.Body)
		}
	}
	if prefs.PushEnabled {
		_ = s.ext.SendPush(ctx, n.RecipientUserID, title, n.Body)
	}
}

func (s *Service) List(ctx context.Context, userID uuid.UUID, status domain.Status, includeResolved bool, limit, offset int) ([]domain.Notification, int, error) {
	if userID == uuid.Nil {
		return nil, 0, shared.NewValidation("user_id required")
	}
	items, total, err := s.repo.List(ctx, domain.ListFilter{
		RecipientUserID: userID, Status: status, IncludeResolved: includeResolved,
		Limit: limit, Offset: offset,
	})
	if err != nil {
		return nil, 0, err
	}
	for i := range items {
		items[i].Title = domain.DisplayTitle(items[i])
	}
	return items, total, nil
}

func (s *Service) UnreadCount(ctx context.Context, userID uuid.UUID) (int, error) {
	return s.repo.CountUnread(ctx, userID)
}

func (s *Service) Acknowledge(ctx context.Context, id, actorID uuid.UUID) (*domain.Notification, error) {
	return s.mutate(ctx, id, actorID, func(n *domain.Notification) error {
		if n.RecipientUserID != actorID {
			return shared.NewForbidden("cannot acknowledge another user's notification")
		}
		return n.Acknowledge(actorID)
	})
}

func (s *Service) Resolve(ctx context.Context, id, actorID uuid.UUID) (*domain.Notification, error) {
	return s.mutate(ctx, id, actorID, func(n *domain.Notification) error {
		if n.RecipientUserID != actorID {
			return shared.NewForbidden("cannot resolve another user's notification")
		}
		return n.Resolve(actorID)
	})
}

func (s *Service) AcknowledgeAll(ctx context.Context, actorID uuid.UUID) (int, error) {
	items, _, err := s.repo.List(ctx, domain.ListFilter{
		RecipientUserID: actorID, Status: domain.StatusOpen, Limit: 200,
	})
	if err != nil {
		return 0, err
	}
	n := 0
	for i := range items {
		if err := items[i].Acknowledge(actorID); err != nil {
			continue
		}
		if err := s.repo.Update(ctx, &items[i]); err != nil {
			continue
		}
		n++
	}
	return n, nil
}

func (s *Service) mutate(ctx context.Context, id, actorID uuid.UUID, fn func(*domain.Notification) error) (*domain.Notification, error) {
	var out *domain.Notification
	err := s.tx.WithinTransaction(ctx, func(ctx context.Context) error {
		n, err := s.repo.FindByID(ctx, id)
		if err != nil || n == nil {
			return shared.NewNotFound("notification")
		}
		if err := fn(n); err != nil {
			return err
		}
		if err := s.repo.Update(ctx, n); err != nil {
			return err
		}
		out = n
		return nil
	})
	return out, err
}

func (s *Service) GetPreferences(ctx context.Context, userID uuid.UUID) (*domain.Preference, error) {
	p, err := s.repo.GetPreference(ctx, userID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		d := domain.DefaultPreference(userID)
		return &d, nil
	}
	return p, nil
}

func (s *Service) UpdatePreferences(ctx context.Context, userID uuid.UUID, email, push bool) (*domain.Preference, error) {
	if userID == uuid.Nil {
		return nil, shared.NewValidation("user_id required")
	}
	p := &domain.Preference{
		UserID: userID, EmailEnabled: email, PushEnabled: push, UpdatedAt: time.Now().UTC(),
	}
	if err := s.repo.UpsertPreference(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Service) Rules() []domain.Rule {
	return domain.DefaultRules()
}

// ProcessEscalations promotes aged open alerts to escalate-to roles (T-174/T-175).
func (s *Service) ProcessEscalations(ctx context.Context, now time.Time) (int, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	// Look back far enough to catch slowest escalate_after (48h docs).
	items, err := s.repo.ListOpenOlderThan(ctx, now.Add(-72*time.Hour), 500)
	if err != nil {
		return 0, err
	}
	emitted := 0
	for i := range items {
		n := items[i]
		if !domain.ShouldEscalate(n.Kind, n.CreatedAt, now) {
			continue
		}
		rule := domain.MatchRule(n.Kind)
		if rule == nil || len(rule.EscalateToRoles) == 0 {
			continue
		}
		// Mark source acknowledged so it does not re-escalate every sweep.
		_ = n.Acknowledge(n.RecipientUserID)
		_ = s.repo.Update(ctx, &n)

		count, err := s.EmitToRoles(ctx, n.BranchID, rule.EscalateToRoles, EmitInput{
			Kind: n.Kind, Title: "Escalated: " + n.Title, Body: n.Body,
			EntityType: n.EntityType, EntityID: n.EntityID, HrefHint: n.HrefHint,
			Severity: domain.SeverityCritical,
			Meta:     map[string]any{"escalated_from": n.ID.String(), "source_recipient": n.RecipientUserID.String()},
		})
		if err != nil {
			continue
		}
		emitted += count
	}
	return emitted, nil
}

// NopExternal is a no-op adapter for optional channels.
type NopExternal struct{}

func (NopExternal) SendEmail(context.Context, string, string, string) error { return nil }
func (NopExternal) SendPush(context.Context, uuid.UUID, string, string) error {
	return nil
}

// LogExternal logs optional channel deliveries (dev/prod stub).
type LogExternal struct {
	Log *slog.Logger
}

func (l LogExternal) SendEmail(_ context.Context, to, title, body string) error {
	if l.Log != nil {
		l.Log.Info("notification.email", "to", to, "title", title, "body", body)
	}
	return nil
}

func (l LogExternal) SendPush(_ context.Context, userID uuid.UUID, title, body string) error {
	if l.Log != nil {
		l.Log.Info("notification.push", "user_id", userID, "title", title, "body", body)
	}
	return nil
}
