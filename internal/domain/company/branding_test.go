package company

import (
	"bytes"
	"errors"
	"testing"

	"github.com/wodi-crm/wodi-crm-be/internal/domain/shared"
)

func TestNewLogo(t *testing.T) {
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	jpeg := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, make([]byte, 64)...)
	webp := append([]byte("RIFF\x00\x00\x00\x00WEBPVP8 "), make([]byte, 64)...)

	for name, data := range map[string][]byte{"png": png, "jpeg": jpeg, "webp": webp} {
		logo, err := NewLogo(data)
		if err != nil || logo.ContentType != "image/"+name {
			t.Errorf("%s: %+v %v", name, logo.ContentType, err)
		}
	}

	for name, data := range map[string][]byte{
		"empty":    nil,
		"svg":      []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
		"html":     []byte("<!doctype html><html></html>"),
		"gif":      []byte("GIF89a......"),
		"too big":  append(png, bytes.Repeat([]byte{0}, MaxLogoBytes)...),
		"pdf":      []byte("%PDF-1.7"),
		"text/png": []byte("not really a png"),
	} {
		if _, err := NewLogo(data); !errors.Is(err, shared.ErrValidation) {
			t.Errorf("%s: want validation error, got %v", name, err)
		}
	}
}

func TestBranchSlugCannotShadowLogin(t *testing.T) {
	b := Branch{NameEN: "Login", Code: "LOGIN", Slug: "login"}
	if err := b.Normalize(); !errors.Is(err, shared.ErrValidation) {
		t.Fatalf("branch slug 'login' must be rejected: %v", err)
	}
}
