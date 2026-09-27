package adminconfig

import (
	"testing"

	"github.com/google/uuid"
)

func TestAlertThresholdsNormalize(t *testing.T) {
	valid := DefaultAlertThresholds(uuid.New())
	if err := valid.Normalize(); err != nil {
		t.Fatalf("defaults rejected: %v", err)
	}
	cases := map[string]func(a *AlertThresholds){
		"zero hours":          func(a *AlertThresholds) { a.PaymentOverdueHours = 0 },
		"hours above cap":     func(a *AlertThresholds) { a.LeadNoFollowupHours = MaxThresholdHours + 1 },
		"capacity above 100":  func(a *AlertThresholds) { a.CapacitySoftPct = 101 },
		"warn not below B":    func(a *AlertThresholds) { a.SLAWarnPct, a.SLABreachPct = 100, 100 },
		"breach out of range": func(a *AlertThresholds) { a.SLABreachPct = 301 },
		"visa days above 90":  func(a *AlertThresholds) { a.VisaFollowUpDays = 91 },
	}
	for name, mutate := range cases {
		a := DefaultAlertThresholds(uuid.New())
		mutate(&a)
		if err := a.Normalize(); err == nil {
			t.Fatalf("%s: expected validation error", name)
		}
	}
	edge := DefaultAlertThresholds(uuid.New())
	edge.MissingDocHours = MaxThresholdHours
	edge.SLAWarnPct, edge.SLABreachPct = 10, 50
	if err := edge.Normalize(); err != nil {
		t.Fatalf("edge values rejected: %v", err)
	}
	legacy := DefaultAlertThresholds(uuid.New())
	legacy.SLAWarnPct, legacy.SLABreachPct, legacy.VisaFollowUpDays = 0, 0, 0
	if err := legacy.Normalize(); err != nil || legacy.SLAWarnPct != 75 || legacy.SLABreachPct != 100 || legacy.VisaFollowUpDays != 7 {
		t.Fatalf("legacy rows get defaults: %+v %v", legacy, err)
	}
}
