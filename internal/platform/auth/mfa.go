package auth

// MFAPolicy decides which roles must use a second factor (T-251).
type MFAPolicy struct {
	forced map[Role]bool
}

// NewMFAPolicy forces MFA for the given roles; with none, MFA is opt-in.
func NewMFAPolicy(forced ...Role) MFAPolicy {
	m := make(map[Role]bool, len(forced))
	for _, r := range forced {
		m[r] = true
	}
	return MFAPolicy{forced: m}
}

// Required reports whether role must enroll in MFA before getting tokens.
func (p MFAPolicy) Required(role Role) bool {
	return p.forced[role]
}
