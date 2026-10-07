// Package support models help requests users send to the platform team.
package support

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// Status lifecycle: open → in_progress → resolved; a resolved request may be reopened.
type Status string

const (
	StatusOpen       Status = "open"
	StatusInProgress Status = "in_progress"
	StatusResolved   Status = "resolved"
)

// Statuses lists every status in workflow order.
func Statuses() []Status { return []Status{StatusOpen, StatusInProgress, StatusResolved} }

func ValidStatus(s Status) bool {
	for _, x := range Statuses() {
		if x == s {
			return true
		}
	}
	return false
}

// Limits mirror the database checks and the UI counters.
const (
	TitleMin       = 4
	TitleMax       = 120
	DescriptionMin = 10
	DescriptionMax = 4000
	NoteMax        = 1000
	pageMax        = 200
)

// Request is one help request.
type Request struct {
	ID          uuid.UUID
	Number      int64
	BranchID    uuid.UUID
	CompanyID   *uuid.UUID
	RequesterID uuid.UUID
	Title       string
	Description string
	Status      Status
	// Page is the app path the user was on, to reproduce the problem.
	Page       string
	Locale     string
	AdminNote  string
	ResolvedAt *time.Time
	ResolvedBy *uuid.UUID
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Draft is what the user submits.
type Draft struct {
	Title       string
	Description string
	Page        string
	Locale      string
}

// NewRequest validates a draft into an open request.
func NewRequest(d Draft, requesterID, branchID uuid.UUID, now time.Time) (*Request, error) {
	if requesterID == uuid.Nil || branchID == uuid.Nil {
		return nil, shared.NewValidation("requester and branch are required")
	}
	title := strings.Join(strings.Fields(stripControl(d.Title, false)), " ")
	desc := strings.TrimSpace(stripControl(d.Description, true))
	if n := utf8.RuneCountInString(title); n < TitleMin || n > TitleMax {
		return nil, shared.NewValidation("title must be 4 to 120 characters")
	}
	if n := utf8.RuneCountInString(desc); n < DescriptionMin || n > DescriptionMax {
		return nil, shared.NewValidation("description must be 10 to 4000 characters")
	}
	now = now.UTC()
	return &Request{
		ID: uuid.New(), BranchID: branchID, RequesterID: requesterID,
		Title: title, Description: desc, Status: StatusOpen,
		Page: cleanPage(d.Page), Locale: cleanLocale(d.Locale),
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

// Transition moves the request to `to`, optionally replacing the note shown to the requester.
func (r *Request) Transition(to Status, note *string, actor uuid.UUID, now time.Time) error {
	if !ValidStatus(to) {
		return shared.NewValidation("status must be open, in_progress or resolved")
	}
	if note != nil {
		n := strings.TrimSpace(stripControl(*note, true))
		if utf8.RuneCountInString(n) > NoteMax {
			return shared.NewValidation("note must be at most 1000 characters")
		}
		r.AdminNote = n
	}
	now = now.UTC()
	switch {
	case to == StatusResolved && r.Status != StatusResolved:
		r.ResolvedAt, r.ResolvedBy = &now, &actor
	case to != StatusResolved:
		r.ResolvedAt, r.ResolvedBy = nil, nil
	}
	r.Status = to
	r.UpdatedAt = now
	return nil
}

// stripControl drops control characters; newlines and tabs are kept in long
// text and become spaces in one-line text.
func stripControl(s string, multiline bool) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			if multiline {
				return r
			}
			return ' '
		}
		if r == '\r' || unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

// cleanPage keeps an in-app path only, so the field cannot carry links or scripts.
func cleanPage(p string) string {
	p = strings.TrimSpace(p)
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || len(p) > pageMax {
		return ""
	}
	for _, r := range p {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("/-_.", r)) {
			return ""
		}
	}
	return p
}

func cleanLocale(l string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(l)), "ar") {
		return "ar"
	}
	return "en"
}
