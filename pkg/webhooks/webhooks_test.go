package webhooks_test

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/pkg/webhooks"
)

// Vector from the Standard Webhooks specification.
const (
	secret    = "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw"
	msgID     = "msg_p5jXN8AQM9LWM0D4loKWxJek"
	body      = `{"test": 2432232314}`
	timestamp = 1614265330
	signature = "v1,g0hM9SsE+OTPJTGt/tmIKtSyZlE3uFJELVlNIOLJ1OE="
)

func headers(sig string) http.Header {
	h := http.Header{}
	h.Set(webhooks.HeaderID, msgID)
	h.Set(webhooks.HeaderTimestamp, strconv.Itoa(timestamp))
	h.Set(webhooks.HeaderSignature, sig)
	return h
}

func TestSign(t *testing.T) {
	got, err := webhooks.Sign(secret, msgID, time.Unix(timestamp, 0), []byte(body))
	require.NoError(t, err)
	require.Equal(t, signature, got)

	_, err = webhooks.Sign("nope", msgID, time.Unix(timestamp, 0), []byte(body))
	require.Error(t, err)
	_, err = webhooks.Sign("whsec_!!", msgID, time.Unix(timestamp, 0), []byte(body))
	require.Error(t, err)
}

func TestVerify(t *testing.T) {
	now := time.Unix(timestamp, 0)
	other := "whsec_" + "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	tests := []struct {
		name    string
		secrets []string
		headers http.Header
		body    string
		now     time.Time
		wantErr error
	}{
		{name: "valid", secrets: []string{secret}, headers: headers(signature), body: body, now: now},
		{name: "second secret during rotation", secrets: []string{other, secret}, headers: headers(signature), body: body, now: now},
		{name: "several signatures in the header", secrets: []string{secret}, headers: headers("v1,bogus= " + signature), body: body, now: now},
		{name: "wrong secret", secrets: []string{other}, headers: headers(signature), body: body, now: now, wantErr: webhooks.ErrNoMatch},
		{name: "tampered body", secrets: []string{secret}, headers: headers(signature), body: body + " ", now: now, wantErr: webhooks.ErrNoMatch},
		{name: "too old", secrets: []string{secret}, headers: headers(signature), body: body, now: now.Add(6 * time.Minute), wantErr: webhooks.ErrTimestampSkew},
		{name: "from the future", secrets: []string{secret}, headers: headers(signature), body: body, now: now.Add(-6 * time.Minute), wantErr: webhooks.ErrTimestampSkew},
		{name: "missing headers", secrets: []string{secret}, headers: http.Header{}, body: body, now: now, wantErr: webhooks.ErrMissingHeaders},
		{name: "unknown version only", secrets: []string{secret}, headers: headers("v2,abc"), body: body, now: now, wantErr: webhooks.ErrNoMatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := webhooks.Verify(tt.secrets, tt.headers, []byte(tt.body), tt.now, 5*time.Minute)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
	bad := headers(signature)
	bad.Set(webhooks.HeaderTimestamp, "yesterday")
	require.ErrorIs(t, webhooks.Verify([]string{secret}, bad, []byte(body), now, time.Minute), webhooks.ErrBadTimestamp)
}
