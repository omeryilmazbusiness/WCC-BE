package app

import (
	"testing"

	domainassistant "github.com/wodi-crm/wodi-crm-be/internal/domain/assistant"
	platformauth "github.com/wodi-crm/wodi-crm-be/internal/platform/auth"
)

// The assistant domain names permissions as strings; each must be granted by some role.
func TestAssistantPermissionsExist(t *testing.T) {
	known := map[platformauth.Permission]bool{}
	for _, role := range platformauth.AllRoles() {
		for _, p := range platformauth.PermissionsFor(role) {
			known[p] = true
		}
	}
	for _, c := range domainassistant.Capabilities() {
		for _, p := range c.Requires {
			if !known[platformauth.Permission(p)] {
				t.Errorf("%s requires unknown permission %q", c.ID, p)
			}
		}
	}
}
