package company

import (
	"fmt"
	"strings"
)

// UpgradeMessage is the auto-written note the platform team sees.
type UpgradeMessage struct {
	Title       string
	Description string
	Page        string
	Locale      string
}

// ComposeUpgrade writes the request in the caller's locale. Feature is optional.
func ComposeUpgrade(locale, name, email, role, company string, from, to Plan, feature string) UpgradeMessage {
	loc := "en"
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(locale)), "ar") {
		loc = "ar"
	}
	from, to = Of(from).Plan, Of(to).Plan
	who := strings.TrimSpace(name)
	if who == "" {
		who = strings.TrimSpace(email)
	}
	if loc == "ar" {
		return UpgradeMessage{
			Title:       fmt.Sprintf("طلب ترقية: %s ← %s", from, to),
			Description: arabicUpgrade(who, email, role, company, from, to, feature),
			Page:        UpgradePage(to),
			Locale:      loc,
		}
	}
	return UpgradeMessage{
		Title:       fmt.Sprintf("Upgrade request: %s → %s", from, to),
		Description: englishUpgrade(who, email, role, company, from, to, feature),
		Page:        UpgradePage(to),
		Locale:      loc,
	}
}

func englishUpgrade(name, email, role, company string, from, to Plan, feature string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s, %s) at %s requested an upgrade from %s to %s.",
		name, strings.TrimSpace(email), strings.TrimSpace(role), strings.TrimSpace(company), from, to)
	if f := strings.TrimSpace(feature); f != "" {
		fmt.Fprintf(&b, "\nLocked menu: %s.", f)
	}
	return b.String()
}

func arabicUpgrade(name, email, role, company string, from, to Plan, feature string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s، %s) في %s يطلب الترقية من %s إلى %s.",
		name, strings.TrimSpace(email), strings.TrimSpace(role), strings.TrimSpace(company), from, to)
	if f := strings.TrimSpace(feature); f != "" {
		fmt.Fprintf(&b, "\nالقائمة المقفلة: %s.", f)
	}
	return b.String()
}
