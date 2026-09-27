package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	testSecretA1 = "a1-secret-a1-secret-a1-secret-a1-secret"
	testSecretA2 = "a2-secret-a2-secret-a2-secret-a2-secret"
)

var testValidation = Validation{Issuer: "wodi-test", Audience: "wodi-crm-api", Leeway: DefaultLeeway}

func mustKey(t *testing.T, id, secret string) SigningKey {
	t.Helper()
	k, err := NewHS256Key(id, secret)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func mustSigner(t *testing.T, active SigningKey, previous ...SigningKey) *JWTSigner {
	t.Helper()
	s, err := NewJWTSigner(active, previous, testValidation)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func validClaims(now time.Time) *Claims {
	uid := uuid.New()
	return &Claims{
		UserID: uid, Role: RoleEmployee, SessionID: uuid.New(), TokenVersion: 1,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: testValidation.Issuer, Subject: uid.String(), Audience: jwt.ClaimStrings{testValidation.Audience},
			IssuedAt: jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
		},
	}
}

func signRaw(t *testing.T, method jwt.SigningMethod, kid string, key any, c *Claims) string {
	t.Helper()
	tok := jwt.NewWithClaims(method, c)
	if kid != "" {
		tok.Header["kid"] = kid
	}
	s, err := tok.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSignerRoundTripSetsKid(t *testing.T) {
	s := mustSigner(t, mustKey(t, "a1", testSecretA1))
	c := validClaims(time.Now())
	tok, err := s.Sign(c)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _, err := jwt.NewParser().ParseUnverified(tok, &Claims{})
	if err != nil || parsed.Header["kid"] != "a1" || parsed.Header["alg"] != "HS256" {
		t.Fatalf("header: %v %v", parsed.Header, err)
	}
	got, err := s.Verify(tok)
	if err != nil {
		t.Fatal(err)
	}
	if got.SessionID != c.SessionID || got.TokenVersion != 1 || got.UserID != c.UserID {
		t.Fatalf("claims lost: %+v", got)
	}
}

func TestSignerKeyRotation(t *testing.T) {
	a1, a2 := mustKey(t, "a1", testSecretA1), mustKey(t, "a2", testSecretA2)
	old := mustSigner(t, a1)
	tok, _ := old.Sign(validClaims(time.Now()))

	rotated := mustSigner(t, a2, a1)
	if _, err := rotated.Verify(tok); err != nil {
		t.Fatalf("previous key must still verify: %v", err)
	}
	fresh, _ := rotated.Sign(validClaims(time.Now()))
	if _, err := old.Verify(fresh); err == nil {
		t.Fatal("a signer without a2 must reject a2 tokens")
	}
	retired := mustSigner(t, a2)
	if _, err := retired.Verify(tok); err == nil || !strings.Contains(err.Error(), "unknown kid") {
		t.Fatalf("retired kid must be rejected: %v", err)
	}
	if _, err := NewJWTSigner(a1, []SigningKey{mustKey(t, "a1", testSecretA2)}, testValidation); err == nil {
		t.Fatal("duplicate kid must be rejected")
	}
}

func TestSignerRejects(t *testing.T) {
	now := time.Now()
	s := mustSigner(t, mustKey(t, "a1", testSecretA1))
	secret := []byte(testSecretA1)
	mut := func(f func(*Claims)) *Claims { c := validClaims(now); f(c); return c }

	cases := map[string]string{
		"wrong audience": signRaw(t, jwt.SigningMethodHS256, "a1", secret, mut(func(c *Claims) { c.Audience = jwt.ClaimStrings{"other-api"} })),
		"no audience":    signRaw(t, jwt.SigningMethodHS256, "a1", secret, mut(func(c *Claims) { c.Audience = nil })),
		"wrong issuer":   signRaw(t, jwt.SigningMethodHS256, "a1", secret, mut(func(c *Claims) { c.Issuer = "evil" })),
		"expired":        signRaw(t, jwt.SigningMethodHS256, "a1", secret, mut(func(c *Claims) { c.ExpiresAt = jwt.NewNumericDate(now.Add(-time.Minute)) })),
		"no expiry":      signRaw(t, jwt.SigningMethodHS256, "a1", secret, mut(func(c *Claims) { c.ExpiresAt = nil })),
		"not yet valid":  signRaw(t, jwt.SigningMethodHS256, "a1", secret, mut(func(c *Claims) { c.NotBefore = jwt.NewNumericDate(now.Add(time.Minute)) })),
		"issued in future": signRaw(t, jwt.SigningMethodHS256, "a1", secret, mut(func(c *Claims) {
			c.IssuedAt = jwt.NewNumericDate(now.Add(time.Minute))
		})),
		"missing kid":   signRaw(t, jwt.SigningMethodHS256, "", secret, validClaims(now)),
		"unknown kid":   signRaw(t, jwt.SigningMethodHS256, "zz", secret, validClaims(now)),
		"wrong secret":  signRaw(t, jwt.SigningMethodHS256, "a1", []byte(testSecretA2), validClaims(now)),
		"alg none":      signRaw(t, jwt.SigningMethodNone, "a1", jwt.UnsafeAllowNoneSignatureType, validClaims(now)),
		"other hmac":    signRaw(t, jwt.SigningMethodHS512, "a1", secret, validClaims(now)),
		"garbage token": "not.a.jwt",
	}
	for name, tok := range cases {
		if _, err := s.Verify(tok); err == nil {
			t.Errorf("%s: must be rejected", name)
		}
	}
}

func TestSignerLeeway(t *testing.T) {
	now := time.Now()
	s := mustSigner(t, mustKey(t, "a1", testSecretA1))
	c := validClaims(now)
	c.ExpiresAt = jwt.NewNumericDate(now.Add(-10 * time.Second))
	c.NotBefore = jwt.NewNumericDate(now.Add(10 * time.Second))
	if _, err := s.Verify(signRaw(t, jwt.SigningMethodHS256, "a1", []byte(testSecretA1), c)); err != nil {
		t.Fatalf("skew inside 30s leeway must be tolerated: %v", err)
	}
}

func TestTokenServiceIssuesSessionBoundTokens(t *testing.T) {
	s := mustSigner(t, mustKey(t, "a1", testSecretA1))
	svc := NewTokenService(s, 15*time.Minute, testValidation.Issuer, testValidation.Audience)
	sub := AccessSubject{UserID: uuid.New(), Role: RoleGM, BranchID: uuid.New(), SessionID: uuid.New(), TokenVersion: 7}
	at, err := svc.IssueAccess(sub)
	if err != nil || at.ExpiresIn != 900 {
		t.Fatalf("issue: %v %d", err, at.ExpiresIn)
	}
	c, err := svc.ParseAccess(at.Token)
	if err != nil {
		t.Fatal(err)
	}
	if c.SessionID != sub.SessionID || c.TokenVersion != 7 || c.Subject != sub.UserID.String() || c.NotBefore == nil {
		t.Fatalf("claims: %+v", c)
	}

	noSID := validClaims(time.Now())
	noSID.SessionID = uuid.Nil
	tok, _ := s.Sign(noSID)
	if _, err := svc.ParseAccess(tok); err == nil {
		t.Fatal("token without sid must be rejected")
	}
}

func TestParseHS256Keys(t *testing.T) {
	keys, err := ParseHS256Keys([]string{"a0:" + testSecretA1, " old : sec:with:colons"})
	if err != nil || len(keys) != 2 || keys[0].ID() != "a0" || keys[1].ID() != "old" {
		t.Fatalf("parse: %v %v", keys, err)
	}
	if _, err := ParseHS256Keys([]string{"nokid"}); err == nil {
		t.Fatal("missing separator must fail")
	}
}
