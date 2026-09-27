package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// SigningKey is one JWT key identified by kid. Adding an algorithm (e.g.
// EdDSA) means adding an implementation; signers and callers stay unchanged.
type SigningKey interface {
	ID() string
	Method() jwt.SigningMethod
	SignKey() any
	VerifyKey() any
}

type hmacKey struct {
	id     string
	secret []byte
}

// NewHS256Key returns an HMAC-SHA256 key. Minimum secret length is a
// deployment policy enforced by config validation.
func NewHS256Key(id, secret string) (SigningKey, error) {
	if id == "" || secret == "" {
		return nil, errors.New("hs256 key: id and secret are required")
	}
	return hmacKey{id: id, secret: []byte(secret)}, nil
}

func (k hmacKey) ID() string                { return k.id }
func (k hmacKey) Method() jwt.SigningMethod { return jwt.SigningMethodHS256 }
func (k hmacKey) SignKey() any              { return k.secret }
func (k hmacKey) VerifyKey() any            { return k.secret }

// TokenSigner signs access-token claims with the active key and verifies
// tokens signed by any known key.
type TokenSigner interface {
	Sign(c *Claims) (string, error)
	Verify(token string) (*Claims, error)
}

// Validation is the registered-claim policy applied on Verify.
type Validation struct {
	Issuer   string
	Audience string
	Leeway   time.Duration
	// Now overrides the clock (tests).
	Now func() time.Time
}

// DefaultLeeway tolerates clock skew between instances.
const DefaultLeeway = 30 * time.Second

// JWTSigner is a kid-addressed keyring: new tokens use the active key, and
// retired keys keep verifying until their tokens have expired.
type JWTSigner struct {
	active SigningKey
	keys   map[string]SigningKey
	parser *jwt.Parser
}

func NewJWTSigner(active SigningKey, previous []SigningKey, v Validation) (*JWTSigner, error) {
	if active == nil {
		return nil, errors.New("jwt signer: active key is required")
	}
	if v.Issuer == "" || v.Audience == "" {
		return nil, errors.New("jwt signer: issuer and audience are required")
	}
	keys := map[string]SigningKey{active.ID(): active}
	algs := map[string]struct{}{active.Method().Alg(): {}}
	for _, k := range previous {
		if _, dup := keys[k.ID()]; dup {
			return nil, fmt.Errorf("jwt signer: duplicate kid %q", k.ID())
		}
		keys[k.ID()] = k
		algs[k.Method().Alg()] = struct{}{}
	}
	methods := make([]string, 0, len(algs))
	for a := range algs {
		methods = append(methods, a)
	}
	opts := []jwt.ParserOption{
		jwt.WithValidMethods(methods),
		jwt.WithIssuer(v.Issuer),
		jwt.WithAudience(v.Audience),
		jwt.WithLeeway(v.Leeway),
		jwt.WithIssuedAt(),
		jwt.WithExpirationRequired(),
	}
	if v.Now != nil {
		opts = append(opts, jwt.WithTimeFunc(v.Now))
	}
	return &JWTSigner{active: active, keys: keys, parser: jwt.NewParser(opts...)}, nil
}

func (s *JWTSigner) Sign(c *Claims) (string, error) {
	t := jwt.NewWithClaims(s.active.Method(), c)
	t.Header["kid"] = s.active.ID()
	return t.SignedString(s.active.SignKey())
}

func (s *JWTSigner) Verify(token string) (*Claims, error) {
	claims := &Claims{}
	parsed, err := s.parser.ParseWithClaims(token, claims, s.keyFor)
	if err != nil {
		return nil, err
	}
	if !parsed.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

func (s *JWTSigner) keyFor(t *jwt.Token) (any, error) {
	kid, _ := t.Header["kid"].(string)
	k, ok := s.keys[kid]
	if !ok {
		return nil, fmt.Errorf("unknown kid %q", kid)
	}
	if t.Method.Alg() != k.Method().Alg() {
		return nil, fmt.Errorf("alg %q does not match key %q", t.Method.Alg(), kid)
	}
	return k.VerifyKey(), nil
}

var _ TokenSigner = (*JWTSigner)(nil)
