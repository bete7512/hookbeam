package auth_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/pkg/auth"
)

var testParams = auth.Argon2idParams{Memory: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}

func TestVerify(t *testing.T) {
	h := auth.NewArgon2id(testParams)
	hash, err := h.Hash("correct horse")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(hash, "$argon2id$v=19$m=8192,t=1,p=1$"), hash)

	tests := []struct {
		name     string
		password string
		hash     string
		want     bool
		wantErr  error
	}{
		{name: "correct password", password: "correct horse", hash: hash, want: true},
		{name: "wrong password", password: "wrong horse", hash: hash},
		{name: "empty hash", password: "x", hash: "", wantErr: auth.ErrInvalidHash},
		{name: "other algorithm", password: "x", hash: "$2b$10$abcdefghijklmnopqrstuv", wantErr: auth.ErrInvalidHash},
		{name: "unknown version", password: "x", hash: strings.Replace(hash, "v=19", "v=16", 1), wantErr: auth.ErrInvalidHash},
		{name: "zero iterations", password: "x", hash: strings.Replace(hash, "t=1", "t=0", 1), wantErr: auth.ErrInvalidHash},
		{name: "bad key encoding", password: "x", hash: hash + "!", wantErr: auth.ErrInvalidHash},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := h.Verify(tt.password, tt.hash)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHashIsSalted(t *testing.T) {
	h := auth.NewArgon2id(testParams)
	a, err := h.Hash("same")
	require.NoError(t, err)
	b, err := h.Hash("same")
	require.NoError(t, err)
	assert.NotEqual(t, a, b)
}

func TestVerifyUsesParamsFromHash(t *testing.T) {
	old, err := auth.NewArgon2id(testParams).Hash("correct horse")
	require.NoError(t, err)

	ok, err := auth.NewArgon2id(auth.DefaultArgon2idParams()).Verify("correct horse", old)
	require.NoError(t, err)
	assert.True(t, ok)
}
