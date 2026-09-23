package importexport

import (
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

// NormalizePhone delegates to shared phone normalization.
func NormalizePhone(phone string) string {
	return shared.NormalizePhone(phone)
}

// NormalizeCurrency uppercases ISO-like currency codes.
func NormalizeCurrency(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

// NormalizeStatus lowercases and converts spaces/hyphens to snake_case.
func NormalizeStatus(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, " ", "_")
	for strings.Contains(s, "__") {
		s = strings.ReplaceAll(s, "__", "_")
	}
	return s
}

// NormalizeDate parses YYYY-MM-DD, DD/MM/YYYY, or Excel serial dates.
func NormalizeDate(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		u := t.UTC()
		return &u, nil
	}
	if t, err := time.Parse("02/01/2006", s); err == nil {
		u := t.UTC()
		return &u, nil
	}
	if t, err := time.Parse("2/1/2006", s); err == nil {
		u := t.UTC()
		return &u, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		u := t.UTC()
		return &u, nil
	}
	// Excel serial (days since 1899-12-30)
	if isNumeric(s) {
		f, err := strconv.ParseFloat(s, 64)
		if err == nil && f > 0 {
			base := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)
			days := int(f)
			frac := f - float64(days)
			t := base.AddDate(0, 0, days).Add(time.Duration(frac * float64(24*time.Hour)))
			return &t, nil
		}
	}
	return nil, shared.NewValidation("invalid date: " + s)
}

func isNumeric(s string) bool {
	dot := 0
	for _, r := range s {
		if r == '.' {
			dot++
			if dot > 1 {
				return false
			}
			continue
		}
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return len(s) > 0
}

// NormalizeMoney parses money as integer minor units when no decimal, or major×100 when decimal.
func NormalizeMoney(s string) (int64, error) {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ",", "")
	s = strings.ReplaceAll(s, " ", "")
	if s == "" {
		return 0, nil
	}
	if strings.Contains(s, ".") {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, shared.NewValidation("invalid money: " + s)
		}
		return int64(f*100 + 0.5), nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, shared.NewValidation("invalid money: " + s)
	}
	return n, nil
}

// ValidateMappedRow checks required fields and normalizes typed values for an entity.
// Returns normalized values and a list of field→message validation errors.
func ValidateMappedRow(entity EntityType, values map[string]string) (map[string]string, map[string]string) {
	catalog := FieldCatalog(entity)
	norm := make(map[string]string, len(values))
	errs := make(map[string]string)

	for _, f := range catalog {
		raw := strings.TrimSpace(values[f.Key])
		if f.Required && raw == "" {
			errs[f.Key] = f.Key + " is required"
			continue
		}
		if raw == "" {
			norm[f.Key] = ""
			continue
		}
		switch f.Type {
		case "phone":
			p := NormalizePhone(raw)
			if p == "" {
				errs[f.Key] = "invalid phone"
			} else {
				norm[f.Key] = p
			}
		case "date":
			t, err := NormalizeDate(raw)
			if err != nil {
				errs[f.Key] = err.Error()
			} else if t != nil {
				norm[f.Key] = t.Format("2006-01-02")
			}
		case "currency":
			norm[f.Key] = NormalizeCurrency(raw)
		case "status":
			norm[f.Key] = NormalizeStatus(raw)
		case "money":
			n, err := NormalizeMoney(raw)
			if err != nil {
				errs[f.Key] = err.Error()
			} else {
				norm[f.Key] = strconv.FormatInt(n, 10)
			}
		case "int":
			n, err := strconv.Atoi(strings.TrimSpace(raw))
			if err != nil {
				errs[f.Key] = "invalid integer"
			} else {
				norm[f.Key] = strconv.Itoa(n)
			}
		default:
			norm[f.Key] = raw
		}
	}
	return norm, errs
}
