//go:build security

package webhooks_test

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/pkg/webhooks"
)

// TestSecuritySignatureTampering signs one delivery and then alters each thing a forger could alter: the body,
// the id, the timestamp, the secret, the signature bytes, the headers themselves. Every alteration is refused;
// the untouched request and rotation (two secrets, two signatures) are accepted.
func TestSecuritySignatureTampering(t *testing.T) {
	const secret = "whsec_c2VjcmV0IGZvciB0aGUgc2lnbmF0dXJlIHRlc3Rz"
	const other = "whsec_YW5vdGhlciBzZWNyZXQgZm9yIHRoZSB0ZXN0cw=="
	now := time.Now()
	body := []byte(`{"type":"order.shipped","data":{"order_id":"817"}}`)
	sig, err := webhooks.Sign(secret, "msg_1", now, body)
	require.NoError(t, err)
	headers := func(id string, ts time.Time, signature string) http.Header {
		h := http.Header{}
		h.Set(webhooks.HeaderID, id)
		h.Set(webhooks.HeaderTimestamp, strconv.FormatInt(ts.Unix(), 10))
		h.Set(webhooks.HeaderSignature, signature)
		return h
	}
	tolerance := 5 * time.Minute
	require.NoError(t, webhooks.Verify([]string{secret}, headers("msg_1", now, sig), body, now, tolerance), "untouched")

	tests := []struct {
		name    string
		secrets []string
		headers http.Header
		body    []byte
		at      time.Time
	}{
		{"body changed", []string{secret}, headers("msg_1", now, sig), []byte(`{"type":"order.shipped","data":{"order_id":"818"}}`), now},
		{"id changed", []string{secret}, headers("msg_2", now, sig), body, now},
		{"timestamp changed", []string{secret}, headers("msg_1", now.Add(time.Second), sig), body, now},
		{"wrong secret", []string{other}, headers("msg_1", now, sig), body, now},
		{"signature bytes flipped", []string{secret}, headers("msg_1", now, sig[:len(sig)-2]+"AA"), body, now},
		{"unknown version", []string{secret}, headers("msg_1", now, "v9"+sig[2:]), body, now},
		{"expired", []string{secret}, headers("msg_1", now, sig), body, now.Add(tolerance + time.Minute)},
		{"from the future", []string{secret}, headers("msg_1", now, sig), body, now.Add(-(tolerance + time.Minute))},
		{"no headers", []string{secret}, http.Header{}, body, now},
		{"no signature header", []string{secret}, func() http.Header { h := headers("msg_1", now, sig); h.Del(webhooks.HeaderSignature); return h }(), body, now},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Error(t, webhooks.Verify(tt.secrets, tt.headers, tt.body, tt.at, tolerance))
		})
	}

	otherSig, err := webhooks.Sign(other, "msg_1", now, body)
	require.NoError(t, err)
	require.NoError(t, webhooks.Verify([]string{other, secret}, headers("msg_1", now, sig), body, now, tolerance), "a receiver holding both secrets during rotation")
	require.NoError(t, webhooks.Verify([]string{secret}, headers("msg_1", now, otherSig+" "+sig), body, now, tolerance), "two signatures, one matches")
	require.Error(t, webhooks.Verify([]string{secret}, headers("msg_1", now, otherSig), body, now, tolerance), "only the other secret's signature")
}
