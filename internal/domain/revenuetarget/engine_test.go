package revenuetarget_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	domain "github.com/wodi-crm/wodi-crm-be/internal/domain/revenuetarget"
)

func TestEngineLinearExpected(t *testing.T) {
	eng := domain.Engine{}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	asOf := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	res := eng.Calculate(domain.CalcInput{
		TargetAmount: 10000, Actual: 6000,
		PeriodStart: start, PeriodEnd: end, AsOf: asOf,
		Curve: domain.CurveLinear,
	})
	// days 1-5 of 10 = 50% → expected 5000
	if res.ExpectedToDate != 5000 {
		t.Fatalf("expected=%d", res.ExpectedToDate)
	}
	if res.Variance != 1000 {
		t.Fatalf("variance=%d", res.Variance)
	}
	if res.Status != domain.StatusAhead {
		t.Fatalf("status=%s", res.Status)
	}
}

func TestValidateWeightsSum(t *testing.T) {
	eng := domain.Engine{}
	err := eng.ValidateWeights(domain.CurveSeasonal, []domain.Weight{
		{Bucket: 0, WeightBps: 5000}, {Bucket: 1, WeightBps: 5000},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = eng.ValidateWeights(domain.CurveSeasonal, []domain.Weight{
		{Bucket: 0, WeightBps: 4000},
	})
	if err == nil {
		t.Fatal("expected sum error")
	}
}

func TestValidateShares(t *testing.T) {
	eng := domain.Engine{}
	u1 := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	u2 := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	err := eng.ValidateShares([]domain.Share{
		{UserID: u1, ShareBps: 5000}, {UserID: u2, ShareBps: 5000},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = eng.ValidateShares([]domain.Share{
		{UserID: u1, ShareBps: 4000},
	})
	if err == nil {
		t.Fatal("expected sum error")
	}
}
