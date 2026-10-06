package identity

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

func reasonOf(t *testing.T, err error) string {
	t.Helper()
	var app *shared.AppError
	if !errors.As(err, &app) || !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("want a validation error, got %v", err)
	}
	for k, v := range app.Details {
		return k + "=" + v.(string)
	}
	return ""
}

func TestNormalizeFullName(t *testing.T) {
	got, err := NormalizeFullName("  Ömer   Yılmaz ")
	if err != nil || got != "Ömer Yılmaz" {
		t.Fatalf("got %q %v", got, err)
	}
	for raw, want := range map[string]string{
		" ":                      "full_name=required",
		"A":                      "full_name=required",
		strings.Repeat("ب", 121): "full_name=too_long",
		"Bad\u0000Name":          "full_name=invalid",
		"Line\nBreak":            "full_name=invalid",
	} {
		if _, err := NormalizeFullName(raw); reasonOf(t, err) != want {
			t.Errorf("%q: want %s", raw, want)
		}
	}
	if got, err := NormalizeFullName(strings.Repeat("ب", 120)); err != nil || len([]rune(got)) != 120 {
		t.Fatal("120 runes is the limit, counted in runes")
	}
}

func TestNormalizeJobTitle(t *testing.T) {
	if got, err := NormalizeJobTitle("  General   Manager "); err != nil || got != "General Manager" {
		t.Fatalf("got %q %v", got, err)
	}
	if got, err := NormalizeJobTitle("   "); err != nil || got != "" {
		t.Fatal("blank clears the title")
	}
	if _, err := NormalizeJobTitle(strings.Repeat("x", 81)); reasonOf(t, err) != "job_title=too_long" {
		t.Fatal("81 runes is too long")
	}
}

func TestNormalizePhone(t *testing.T) {
	for raw, want := range map[string]string{
		"+90 532 123 45 67":   "+90 532 123 45 67",
		" +966 (11) 234-5678": "+966 (11) 234-5678",
		"0532.123.45.67":      "0532.123.45.67",
		"":                    "",
		"   ":                 "",
	} {
		if got, err := NormalizePhone(raw); err != nil || got != want {
			t.Errorf("%q: got %q %v", raw, got, err)
		}
	}
	for _, raw := range []string{"12345", "+1234567890123456", "90+532 123 4567", "0532 abc 4567", "+90 532 123 45 67 ext 9"} {
		if _, err := NormalizePhone(raw); reasonOf(t, err) != "phone=invalid" {
			t.Errorf("%q must be invalid", raw)
		}
	}
}

func TestNormalizeEmail(t *testing.T) {
	if got, err := NormalizeEmail("  GM@Wodi.Example "); err != nil || got != "gm@wodi.example" {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := NormalizeEmail(""); reasonOf(t, err) != "email=required" {
		t.Fatal("empty email is required")
	}
	for _, raw := range []string{"gm", "gm@", "gm@localhost", "Omer <gm@wodi.example>", "a b@wodi.example", strings.Repeat("a", 250) + "@x.io"} {
		if _, err := NormalizeEmail(raw); reasonOf(t, err) != "email=invalid" {
			t.Errorf("%q must be invalid", raw)
		}
	}
}

func TestNewAvatar(t *testing.T) {
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	a, err := NewAvatar(png)
	if err != nil || a.ContentType != "image/png" {
		t.Fatalf("png: %v %q", err, a.ContentType)
	}
	if _, err := NewAvatar(nil); reasonOf(t, err) != "avatar=required" {
		t.Fatal("empty upload")
	}
	if _, err := NewAvatar([]byte(`<svg xmlns="http://www.w3.org/2000/svg"><script/></svg>`)); reasonOf(t, err) != "avatar=type" {
		t.Fatal("svg must be refused")
	}
	big := append(png, bytes.Repeat([]byte{0}, MaxAvatarBytes)...)
	if _, err := NewAvatar(big); reasonOf(t, err) != "avatar=too_large" {
		t.Fatal("over 1 MB must be refused")
	}
}
