// Package bus is the coach-operator catalogue for the operations desk.
package bus

import (
	"context"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

const (
	ClassStandard = "standard"
	ClassVIP      = "vip"
	ClassSleeper  = "sleeper"
	ClassMixed    = "mixed"
)

func Classes() []string {
	return []string{ClassStandard, ClassVIP, ClassSleeper, ClassMixed}
}

const (
	MaxNameLen    = 160
	MaxNotesLen   = 2000
	MaxCountryLen = 80
	MaxAddressLen = 240
)

var phonePattern = regexp.MustCompile(`^\+?[0-9][0-9 ()-]{5,19}$`)

// Company is one operator the desk can book seats with.
type Company struct {
	ID           uuid.UUID
	BranchID     uuid.UUID
	Name         string
	Phone        string
	Email        string
	Website      string
	Country      string
	City         string
	Address      string
	VehicleClass string
	LicenseNo    string
	ContactName  string
	ContactPhone string
	Notes        string
	IsActive     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ListFilter is the operations list query.
type ListFilter struct {
	BranchID   uuid.UUID
	Query      string
	ActiveOnly bool
}

// Repository persists coach operators for a branch.
type Repository interface {
	Create(ctx context.Context, c *Company) error
	Update(ctx context.Context, c *Company) error
	Find(ctx context.Context, id uuid.UUID) (*Company, error)
	List(ctx context.Context, f ListFilter) ([]Company, error)
}

func oneOf(v string, set []string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

func trimPhone(v string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(v)), " ")
}

// Normalize trims fields and validates the operator card.
func (c *Company) Normalize() error {
	c.Name = strings.Join(strings.Fields(c.Name), " ")
	c.Phone = trimPhone(c.Phone)
	c.Email = strings.TrimSpace(strings.ToLower(c.Email))
	c.Website = strings.TrimSpace(c.Website)
	c.Country = strings.Join(strings.Fields(c.Country), " ")
	c.City = strings.Join(strings.Fields(c.City), " ")
	c.Address = strings.Join(strings.Fields(c.Address), " ")
	c.VehicleClass = strings.TrimSpace(c.VehicleClass)
	c.LicenseNo = strings.TrimSpace(c.LicenseNo)
	c.ContactName = strings.Join(strings.Fields(c.ContactName), " ")
	c.ContactPhone = trimPhone(c.ContactPhone)
	c.Notes = strings.TrimSpace(c.Notes)
	if c.VehicleClass == "" {
		c.VehicleClass = ClassStandard
	}

	fields := map[string]any{}
	if n := utf8.RuneCountInString(c.Name); n < 2 || n > MaxNameLen {
		fields["name"] = "2–160 characters"
	}
	if c.Phone != "" && !phonePattern.MatchString(c.Phone) {
		fields["phone"] = "invalid phone"
	}
	if c.ContactPhone != "" && !phonePattern.MatchString(c.ContactPhone) {
		fields["contact_phone"] = "invalid phone"
	}
	if c.Email != "" {
		if _, err := mail.ParseAddress(c.Email); err != nil {
			fields["email"] = "invalid email"
		}
	}
	if utf8.RuneCountInString(c.Country) > MaxCountryLen {
		fields["country"] = "too long"
	}
	if !oneOf(c.VehicleClass, Classes()) {
		fields["vehicle_class"] = "unknown class"
	}
	if utf8.RuneCountInString(c.Address) > MaxAddressLen {
		fields["address"] = "too long"
	}
	if utf8.RuneCountInString(c.Notes) > MaxNotesLen {
		fields["notes"] = "too long"
	}
	if utf8.RuneCountInString(c.LicenseNo) > 64 {
		fields["license_no"] = "too long"
	}
	if len(fields) > 0 {
		err := shared.NewValidation("invalid bus company")
		err.Details = fields
		return err
	}
	return nil
}
