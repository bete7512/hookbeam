// Package webhooks signs and verifies webhook requests with the Standard Webhooks HMAC-SHA256 scheme.
package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Header names every signed request carries.
const (
	HeaderID        = "webhook-id"
	HeaderTimestamp = "webhook-timestamp"
	HeaderSignature = "webhook-signature"
	// SecretPrefix starts every signing secret; the rest is the base64 key.
	SecretPrefix = "whsec_"
	version      = "v1"
)

// Verification errors.
var (
	ErrMissingHeaders = errors.New("webhooks: missing signature headers")
	ErrBadTimestamp   = errors.New("webhooks: timestamp is not a unix time")
	ErrTimestampSkew  = errors.New("webhooks: timestamp outside tolerance")
	ErrNoMatch        = errors.New("webhooks: no signature matched")
)

// Key returns the raw HMAC key of a whsec_ secret.
func Key(secret string) ([]byte, error) {
	encoded, ok := strings.CutPrefix(secret, SecretPrefix)
	if !ok {
		return nil, errors.New("webhooks: secret must start with " + SecretPrefix)
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("webhooks: secret is not base64: %w", err)
	}
	return key, nil
}

// Sign returns the "v1,<base64>" signature of id, timestamp and body under the secret.
func Sign(secret, id string, timestamp time.Time, body []byte) (string, error) {
	key, err := Key(secret)
	if err != nil {
		return "", err
	}
	return version + "," + base64.StdEncoding.EncodeToString(mac(key, id, timestamp.Unix(), body)), nil
}

// Verify checks the headers against body with the secrets (several during rotation), within tolerance of now.
func Verify(secrets []string, headers http.Header, body []byte, now time.Time, tolerance time.Duration) error {
	id, ts, sigs := headers.Get(HeaderID), headers.Get(HeaderTimestamp), headers.Get(HeaderSignature)
	if id == "" || ts == "" || sigs == "" {
		return ErrMissingHeaders
	}
	unix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return ErrBadTimestamp
	}
	if d := now.Unix() - unix; d > int64(tolerance.Seconds()) || -d > int64(tolerance.Seconds()) {
		return ErrTimestampSkew
	}
	for _, secret := range secrets {
		key, err := Key(secret)
		if err != nil {
			return err
		}
		want := mac(key, id, unix, body)
		for sig := range strings.FieldsSeq(sigs) {
			encoded, ok := strings.CutPrefix(sig, version+",")
			if !ok {
				continue
			}
			got, err := base64.StdEncoding.DecodeString(encoded)
			if err == nil && hmac.Equal(got, want) {
				return nil
			}
		}
	}
	return ErrNoMatch
}

func mac(key []byte, id string, unix int64, body []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(id))
	h.Write([]byte("."))
	h.Write([]byte(strconv.FormatInt(unix, 10)))
	h.Write([]byte("."))
	h.Write(body)
	return h.Sum(nil)
}
