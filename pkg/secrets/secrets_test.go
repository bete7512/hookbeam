package secrets_test

import (
	"crypto/rand"
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/pkg/secrets"
)

func newKey(t *testing.T) string {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(key)
}

func TestNew(t *testing.T) {
	_, err := secrets.New("not base64!")
	require.ErrorContains(t, err, "not base64")
	_, err = secrets.New(base64.StdEncoding.EncodeToString([]byte("short")))
	require.ErrorContains(t, err, "32 bytes")
	_, err = secrets.New(newKey(t))
	require.NoError(t, err)
}

func TestEncryptDecrypt(t *testing.T) {
	c, err := secrets.New(newKey(t))
	require.NoError(t, err)
	plaintext := []byte("whsec_example")

	a, err := c.Encrypt(plaintext)
	require.NoError(t, err)
	b, err := c.Encrypt(plaintext)
	require.NoError(t, err)
	require.NotEqual(t, a, b, "every encryption uses a fresh nonce")

	got, err := c.Decrypt(a)
	require.NoError(t, err)
	require.Equal(t, plaintext, got)

	a[len(a)-1] ^= 1
	_, err = c.Decrypt(a)
	require.Error(t, err, "tampered ciphertext")
	_, err = c.Decrypt([]byte("short"))
	require.Error(t, err)

	other, err := secrets.New(newKey(t))
	require.NoError(t, err)
	_, err = other.Decrypt(b)
	require.Error(t, err, "wrong key")
}
