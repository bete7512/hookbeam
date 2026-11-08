package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bete7512/goauth/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/ratelimit"
)

const submission = `{"type":"order.shipped","external_id":"order-817","data":{"order_id":"817"}}`

var senderKey = types.Principal{OrganizationID: "ws_key", Methods: []string{"api_key"}}

// submit sends a submission with the given key, header and body through the router.
func submit(f *fixture, idempotencyKey, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/applications/7/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer hb_sender")
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	return f.send(req)
}

func expectSenderKey(f *fixture, scopes ...string) {
	f.apiKeys.EXPECT().Authenticate(gomock.Any(), "hb_sender").Return(senderKey, &models.APIKey{ID: 9, WorkspaceID: senderKey.OrganizationID, Scopes: scopes}, nil)
}

func TestSubmitMessage(t *testing.T) {
	accepted := &models.MessageAccepted{Message: models.Message{ID: 42, WebhookID: "msg_x", ApplicationID: 7, Type: "order.shipped", AcceptedAt: ada.AuthenticatedAt, DeliveryCount: 2}}
	in := services.SubmitMessageInput{ApplicationID: 7, IdempotencyKey: "order-817-v1", Payload: []byte(submission)}
	tests := []struct {
		name       string
		key        string
		body       string
		setup      func(f *fixture)
		wantStatus int
		wantError  string
	}{
		{name: "accepted", key: "order-817-v1", body: submission, setup: func(f *fixture) {
			expectSenderKey(f, models.ScopeMessagesWrite)
			f.delivery.EXPECT().SubmitMessage(gomock.Any(), "ws_key", in).Return(accepted, nil)
		}, wantStatus: http.StatusAccepted},
		// Parameter binding runs before authentication, so the key is never checked here.
		{name: "missing idempotency key is a binding error", key: "", body: submission, setup: func(*fixture) {}, wantStatus: http.StatusBadRequest, wantError: apperr.CodeBadRequest},
		{name: "key without the write scope", key: "k", body: submission, setup: func(f *fixture) {
			expectSenderKey(f, models.ScopeMessagesRead)
		}, wantStatus: http.StatusForbidden, wantError: apperr.CodeForbidden},
		{name: "no subscriber", key: "k", body: submission, setup: func(f *fixture) {
			expectSenderKey(f, models.ScopeMessagesWrite)
			f.delivery.EXPECT().SubmitMessage(gomock.Any(), "ws_key", gomock.Any()).Return(nil, apperr.NewValidation(map[string]string{"type": "no endpoint"}))
		}, wantStatus: http.StatusUnprocessableEntity, wantError: apperr.CodeValidation},
		{name: "key reused with other bytes", key: "k", body: submission, setup: func(f *fixture) {
			expectSenderKey(f, models.ScopeMessagesWrite)
			f.delivery.EXPECT().SubmitMessage(gomock.Any(), "ws_key", gomock.Any()).Return(nil, apperr.NewConflict("reused", nil))
		}, wantStatus: http.StatusConflict, wantError: apperr.CodeConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			tt.setup(f)
			rec := submit(f, tt.key, tt.body)
			require.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
			if tt.wantError != "" {
				var body struct {
					Error string `json:"error"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
				assert.Equal(t, tt.wantError, body.Error)
				return
			}
			var got openapi.MessageAccepted
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
			assert.Equal(t, int64(42), got.MessageId)
			assert.Equal(t, "msg_x", got.WebhookId)
			assert.Equal(t, openapi.Accepted, got.Status)
			assert.Equal(t, int32(2), got.DeliveryCount)
			assert.False(t, got.Duplicate)
		})
	}
	t.Run("a session cannot submit", func(t *testing.T) {
		f := newFixture(t)
		req := httptest.NewRequest(http.MethodPost, "/v1/applications/7/messages", strings.NewReader(submission))
		req.Header.Set("Authorization", "Bearer good")
		req.Header.Set("Idempotency-Key", "k")
		rec := f.send(req)
		require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	})
}

func TestGetMessage(t *testing.T) {
	reason := "http_503"
	message := &models.Message{ID: 42, WebhookID: "msg_x", ApplicationID: 7, Type: "order.shipped", AcceptedAt: ada.AuthenticatedAt, DeliveryCount: 1}
	deliveries := []models.Delivery{{ID: 5, MessageID: 42, EndpointID: 3, State: "retry_scheduled", AttemptCount: 1, DueAt: &ada.ExpiresAt, DeadlineAt: ada.ExpiresAt, FailureReason: &reason, CreatedAt: ada.AuthenticatedAt}}
	runEndpoints(t, []endpointCase{
		{"get", http.MethodGet, "/v1/messages/42", "", func(f *fixture) {
			f.delivery.EXPECT().GetMessage(gomock.Any(), workspace, int64(42)).Return(message, deliveries, nil)
		}, http.StatusOK, "", true},
		{"not found", http.MethodGet, "/v1/messages/42", "", func(f *fixture) {
			f.delivery.EXPECT().GetMessage(gomock.Any(), workspace, int64(42)).Return(nil, nil, apperr.NewNotFound("message", nil))
		}, http.StatusNotFound, apperr.CodeNotFound, true},
		{"bad id", http.MethodGet, "/v1/messages/zero", "", nil, http.StatusBadRequest, apperr.CodeBadRequest, false},
		{"session without a workspace", http.MethodGet, "/v1/messages/42", "", func(f *fixture) {
			f.auth.principal.OrganizationID = ""
		}, http.StatusForbidden, apperr.CodeForbidden, false},
	})

	t.Run("body", func(t *testing.T) {
		f := newFixture(t)
		f.delivery.EXPECT().GetMessage(gomock.Any(), workspace, int64(42)).Return(message, deliveries, nil)
		rec := f.do(http.MethodGet, "/v1/messages/42", "")
		require.Equal(t, http.StatusOK, rec.Code)
		var got openapi.MessageDetail
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, "msg_x", got.Message.WebhookId)
		require.Len(t, got.Deliveries, 1)
		assert.Equal(t, openapi.RetryScheduled, got.Deliveries[0].State)
		assert.Equal(t, "http_503", *got.Deliveries[0].FailureReason)
	})
}

func TestSubmitMessageKeyBindingAndRate(t *testing.T) {
	accepted := &models.MessageAccepted{Message: models.Message{ID: 1, WebhookID: "msg_x"}}
	t.Run("a key bound to the application submits", func(t *testing.T) {
		f := newFixture(t)
		app := int64(7)
		f.apiKeys.EXPECT().Authenticate(gomock.Any(), "hb_sender").Return(senderKey, &models.APIKey{ID: 9, ApplicationID: &app, Scopes: []string{models.ScopeMessagesWrite}}, nil)
		f.delivery.EXPECT().SubmitMessage(gomock.Any(), senderKey.OrganizationID, gomock.Any()).Return(accepted, nil)
		rec := submit(f, "k1", `{"type":"a","data":{}}`)
		require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
		require.Equal(t, []string{"api-key:9"}, f.limiter.keys, "counted per key")
	})
	t.Run("a key bound to another application is refused before the data plane", func(t *testing.T) {
		f := newFixture(t)
		other := int64(8)
		f.apiKeys.EXPECT().Authenticate(gomock.Any(), "hb_sender").Return(senderKey, &models.APIKey{ID: 9, ApplicationID: &other, Scopes: []string{models.ScopeMessagesWrite}}, nil)
		rec := submit(f, "k1", `{"type":"a","data":{}}`)
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		require.Empty(t, f.limiter.keys)
	})
	t.Run("a key over its rate gets 429 with Retry-After", func(t *testing.T) {
		f := newFixture(t)
		f.limiter.decision = ratelimit.Decision{Allowed: false, RetryAfter: 42 * time.Second}
		expectSenderKey(f, models.ScopeMessagesWrite)
		rec := submit(f, "k1", `{"type":"a","data":{}}`)
		require.Equal(t, http.StatusTooManyRequests, rec.Code, rec.Body.String())
		require.Equal(t, "42", rec.Header().Get("Retry-After"))
	})
}

func TestGetMessageCarriesTheEnvironment(t *testing.T) {
	f := newFixture(t)
	f.delivery.EXPECT().GetMessage(gomock.Any(), workspace, int64(42)).Return(&models.Message{ID: 42, WebhookID: "msg_x", ApplicationID: 7, EnvironmentID: 2, EnvironmentKind: "production", Type: "a", AcceptedAt: ada.AuthenticatedAt}, nil, nil)
	rec := f.do(http.MethodGet, "/v1/messages/42", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got openapi.MessageDetail
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, int64(2), got.Message.EnvironmentId)
	require.Equal(t, openapi.EnvironmentKind("production"), got.Message.EnvironmentKind)
}
