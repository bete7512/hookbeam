package auth_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/scaffold/pkg/auth"
)

var seed = bytes.Repeat([]byte{1}, 32)

func newTokens(t *testing.T, seed []byte, issuer string, ttl time.Duration) *auth.Tokens {
	t.Helper()
	tokens, err := auth.NewTokens(seed, issuer, ttl)
	require.NoError(t, err)
	return tokens
}

func issue(t *testing.T, tokens *auth.Tokens) string {
	t.Helper()
	token, _, err := tokens.Issue(42, 3)
	require.NoError(t, err)
	return token
}

func TestParse(t *testing.T) {
	tokens := newTokens(t, seed, "scaffold", time.Minute)
	valid := issue(t, tokens)
	hs256, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject: "42", Issuer: "scaffold", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)),
	}).SignedString([]byte("secret"))
	require.NoError(t, err)

	tests := []struct {
		name    string
		token   string
		wantErr bool
	}{
		{name: "valid", token: valid},
		{name: "expired", token: issue(t, newTokens(t, seed, "scaffold", -time.Minute)), wantErr: true},
		{name: "signed by another key", token: issue(t, newTokens(t, bytes.Repeat([]byte{2}, 32), "scaffold", time.Minute)), wantErr: true},
		{name: "wrong issuer", token: issue(t, newTokens(t, seed, "other", time.Minute)), wantErr: true},
		{name: "other algorithm", token: hs256, wantErr: true},
		{name: "tampered signature", token: valid[:len(valid)-4] + "AAAA", wantErr: true},
		{name: "garbage", token: "not-a-token", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims, err := tokens.Parse(tt.token)
			if tt.wantErr {
				require.ErrorIs(t, err, auth.ErrInvalidToken)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, int64(42), claims.UserID)
			assert.Equal(t, int32(3), claims.TokenVersion)
		})
	}
}

func TestNewTokensRejectsShortSeed(t *testing.T) {
	_, err := auth.NewTokens([]byte("short"), "scaffold", time.Minute)
	require.Error(t, err)
}

func TestRefreshToken(t *testing.T) {
	a, hash, err := auth.NewToken()
	require.NoError(t, err)
	b, _, err := auth.NewToken()
	require.NoError(t, err)

	assert.NotEqual(t, a, b)
	assert.Equal(t, hash, auth.HashToken(a))
	assert.Len(t, hash, 32)
}

func TestClaimsContext(t *testing.T) {
	_, ok := auth.ClaimsFrom(context.Background())
	assert.False(t, ok)

	ctx := auth.WithClaims(context.Background(), auth.Claims{UserID: 7})
	claims, ok := auth.ClaimsFrom(ctx)
	require.True(t, ok)
	assert.Equal(t, int64(7), claims.UserID)
}
