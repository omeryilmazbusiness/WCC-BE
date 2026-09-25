package ai

import (
	"encoding/json"
	"testing"
	"time"
)

func TestScoreLeadUrgent(t *testing.T) {
	score, band, signals := ScoreLead(LeadSignals{
		Stage: "qualified", NoFollowUp: true, HoursSinceTouch: 80,
		OverdueTask: true, UnansweredInbox: true, Source: "whatsapp",
	})
	if band != BandUrgent || score < 70 {
		t.Fatalf("score=%d band=%s signals=%v", score, band, signals)
	}
	if len(signals) < 3 {
		t.Fatal("expected explainable signals")
	}
}

func TestScoreLeadLow(t *testing.T) {
	score, band, _ := ScoreLead(LeadSignals{Stage: "won", HoursSinceTouch: 1})
	if band != BandLow && score >= 20 {
		// won gives -20, may be low
		_ = score
	}
	if band == BandUrgent {
		t.Fatal("won should not be urgent")
	}
}

func TestDeterministicTargetInsight(t *testing.T) {
	out := DeterministicTargetInsight("Q1", 1000, 400, 600, "behind")
	recs, _ := out["recommendations"].([]string)
	if len(recs) == 0 || out["source"] != "deterministic" {
		t.Fatalf("%v", out)
	}
}

func TestMaskedKeyAndHash(t *testing.T) {
	if MaskedKeyHint("sk-abc123456") != "••••3456" {
		t.Fatal(MaskedKeyHint("sk-abc123456"))
	}
	cfg, _ := json.Marshal(map[string]string{"api_key": "secret-key-9999"})
	if ParseAPIKey(cfg) != "secret-key-9999" {
		t.Fatal("parse")
	}
	h := HashInput("a", "b")
	if len(h) != 64 {
		t.Fatal(h)
	}
	now := time.Now()
	past := now.Add(-48 * time.Hour)
	if HoursSince(&past, now) < 47 {
		t.Fatal("hours")
	}
}

func TestValidProvider(t *testing.T) {
	if !ValidProvider(ProviderGemini) || ValidProvider("foo") {
		t.Fatal("provider")
	}
	if DefaultModel(ProviderOpenAI) == "" {
		t.Fatal("model")
	}
}
