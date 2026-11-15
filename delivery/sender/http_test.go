package sender_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bete7512/hookbeam/delivery/sender"
	"github.com/bete7512/hookbeam/delivery/services"
	"github.com/bete7512/hookbeam/pkg/safehttp"
)

func TestSend(t *testing.T) {
	var got *http.Request
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		b := make([]byte, 1024)
		n, _ := r.Body.Read(b)
		gotBody = string(b[:n])
		switch r.URL.Path {
		case "/slow":
			time.Sleep(300 * time.Millisecond)
		case "/redirect":
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
			return
		case "/big":
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(strings.Repeat("x", 5000) + "é"))
			return
		}
		w.Header().Set("Set-Cookie", "secret=1")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := safehttp.Config{Timeout: 200 * time.Millisecond, AllowPrivate: true}.NewClient()
	s := sender.New(client, sender.Config{MaxResponseBytes: 4096, MaxPreviewBytes: 16})
	ctx := context.Background()
	req := services.SendRequest{URL: srv.URL + "/hooks", Headers: map[string]string{"webhook-id": "msg_1", "content-type": "application/json"}, Body: []byte(`{"a":1}`)}

	res := s.Send(ctx, req)
	require.NoError(t, res.Err)
	require.Equal(t, http.StatusNoContent, res.StatusCode)
	require.Equal(t, http.MethodPost, got.Method)
	require.Equal(t, "msg_1", got.Header.Get("webhook-id"))
	require.Equal(t, `{"a":1}`, gotBody)
	require.NotContains(t, res.Headers, "set-cookie")

	res = s.Send(ctx, services.SendRequest{URL: srv.URL + "/big", Body: []byte(`{}`)})
	require.Equal(t, http.StatusServiceUnavailable, res.StatusCode)
	require.Equal(t, "30", res.Headers["retry-after"])
	require.Equal(t, strings.Repeat("x", 16), res.BodyPreview, "preview is bounded")

	res = s.Send(ctx, services.SendRequest{URL: srv.URL + "/redirect", Body: []byte(`{}`)})
	require.Equal(t, http.StatusFound, res.StatusCode, "redirects are recorded, not followed")

	res = s.Send(ctx, services.SendRequest{URL: srv.URL + "/slow", Body: []byte(`{}`)})
	require.Equal(t, 0, res.StatusCode)
	require.Equal(t, "timeout", res.ErrorClass)

	res = s.Send(ctx, services.SendRequest{URL: "http://127.0.0.1:1/", Body: []byte(`{}`)})
	require.Equal(t, "connection", res.ErrorClass)

	// A resolver that answers NXDOMAIN gives "dns"; one that is slow or absent gives "timeout" within the budget.
	res = s.Send(ctx, services.SendRequest{URL: "http://name.invalid/", Body: []byte(`{}`)})
	require.Contains(t, []string{"dns", "timeout"}, res.ErrorClass)

	strict := sender.New(safehttp.Config{Timeout: time.Second}.NewClient(), sender.Config{})
	res = strict.Send(ctx, services.SendRequest{URL: srv.URL, Body: []byte(`{}`)})
	require.Equal(t, "blocked_destination", res.ErrorClass)

	res = s.Send(ctx, services.SendRequest{URL: "://bad", Body: nil})
	require.Equal(t, "connection", res.ErrorClass)
}

func TestSendTLSFailure(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	s := sender.New(safehttp.Config{Timeout: time.Second, AllowPrivate: true}.NewClient(), sender.Config{})
	res := s.Send(context.Background(), services.SendRequest{URL: srv.URL, Body: []byte(`{}`)})
	require.Equal(t, "tls", res.ErrorClass, "self-signed certificate is refused")
}

func TestSendCustomHeaders(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Authorization", "Bearer echoed-by-the-receiver")
		w.Header().Set("X-Api-Key", "echoed-by-the-receiver")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	s := sender.New(safehttp.Config{Timeout: time.Second, AllowPrivate: true}.NewClient(), sender.Config{})
	fixed := map[string]string{
		"content-type":      "application/json",
		"user-agent":        "Hookbeam/1",
		"webhook-id":        "msg_1",
		"webhook-timestamp": "1700000000",
		"webhook-signature": "v1,real",
	}
	tests := []struct {
		name   string
		custom map[string]string
		want   map[string]string // what the receiver must see
	}{
		{name: "custom headers arrive next to the signed ones", custom: map[string]string{"authorization": "Bearer t0ken", "x-tenant": "acme"}, want: map[string]string{"Authorization": "Bearer t0ken", "X-Tenant": "acme", "Webhook-Signature": "v1,real"}},
		{name: "no custom headers", want: map[string]string{"Webhook-Signature": "v1,real", "Content-Type": "application/json"}},
		{
			name:   "a custom header can never replace what Hookbeam sends, whatever its case",
			custom: map[string]string{"webhook-signature": "v1,forged", "Webhook-Id": "msg_forged", "WEBHOOK-TIMESTAMP": "1", "Content-Type": "text/plain", "User-Agent": "evil/1"},
			want:   map[string]string{"Webhook-Signature": "v1,real", "Webhook-Id": "msg_1", "Webhook-Timestamp": "1700000000", "Content-Type": "application/json", "User-Agent": "Hookbeam/1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := s.Send(context.Background(), services.SendRequest{URL: srv.URL + "/hooks", Headers: fixed, CustomHeaders: tt.custom, Body: []byte(`{}`)})
			require.NoError(t, res.Err)
			require.Equal(t, http.StatusNoContent, res.StatusCode)
			for name, value := range tt.want {
				require.Equal(t, []string{value}, got.Values(name), name)
			}
			require.NotContains(t, res.Headers, "authorization", "a credential echoed by the receiver is not recorded")
			require.NotContains(t, res.Headers, "x-api-key")
		})
	}
}
