package support

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

var now = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func draft(title, desc string) Draft { return Draft{Title: title, Description: desc} }

func TestNewRequestValidates(t *testing.T) {
	u, b := uuid.New(), uuid.New()
	r, err := NewRequest(Draft{Title: "  Cannot   export\treport ", Description: "  The export button\r\n does nothing.\x00 ", Page: "/en/reports", Locale: "ar-SA"}, u, b, now)
	if err != nil {
		t.Fatal(err)
	}
	if r.Title != "Cannot export report" || r.Description != "The export button\n does nothing." {
		t.Fatalf("cleaned: %q / %q", r.Title, r.Description)
	}
	if r.Status != StatusOpen || r.Page != "/en/reports" || r.Locale != "ar" || r.RequesterID != u || r.BranchID != b {
		t.Fatalf("%+v", r)
	}
	for name, d := range map[string]Draft{
		"short title":      draft("abc", "long enough description"),
		"long title":       draft(strings.Repeat("x", TitleMax+1), "long enough description"),
		"short desc":       draft("Valid title", "too short"),
		"long desc":        draft("Valid title", strings.Repeat("x", DescriptionMax+1)),
		"blank after trim": draft("    \t ", "long enough description"),
	} {
		if _, err := NewRequest(d, u, b, now); !errors.Is(err, shared.ErrValidation) {
			t.Fatalf("%s: want validation error, got %v", name, err)
		}
	}
	if _, err := NewRequest(draft("Valid title", "long enough description"), uuid.Nil, b, now); err == nil {
		t.Fatal("requester required")
	}
}

func TestPageIsAnInAppPathOnly(t *testing.T) {
	for in, want := range map[string]string{
		"/en/admin/settings/help":      "/en/admin/settings/help",
		"https://evil.example":         "",
		"//evil.example":               "",
		"/en/x?<script>":               "",
		"":                             "",
		"/" + strings.Repeat("a", 250): "",
	} {
		if got := cleanPage(in); got != want {
			t.Fatalf("cleanPage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTransition(t *testing.T) {
	r, _ := NewRequest(draft("Valid title", "long enough description"), uuid.New(), uuid.New(), now)
	admin := uuid.New()
	note := "  Looking into it  "
	if err := r.Transition(StatusInProgress, &note, admin, now); err != nil || r.AdminNote != "Looking into it" || r.ResolvedAt != nil {
		t.Fatalf("%v %+v", err, r)
	}
	later := now.Add(time.Hour)
	if err := r.Transition(StatusResolved, nil, admin, later); err != nil || r.ResolvedAt == nil || !r.ResolvedAt.Equal(later) || *r.ResolvedBy != admin {
		t.Fatalf("resolve: %v %+v", err, r)
	}
	if r.AdminNote != "Looking into it" {
		t.Fatal("nil note keeps the previous note")
	}
	if err := r.Transition(StatusOpen, nil, admin, later); err != nil || r.ResolvedAt != nil || r.ResolvedBy != nil {
		t.Fatal("reopening clears the resolution")
	}
	if err := r.Transition("closed", nil, admin, now); !errors.Is(err, shared.ErrValidation) {
		t.Fatal("unknown status rejected")
	}
	long := strings.Repeat("n", NoteMax+1)
	if err := r.Transition(StatusOpen, &long, admin, now); !errors.Is(err, shared.ErrValidation) {
		t.Fatal("note capped")
	}
}
