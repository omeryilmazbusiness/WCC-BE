package auth

import (
	"fmt"
	"unicode"
)

// ValidatePassword enforces a production-lean password policy (T-011).
func ValidatePassword(password string) error {
	if len(password) < 10 {
		return fmt.Errorf("password must be at least 10 characters")
	}
	if len(password) > 128 {
		return fmt.Errorf("password is too long")
	}
	var hasLetter, hasDigit bool
	for _, r := range password {
		if unicode.IsLetter(r) {
			hasLetter = true
		}
		if unicode.IsDigit(r) {
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return fmt.Errorf("password must include letters and digits")
	}
	switch password {
	case "password", "password123", "1234567890", "ChangeMe123":
		return fmt.Errorf("password is too common")
	}
	return nil
}
