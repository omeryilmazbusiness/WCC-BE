package company

import "github.com/wodi-crm/wodi-crm-be/internal/domain/shared"

const (
	// DefaultIdleLogoutMinutes is the desk lock for a new company.
	DefaultIdleLogoutMinutes = 15
	MinIdleLogoutMinutes     = 5
	MaxIdleLogoutMinutes     = 120
)

// NormalizeIdleLogout accepts 0 (off) or 5–120 minutes. This is a company
// desk lock, not the JWT idle lifetime.
func NormalizeIdleLogout(minutes int) (int, error) {
	if minutes == 0 || (minutes >= MinIdleLogoutMinutes && minutes <= MaxIdleLogoutMinutes) {
		return minutes, nil
	}
	err := shared.NewValidation("invalid idle logout")
	err.Details = map[string]any{"idle_logout_minutes": "0 or 5-120"}
	return 0, err
}
