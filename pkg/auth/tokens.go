package auth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// ErrInvalidToken means an access token is malformed, expired, or not signed by us.
var ErrInvalidToken = errors.New("auth: invalid token")

// Claims identify the user an access token was issued to.
type Claims struct {
	UserID       int64
	TokenVersion int32
	ExpiresAt    time.Time
}

// Tokens issues and verifies EdDSA-signed access tokens.
type Tokens struct {
	key    ed25519.PrivateKey
	issuer string
	ttl    time.Duration
}

// NewTokens returns Tokens that sign with the ed25519 key derived from a 32-byte seed.
func NewTokens(seed []byte, issuer string, ttl time.Duration) (*Tokens, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("auth: signing key seed must be %d bytes, got %d", ed25519.SeedSize, len(seed))
	}
	return &Tokens{key: ed25519.NewKeyFromSeed(seed), issuer: issuer, ttl: ttl}, nil
}

type accessClaims struct {
	TokenVersion int32 `json:"tv"`
	jwt.RegisteredClaims
}

// Issue returns a signed access token for the user and when it expires.
func (t *Tokens) Issue(userID int64, tokenVersion int32) (string, time.Time, error) {
	now := time.Now()
	expiresAt := now.Add(t.ttl)
	claims := accessClaims{
		TokenVersion: tokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(userID, 10),
			Issuer:    t.issuer,
			ID:        uuid.NewString(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims).SignedString(t.key)
	return signed, expiresAt, err
}

// Parse verifies an access token and returns its claims, or an error wrapping ErrInvalidToken.
func (t *Tokens) Parse(token string) (Claims, error) {
	var claims accessClaims
	_, err := jwt.ParseWithClaims(token, &claims,
		func(*jwt.Token) (any, error) { return t.key.Public(), nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithIssuer(t.issuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: %w", ErrInvalidToken, err)
	}
	userID, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: subject: %w", ErrInvalidToken, err)
	}
	return Claims{UserID: userID, TokenVersion: claims.TokenVersion, ExpiresAt: claims.ExpiresAt.Time}, nil
}

// NewToken returns a random opaque token (refresh, verification, reset) and the hash to store for it.
func NewToken() (string, []byte, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	return token, HashToken(token), nil
}

// HashToken returns the SHA-256 hash an opaque token is stored under.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

type claimsKey struct{}

// WithClaims stores the authenticated user's claims in ctx.
func WithClaims(ctx context.Context, c Claims) context.Context {
	return context.WithValue(ctx, claimsKey{}, c)
}

// ClaimsFrom returns the claims stored by WithClaims.
func ClaimsFrom(ctx context.Context) (Claims, bool) {
	c, ok := ctx.Value(claimsKey{}).(Claims)
	return c, ok
}
