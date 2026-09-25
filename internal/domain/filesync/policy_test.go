package filesync

import "testing"

func TestResolveConflictEqual(t *testing.T) {
	res := ResolveConflict(ConflictPreferPlatform, TruthPlatform, FieldConflict{
		Key: "1", PlatformValue: "same", FileValue: "same",
	})
	if res.Chosen != "same" || res.Flagged || res.Reason != "equal" {
		t.Fatalf("%+v", res)
	}
}

func TestResolveConflictPreferPlatform(t *testing.T) {
	res := ResolveConflict(ConflictPreferPlatform, TruthPlatform, FieldConflict{
		Key: "1", PlatformValue: "plat", FileValue: "file",
	})
	if res.Chosen != "plat" || res.Flagged || res.Reason != "prefer_platform" {
		t.Fatalf("%+v", res)
	}
}

func TestResolveConflictPreferFile(t *testing.T) {
	res := ResolveConflict(ConflictPreferFile, TruthPlatform, FieldConflict{
		Key: "1", PlatformValue: "plat", FileValue: "file",
	})
	if res.Chosen != "file" || res.Flagged || res.Reason != "prefer_file" {
		t.Fatalf("%+v", res)
	}
}

func TestResolveConflictFlag(t *testing.T) {
	res := ResolveConflict(ConflictFlag, TruthPlatform, FieldConflict{
		Key: "1", PlatformValue: "plat", FileValue: "file",
	})
	if res.Chosen != "plat" || !res.Flagged || res.Reason != "flag" {
		t.Fatalf("%+v", res)
	}
	res2 := ResolveConflict(ConflictFlag, TruthFile, FieldConflict{
		Key: "1", PlatformValue: "plat", FileValue: "file",
	})
	if res2.Chosen != "file" || !res2.Flagged {
		t.Fatalf("%+v", res2)
	}
}

func TestResolveConflictManualReview(t *testing.T) {
	res := ResolveConflict(ConflictPreferFile, TruthManualReview, FieldConflict{
		Key: "1", PlatformValue: "plat", FileValue: "file",
	})
	if res.Chosen != "plat" || !res.Flagged || res.Reason != "manual_review" {
		t.Fatalf("%+v", res)
	}
}

func TestApplySampleRowsInsertAndConflict(t *testing.T) {
	platform := map[string]string{
		"a": "plat-a",
		"b": "same",
	}
	rows := []map[string]string{
		{"id": "a", "value": "file-a"},
		{"id": "b", "value": "same"},
		{"id": "c", "value": "new"},
		{"id": "", "value": "skip"},
	}
	applied, conflicts, details := ApplySampleRows(ConflictFlag, TruthPlatform, platform, rows, "id")
	if applied != 2 { // equal + insert
		t.Fatalf("applied=%d details=%+v", applied, details)
	}
	if conflicts != 1 {
		t.Fatalf("conflicts=%d", conflicts)
	}
	if len(details) != 3 {
		t.Fatalf("details len=%d", len(details))
	}
}

func TestApplySampleRowsPreferFile(t *testing.T) {
	platform := map[string]string{"x": "old"}
	rows := []map[string]string{{"id": "x", "value": "new"}}
	applied, conflicts, _ := ApplySampleRows(ConflictPreferFile, TruthPlatform, platform, rows, "id")
	if applied != 1 || conflicts != 0 {
		t.Fatalf("applied=%d conflicts=%d", applied, conflicts)
	}
}
