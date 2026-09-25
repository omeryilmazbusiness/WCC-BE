package adminconfig

import (
	"time"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/notification"
)

// MergeEscalation applies DB overlays onto code defaults (pure, OCP) — T-221.
func MergeEscalation(defaults []notification.Rule, overlays []EscalationOverride) []notification.Rule {
	byKind := map[string]EscalationOverride{}
	for _, o := range overlays {
		byKind[o.Kind] = o
	}
	out := make([]notification.Rule, 0, len(defaults))
	for _, d := range defaults {
		o, ok := byKind[d.Kind]
		if !ok {
			out = append(out, d)
			continue
		}
		if !o.Enabled {
			continue // disabled overlay hides the rule
		}
		cp := d
		cp.EscalateAfter = time.Duration(o.EscalateAfterSeconds) * time.Second
		if len(o.EscalateToRoles) > 0 {
			cp.EscalateToRoles = append([]string(nil), o.EscalateToRoles...)
		}
		out = append(out, cp)
	}
	return out
}
