package services_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

// headerCanary stands in for a credential a customer put in a custom header; it must never come back out.
const headerCanary = "CANARY-hdr-91c4e7"

// sealed encrypts a header set the way the service stores it.
func (s *EndpointServiceSuite) sealed(headers map[string]string) []byte {
	plain, err := json.Marshal(headers)
	s.Require().NoError(err)
	ct, err := s.cipher.Encrypt(plain)
	s.Require().NoError(err)
	return ct
}

// opened decrypts what the service handed to the repo.
func (s *EndpointServiceSuite) opened(ciphertext []byte) map[string]string {
	plain, err := s.cipher.Decrypt(ciphertext)
	s.Require().NoError(err)
	out := map[string]string{}
	s.Require().NoError(json.Unmarshal(plain, &out))
	return out
}

// audits returns every recorded audit detail as one string, to search for a leaked value.
func (s *EndpointServiceSuite) audits() string {
	var b strings.Builder
	for _, e := range s.audited {
		b.Write(e.Details)
	}
	return b.String()
}

func (s *EndpointServiceSuite) TestCreateEndpointHeaderValidation() {
	many := map[string]string{}
	for i := range 21 {
		many[fmt.Sprintf("x-h-%d", i)] = "v"
	}
	tests := []struct {
		name     string
		headers  map[string]string
		wantName string // the offending name the message must show; "" when the message names none
	}{
		{name: "content-type is Hookbeam's", headers: map[string]string{"Content-Type": headerCanary}, wantName: "content-type"},
		{name: "user-agent is Hookbeam's", headers: map[string]string{"User-Agent": headerCanary}, wantName: "user-agent"},
		{name: "host frames the request", headers: map[string]string{"host": headerCanary}, wantName: "host"},
		{name: "content-length frames the request", headers: map[string]string{"Content-Length": "1"}, wantName: "content-length"},
		{name: "transfer-encoding frames the request", headers: map[string]string{"Transfer-Encoding": "chunked"}, wantName: "transfer-encoding"},
		{name: "connection is hop-by-hop", headers: map[string]string{"Connection": "close"}, wantName: "connection"},
		{name: "proxy headers are refused by prefix", headers: map[string]string{"Proxy-Authorization": headerCanary}, wantName: "proxy-authorization"},
		{name: "the signature cannot be replaced", headers: map[string]string{"Webhook-Signature": headerCanary}, wantName: "webhook-signature"},
		{name: "any webhook- header is refused by prefix", headers: map[string]string{"webhook-extra": headerCanary}, wantName: "webhook-extra"},
		{name: "a name with a space", headers: map[string]string{"x api key": headerCanary}, wantName: "x api key"},
		{name: "a name with a colon", headers: map[string]string{"x:y": headerCanary}, wantName: "x:y"},
		{name: "an empty name", headers: map[string]string{"": headerCanary}},
		{name: "a name over 64 characters", headers: map[string]string{strings.Repeat("n", 65): headerCanary}, wantName: strings.Repeat("n", 64)},
		{name: "a value with CR LF would inject a header", headers: map[string]string{"x-api-key": headerCanary + "\r\nx-injected: 1"}, wantName: "x-api-key"},
		{name: "a value with a bare LF", headers: map[string]string{"x-api-key": headerCanary + "\nx"}, wantName: "x-api-key"},
		{name: "a value with NUL", headers: map[string]string{"x-api-key": headerCanary + "\x00"}, wantName: "x-api-key"},
		{name: "a value outside ASCII", headers: map[string]string{"x-api-key": headerCanary + "é"}, wantName: "x-api-key"},
		{name: "an empty value", headers: map[string]string{"x-api-key": "  "}, wantName: "x-api-key"},
		{name: "a value over 1024 bytes", headers: map[string]string{"x-api-key": strings.Repeat("v", 1025)}, wantName: "x-api-key"},
		{name: "the same name twice in different case", headers: map[string]string{"X-Api-Key": headerCanary, "x-api-key": headerCanary}, wantName: "x-api-key"},
		{name: "more than 20 headers", headers: many},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			_, _, _, err := s.svc.CreateEndpoint(context.Background(), scope, services.CreateEndpointInput{
				ApplicationID: 7,
				URL:           "https://receiver.example.com/hooks",
				Headers:       tt.headers,
			})
			requireErr(&s.Suite, err, 422, []string{"headers"})
			var appErr *apperr.AppError
			s.Require().ErrorAs(err, &appErr)
			msg := appErr.Fields["headers"]
			s.NotContains(msg, headerCanary, "the message never quotes a value")
			if tt.wantName != "" {
				s.Contains(msg, fmt.Sprintf("%q", tt.wantName), "the message names the header")
			}
			s.Empty(s.audited)
		})
	}
}

func (s *EndpointServiceSuite) TestCreateEndpointWithHeaders() {
	s.Run("headers are stored encrypted under lower-case names and only the names come back", func() {
		s.expectApplication(true)
		s.endpoints.EXPECT().CreateEndpoint(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, e *models.Endpoint) error {
			s.NotContains(string(e.HeadersCiphertext), headerCanary, "never in the clear")
			s.Equal(map[string]string{"authorization": "Bearer " + headerCanary, "x-tenant": "acme"}, s.opened(e.HeadersCiphertext))
			s.Equal("2026-11", e.HeadersKeyID)
			e.ID = 42
			return nil
		})
		s.secrets.EXPECT().CreateEndpointSecret(gomock.Any(), gomock.Any()).Return(nil)
		s.subs.EXPECT().CreateSubscriptions(gomock.Any(), int64(42), gomock.Any()).Return(nil)
		e, _, _, err := s.svc.CreateEndpoint(context.Background(), scope, services.CreateEndpointInput{
			ApplicationID: 7,
			URL:           "https://receiver.example.com/hooks",
			Headers:       map[string]string{"X-Tenant": " acme ", "Authorization": "Bearer " + headerCanary},
		})
		s.Require().NoError(err)
		s.Equal([]string{"authorization", "x-tenant"}, e.HeaderNames)
		s.Require().Len(s.audited, 1)
		s.Contains(s.audits(), `"header_names":["authorization","x-tenant"]`)
		s.NotContains(s.audits(), headerCanary, "the audit trail names headers, never their values")
	})
	s.Run("no headers stores NULL and answers an empty list", func() {
		s.expectApplication(true)
		s.endpoints.EXPECT().CreateEndpoint(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, e *models.Endpoint) error {
			s.Nil(e.HeadersCiphertext)
			s.Empty(e.HeadersKeyID)
			return nil
		})
		s.secrets.EXPECT().CreateEndpointSecret(gomock.Any(), gomock.Any()).Return(nil)
		s.subs.EXPECT().CreateSubscriptions(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		e, _, _, err := s.svc.CreateEndpoint(context.Background(), scope, services.CreateEndpointInput{ApplicationID: 7, URL: "https://receiver.example.com/hooks"})
		s.Require().NoError(err)
		s.NotNil(e.HeaderNames)
		s.Empty(e.HeaderNames)
	})
}

func (s *EndpointServiceSuite) TestUpdateEndpointHeaders() {
	full := map[string]string{}
	for i := range 20 {
		full[fmt.Sprintf("x-h-%02d", i)] = "v"
	}
	tests := []struct {
		name       string
		stored     map[string]string // what the endpoint holds before
		set        map[string]string
		remove     []string
		want       map[string]string // what is written; nil means the set is cleared
		wantNames  []string
		wantStatus int
		wantWrite  bool // whether the update statement runs at all
	}{
		{
			name:      "a value replaces, a removal drops and an untouched header keeps its value",
			stored:    map[string]string{"x-keep": "kept", "x-old": "old", "x-drop": "gone"},
			set:       map[string]string{"X-Old": headerCanary, "x-new": "n"},
			remove:    []string{"X-Drop"},
			want:      map[string]string{"x-keep": "kept", "x-old": headerCanary, "x-new": "n"},
			wantNames: []string{"x-keep", "x-new", "x-old"},
			wantWrite: true,
		},
		{
			name:      "removing the last header clears the column",
			stored:    map[string]string{"x-only": headerCanary},
			remove:    []string{"x-only"},
			wantNames: []string{},
			wantWrite: true,
		},
		{
			name:      "removing a header that is not there changes nothing but still succeeds",
			stored:    map[string]string{"x-keep": "kept"},
			remove:    []string{"x-absent"},
			want:      map[string]string{"x-keep": "kept"},
			wantNames: []string{"x-keep"},
			wantWrite: true,
		},
		{
			name:      "the first header of an endpoint that had none",
			set:       map[string]string{"x-api-key": headerCanary},
			want:      map[string]string{"x-api-key": headerCanary},
			wantNames: []string{"x-api-key"},
			wantWrite: true,
		},
		{
			name:       "a twenty-first header is refused inside the transaction",
			stored:     full,
			set:        map[string]string{"x-one-more": headerCanary},
			wantStatus: 422,
			wantWrite:  true,
		},
		{
			name:       "a name both set and removed is refused before any write",
			stored:     map[string]string{"x-a": "1"},
			set:        map[string]string{"x-a": headerCanary},
			remove:     []string{"X-A"},
			wantStatus: 422,
		},
		{
			name:       "a reserved name is refused before any write",
			set:        map[string]string{"webhook-id": headerCanary},
			wantStatus: 422,
		},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			current := &models.Endpoint{ID: 3, WorkspaceID: ws, ApplicationID: 7, URL: "https://r.example.com/hooks", Revision: 1}
			if tt.stored != nil {
				current.HeadersCiphertext, current.HeadersKeyID = s.sealed(tt.stored), "2026-11"
			}
			s.endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(3)).Return(current, nil)
			var written map[string]string
			if tt.wantWrite {
				bumped := *current
				bumped.Revision = 2
				s.endpoints.EXPECT().UpdateEndpoint(gomock.Any(), ws, int64(3), current.URL, "", nil).Return(&bumped, nil)
			}
			if tt.wantWrite && tt.wantStatus == 0 {
				s.endpoints.EXPECT().SetEndpointHeaders(gomock.Any(), ws, int64(3), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, _ string, _ int64, ciphertext []byte, keyID string) error {
					if ciphertext == nil {
						s.Empty(keyID, "no ciphertext, no key id")
						return nil
					}
					s.Equal("2026-11", keyID)
					s.NotContains(string(ciphertext), headerCanary)
					written = s.opened(ciphertext)
					return nil
				})
				s.subs.EXPECT().ListEventTypes(gomock.Any(), int64(3)).Return([]string{}, nil)
			}
			got, err := s.svc.UpdateEndpoint(context.Background(), scope, services.UpdateEndpointInput{
				EndpointID:    3,
				SetHeaders:    tt.set,
				RemoveHeaders: tt.remove,
			})
			requireErr(&s.Suite, err, tt.wantStatus, nil)
			s.NotContains(s.audits(), headerCanary, "the audit trail never holds a value")
			if tt.wantStatus != 0 {
				var appErr *apperr.AppError
				s.Require().ErrorAs(err, &appErr)
				s.Contains(appErr.Fields, "headers")
				s.NotContains(appErr.Fields["headers"], headerCanary)
				s.Empty(s.audited)
				return
			}
			s.Equal(tt.want, written)
			s.Equal(tt.wantNames, got.HeaderNames)
			s.Equal(int32(2), got.Revision, "a header change is a configuration change")
			s.Require().Len(s.audited, 1)
			s.Contains(s.audits(), `"header_names"`)
		})
	}
	s.Run("a write that finds no endpoint is not found", func() {
		current := &models.Endpoint{ID: 3, WorkspaceID: ws, ApplicationID: 7, URL: "https://r.example.com/hooks", Revision: 1}
		s.endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(3)).Return(current, nil)
		s.endpoints.EXPECT().UpdateEndpoint(gomock.Any(), ws, int64(3), current.URL, "", nil).Return(current, nil)
		s.endpoints.EXPECT().SetEndpointHeaders(gomock.Any(), ws, int64(3), gomock.Any(), gomock.Any()).Return(apperr.ErrNotFound)
		_, err := s.svc.UpdateEndpoint(context.Background(), scope, services.UpdateEndpointInput{EndpointID: 3, SetHeaders: map[string]string{"x-a": "1"}})
		requireErr(&s.Suite, err, 404, nil)
	})
}

func (s *EndpointServiceSuite) TestEndpointReadsCarryHeaderNamesOnly() {
	stored := &models.Endpoint{ID: 3, WorkspaceID: ws, ApplicationID: 7, URL: "https://r.example.com/hooks", Revision: 1}
	s.Run("a single read lists the names, sorted", func() {
		e := *stored
		e.HeadersCiphertext = s.sealed(map[string]string{"x-b": headerCanary, "authorization": headerCanary})
		s.endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(3)).Return(&e, nil)
		s.subs.EXPECT().ListEventTypes(gomock.Any(), int64(3)).Return(nil, nil)
		got, err := s.svc.GetEndpoint(context.Background(), scope, 3)
		s.Require().NoError(err)
		s.Equal([]string{"authorization", "x-b"}, got.HeaderNames)
	})
	s.Run("a list read lists the names of each endpoint and an empty list for one without headers", func() {
		with, without := *stored, *stored
		with.HeadersCiphertext = s.sealed(map[string]string{"x-b": headerCanary})
		without.ID = 4
		s.apps.EXPECT().GetApplication(gomock.Any(), ws, int64(7)).Return(&models.Application{ID: 7, WorkspaceID: ws}, nil)
		s.endpoints.EXPECT().ListEndpoints(gomock.Any(), ws, int64(7)).Return([]models.Endpoint{with, without}, nil)
		s.subs.EXPECT().ListEventTypesForEndpoints(gomock.Any(), []int64{3, 4}).Return(map[int64][]string{}, nil)
		got, err := s.svc.ListEndpoints(context.Background(), scope, 7)
		s.Require().NoError(err)
		s.Require().Len(got, 2)
		s.Equal([]string{"x-b"}, got[0].HeaderNames)
		s.NotNil(got[1].HeaderNames)
		s.Empty(got[1].HeaderNames)
	})
	s.Run("headers that cannot be decrypted are an internal error, not an empty list", func() {
		e := *stored
		e.HeadersCiphertext = []byte("not a ciphertext produced by this key")
		s.endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(3)).Return(&e, nil)
		s.subs.EXPECT().ListEventTypes(gomock.Any(), int64(3)).Return(nil, nil)
		_, err := s.svc.GetEndpoint(context.Background(), scope, 3)
		requireErr(&s.Suite, err, 500, nil)
	})
}

func (s *DispatchServiceSuite) TestDispatchCustomHeaders() {
	seal := func(headers map[string]string) []byte {
		plain, err := json.Marshal(headers)
		s.Require().NoError(err)
		ct, err := s.cipher.Encrypt(plain)
		s.Require().NoError(err)
		return ct
	}
	s.Run("the endpoint's headers travel next to the signed ones and are never recorded", func() {
		rec := tracetest.NewSpanRecorder()
		otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
		defer otel.SetTracerProvider(sdktrace.NewTracerProvider())
		endpoint := &models.Endpoint{ID: 3, URL: "https://receiver.example.com/hooks", Revision: 2, HeadersCiphertext: seal(map[string]string{"authorization": "Bearer " + headerCanary})}
		s.sender.res = services.SendResult{StatusCode: 204}
		s.deliveries.EXPECT().ClaimDueDeliveries(gomock.Any(), 4, 2, gomock.Any(), time.Minute).Return([]models.Delivery{s.claimed(1)}, nil)
		s.expectLoads(endpoint, 1)
		var recorded models.Attempt
		s.attempts.EXPECT().CreateAttempt(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, a *models.Attempt) error {
			recorded = *a
			return nil
		})
		s.deliveries.EXPECT().RecordOutcome(gomock.Any(), gomock.Any()).Return(models.DeliverySucceeded, nil)
		var event []byte
		s.outbox.EXPECT().CreateOutboxEvent(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, e *models.OutboxEvent) error {
			event = e.Payload
			return nil
		})
		_, err := s.svc.Dispatch(context.Background())
		s.Require().NoError(err)
		s.svc.Drain()
		s.Require().Len(s.sender.got, 1)
		req := s.sender.got[0]
		s.Equal(map[string]string{"authorization": "Bearer " + headerCanary}, req.CustomHeaders)
		s.NotContains(req.Headers, "authorization", "custom headers stay apart from the ones Hookbeam signs")
		s.NotEmpty(req.Headers["webhook-signature"])
		attempt, err := json.Marshal(recorded)
		s.Require().NoError(err)
		s.NotContains(string(attempt), headerCanary, "the attempt row")
		s.NotContains(string(event), headerCanary, "the state event")
		for _, sp := range rec.Ended() {
			for _, kv := range sp.Attributes() {
				s.NotContains(kv.Value.String(), headerCanary, "span %s attribute %s", sp.Name(), kv.Key)
			}
		}
	})
	s.Run("headers that cannot be decrypted release the delivery before anything is sent", func() {
		endpoint := &models.Endpoint{ID: 3, URL: "https://receiver.example.com/hooks", Revision: 2, HeadersCiphertext: []byte("garbage")}
		s.deliveries.EXPECT().ClaimDueDeliveries(gomock.Any(), 4, 2, gomock.Any(), time.Minute).Return([]models.Delivery{s.claimed(1)}, nil)
		s.expectLoads(endpoint, 1)
		s.deliveries.EXPECT().ReleaseDelivery(gomock.Any(), int64(5), gomock.Any()).Return(nil)
		_, err := s.svc.Dispatch(context.Background())
		s.Require().NoError(err)
		s.svc.Drain()
		s.Empty(s.sender.got)
	})
	s.Run("a release that fails is reported with the cause", func() {
		endpoint := &models.Endpoint{ID: 3, URL: "https://receiver.example.com/hooks", Revision: 2, HeadersCiphertext: []byte("garbage")}
		s.deliveries.EXPECT().ClaimDueDeliveries(gomock.Any(), 4, 2, gomock.Any(), time.Minute).Return([]models.Delivery{s.claimed(1)}, nil)
		s.expectLoads(endpoint, 1)
		s.deliveries.EXPECT().ReleaseDelivery(gomock.Any(), int64(5), gomock.Any()).Return(errors.New("boom"))
		_, err := s.svc.Dispatch(context.Background())
		s.Require().NoError(err)
		s.svc.Drain()
		s.Empty(s.sender.got)
	})
}
