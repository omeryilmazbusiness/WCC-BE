package signature

import (
	"errors"
	"strings"
	"testing"
)

func TestMetaSignature(t *testing.T) {
	body := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"1"}]}`)
	secret := "app-secret"
	v := Meta()

	valid := map[string]string{MetaHeader: SignHeader(body, secret)}
	if err := v.Verify(valid, body, secret); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	upper := map[string]string{MetaHeader: "SHA256=" + strings.ToUpper(strings.TrimPrefix(SignHeader(body, secret), "sha256="))}
	if err := v.Verify(upper, body, secret); err != nil {
		t.Fatalf("case-insensitive hex rejected: %v", err)
	}

	tampered := append([]byte{}, body...)
	tampered[10] ^= 1
	if err := v.Verify(valid, tampered, secret); !errors.Is(err, ErrInvalid) {
		t.Fatalf("tampered body accepted: %v", err)
	}
	if err := v.Verify(valid, body, "other-secret"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("wrong secret accepted: %v", err)
	}
	if err := v.Verify(map[string]string{}, body, secret); !errors.Is(err, ErrMissing) {
		t.Fatalf("missing header: %v", err)
	}
	if err := v.Verify(map[string]string{MetaHeader: "sha1=abcd"}, body, secret); !errors.Is(err, ErrInvalid) {
		t.Fatalf("wrong prefix: %v", err)
	}
	if err := v.Verify(map[string]string{MetaHeader: "sha256=zz"}, body, secret); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad hex: %v", err)
	}
	if err := v.Verify(valid, body, ""); !errors.Is(err, ErrNoSecret) {
		t.Fatalf("empty secret: %v", err)
	}
}

func TestSharedSignature(t *testing.T) {
	body := []byte(`{"to":"inbox@wodi.test","event_id":"e1"}`)
	v := Shared()
	h := map[string]string{WodiHeader: SignHeader(body, "s3cret")}
	if err := v.Verify(h, body, "s3cret"); err != nil {
		t.Fatalf("valid rejected: %v", err)
	}
	if err := v.Verify(map[string]string{MetaHeader: h[WodiHeader]}, body, "s3cret"); !errors.Is(err, ErrMissing) {
		t.Fatalf("meta header must not satisfy shared scheme: %v", err)
	}
}
