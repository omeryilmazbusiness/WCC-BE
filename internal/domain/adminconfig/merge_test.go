package adminconfig

import (
	"testing"
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/notification"
)

func TestMergeEscalation(t *testing.T) {
	defs := notification.DefaultRules()
	overlays := []EscalationOverride{
		{Kind: notification.KindMessageSLA, EscalateAfterSeconds: 600, EscalateToRoles: []string{"gm"}, Enabled: true},
		{Kind: notification.KindTaskOverdue, Enabled: false},
	}
	merged := MergeEscalation(defs, overlays)
	var foundSLA, foundOverdue bool
	for _, r := range merged {
		if r.Kind == notification.KindMessageSLA {
			foundSLA = true
			if r.EscalateAfter != 10*time.Minute {
				t.Fatalf("sla after=%v", r.EscalateAfter)
			}
			if len(r.EscalateToRoles) != 1 || r.EscalateToRoles[0] != "gm" {
				t.Fatalf("roles %#v", r.EscalateToRoles)
			}
		}
		if r.Kind == notification.KindTaskOverdue {
			foundOverdue = true
		}
	}
	if !foundSLA {
		t.Fatal("sla missing")
	}
	if foundOverdue {
		t.Fatal("disabled overdue should be hidden")
	}
}
