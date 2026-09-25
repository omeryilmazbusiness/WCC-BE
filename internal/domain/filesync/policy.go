package filesync

import "strings"

// ResolveConflict applies ConflictPolicy without I/O (T-202).
// Platform DB remains authoritative unless policy explicitly prefers the file
// or flags for human review (source_of_truth=manual_review always flags).
func ResolveConflict(policy ConflictPolicy, truth SourceOfTruth, c FieldConflict) Resolution {
	plat := strings.TrimSpace(c.PlatformValue)
	file := strings.TrimSpace(c.FileValue)
	if plat == file {
		return Resolution{Chosen: plat, Flagged: false, Reason: "equal"}
	}
	if truth == TruthManualReview {
		return Resolution{Chosen: plat, Flagged: true, Reason: "manual_review"}
	}
	switch policy {
	case ConflictPreferFile:
		if truth == TruthPlatform {
			// Explicit policy overrides default truth for this field clash.
			return Resolution{Chosen: file, Flagged: false, Reason: "prefer_file"}
		}
		return Resolution{Chosen: file, Flagged: false, Reason: "prefer_file"}
	case ConflictFlag:
		chosen := plat
		if truth == TruthFile {
			chosen = file
		}
		return Resolution{Chosen: chosen, Flagged: true, Reason: "flag"}
	default: // prefer_platform
		return Resolution{Chosen: plat, Flagged: false, Reason: "prefer_platform"}
	}
}

// ApplySampleRows simulates applying pulled rows under policy.
// Returns applied count and conflict count (flagged resolutions).
func ApplySampleRows(
	policy ConflictPolicy,
	truth SourceOfTruth,
	platformByKey map[string]string,
	fileRows []map[string]string,
	keyField string,
) (applied, conflicts int, details []Resolution) {
	if keyField == "" {
		keyField = "id"
	}
	for _, row := range fileRows {
		key := strings.TrimSpace(row[keyField])
		if key == "" {
			continue
		}
		fileVal := strings.TrimSpace(row["value"])
		platVal := strings.TrimSpace(platformByKey[key])
		if platVal == "" {
			applied++
			details = append(details, Resolution{Chosen: fileVal, Reason: "insert"})
			continue
		}
		res := ResolveConflict(policy, truth, FieldConflict{
			Key: key, PlatformValue: platVal, FileValue: fileVal,
		})
		details = append(details, res)
		if res.Flagged {
			conflicts++
		} else {
			applied++
		}
	}
	return applied, conflicts, details
}
