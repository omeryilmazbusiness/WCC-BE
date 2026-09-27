package totp

import (
	"bytes"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"
)

var rfcSecret = []byte("12345678901234567890")

// RFC 6238 Appendix B, SHA1 column.
var rfcVectors = []struct {
	unix int64
	code string
}{
	{59, "94287082"},
	{1111111109, "07081804"},
	{1111111111, "14050471"},
	{1234567890, "89005924"},
	{2000000000, "69279037"},
	{20000000000, "65353130"},
}

func TestRFC6238Vectors(t *testing.T) {
	for _, v := range rfcVectors {
		step := Step(time.Unix(v.unix, 0))
		if got := code(rfcSecret, step, 8); got != v.code {
			t.Fatalf("t=%d: got %s want %s", v.unix, got, v.code)
		}
		if got := Code(rfcSecret, step); got != v.code[2:] {
			t.Fatalf("t=%d 6-digit: got %s want %s", v.unix, got, v.code[2:])
		}
	}
}

func TestVerifySkewAndReplay(t *testing.T) {
	secret := b32.EncodeToString(rfcSecret)
	now := time.Unix(1111111111, 0)
	cur := Step(now)

	for _, d := range []int64{-1, 0, 1} {
		c := Code(rfcSecret, cur+d)
		step, err := Verify(secret, c, now, 0)
		if err != nil || step != cur+d {
			t.Fatalf("skew %d: step=%d err=%v", d, step, err)
		}
	}
	for _, d := range []int64{-2, 2} {
		if _, err := Verify(secret, Code(rfcSecret, cur+d), now, 0); !errors.Is(err, ErrInvalidCode) {
			t.Fatalf("skew %d should fail, got %v", d, err)
		}
	}

	c := Code(rfcSecret, cur)
	if _, err := Verify(secret, c, now, cur); !errors.Is(err, ErrReplay) {
		t.Fatalf("expected replay, got %v", err)
	}
	if _, err := Verify(secret, Code(rfcSecret, cur-1), now, cur); !errors.Is(err, ErrReplay) {
		t.Fatalf("older step must be replay, got %v", err)
	}
	if _, err := Verify(secret, "12345", now, 0); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("short code should fail, got %v", err)
	}
}

func TestGenerateSecretAndURL(t *testing.T) {
	s, err := GenerateSecret(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := DecodeSecret(strings.ToLower(s))
	if err != nil || len(raw) != SecretSize {
		t.Fatalf("decode: %v len=%d", err, len(raw))
	}
	if _, err := GenerateSecret(bytes.NewReader([]byte{1})); err == nil {
		t.Fatal("short reader must error")
	}
	u := URL("Wodi CRM", "gm@wodi.test", s)
	if !strings.HasPrefix(u, "otpauth://totp/Wodi%20CRM:gm@wodi.test?") || !strings.Contains(u, "secret="+s) {
		t.Fatalf("unexpected url %s", u)
	}
}

func TestLooksLikeCode(t *testing.T) {
	if !LooksLikeCode("123 456") || LooksLikeCode("abcd-efgh") || LooksLikeCode("1234567") {
		t.Fatal("LooksLikeCode mismatch")
	}
}
