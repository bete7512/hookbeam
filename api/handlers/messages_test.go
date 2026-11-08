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
		assert.Equal(t, openapi.DeliveryStateRetryScheduled, got.Deliveries[0].State)
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

func TestListMessages(t *testing.T) {
	summary := models.MessageSummary{Message: models.Message{ID: 1, WebhookID: "msg_x", ApplicationID: 7, EnvironmentID: 2, EnvironmentKind: "production", Type: "order.paid", AcceptedAt: ada.AuthenticatedAt, DeliveryCount: 3}, Counts: models.DeliveryCounts{Succeeded: 2, Failed: 1}, PayloadExpiresAt: ada.ExpiresAt}
	runEndpoints(t, []endpointCase{
		{"application list with filters", http.MethodGet, "/v1/applications/7/messages?type=order.paid&outcome=failed&limit=5&sort_by=acceptedAt&sort_dir=desc", "", func(f *fixture) {
			f.delivery.EXPECT().ListMessages(gomock.Any(), workspace, services.ListMessagesInput{ApplicationID: 7, Type: "order.paid", Outcome: "failed", Limit: 5, SortBy: "acceptedAt", SortDir: "desc"}).Return([]models.MessageSummary{summary}, 1, nil)
		}, http.StatusOK, "", true},
		{"environment list with defaults", http.MethodGet, "/v1/environments/2/messages", "", func(f *fixture) {
			f.delivery.EXPECT().ListMessages(gomock.Any(), workspace, services.ListMessagesInput{EnvironmentID: 2, Limit: 20}).Return(nil, 0, nil)
		}, http.StatusOK, "", true},
		{"bad outcome is refused by the data plane", http.MethodGet, "/v1/applications/7/messages?outcome=lost", "", func(f *fixture) {
			f.delivery.EXPECT().ListMessages(gomock.Any(), workspace, gomock.Any()).Return(nil, 0, apperr.NewValidation(map[string]string{"outcome": "x"}))
		}, http.StatusUnprocessableEntity, apperr.CodeValidation, true},
		{"unknown application", http.MethodGet, "/v1/applications/7/messages", "", func(f *fixture) {
			f.delivery.EXPECT().ListMessages(gomock.Any(), workspace, gomock.Any()).Return(nil, 0, apperr.NewNotFound("application", nil))
		}, http.StatusNotFound, apperr.CodeNotFound, true},
	})
	t.Run("body carries counts and never a payload", func(t *testing.T) {
		f := newFixture(t)
		f.delivery.EXPECT().ListMessages(gomock.Any(), workspace, gomock.Any()).Return([]models.MessageSummary{summary}, 1, nil)
		rec := f.do(http.MethodGet, "/v1/applications/7/messages", "")
		require.Equal(t, http.StatusOK, rec.Code)
		var got openapi.MessageList
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.Equal(t, 1, got.Total)
		require.Equal(t, int32(2), got.Items[0].DeliveryCounts.Succeeded)
		require.Equal(t, openapi.EnvironmentKind("production"), got.Items[0].Message.EnvironmentKind)
		require.NotContains(t, rec.Body.String(), "payload\"")
	})
	t.Run("a key with messages:read lists", func(t *testing.T) {
		f := newFixture(t)
		expectSenderKey(f, models.ScopeMessagesRead)
		f.delivery.EXPECT().ListMessages(gomock.Any(), senderKey.OrganizationID, gomock.Any()).Return(nil, 0, nil)
		req := httptest.NewRequest(http.MethodGet, "/v1/applications/7/messages", nil)
		req.Header.Set("Authorization", "Bearer hb_sender")
		require.Equal(t, http.StatusOK, f.send(req).Code)
	})
}

func TestGetMessagePayload(t *testing.T) {
	body := []byte(`{"type":"order.shipped","data":{"order_id":"817"}}`)
	payload := &models.MessagePayload{Message: models.Message{ID: 42, WebhookID: "msg_x"}, Body: body, ExpiresAt: ada.ExpiresAt}
	runEndpoints(t, []endpointCase{
		{"read", http.MethodGet, "/v1/messages/42/payload", "", func(f *fixture) {
			f.delivery.EXPECT().GetMessagePayload(gomock.Any(), adaActor, int64(42)).Return(payload, nil)
		}, http.StatusOK, "", true},
		{"viewer is refused", http.MethodGet, "/v1/messages/42/payload", "", func(f *fixture) {
			f.delivery.EXPECT().GetMessagePayload(gomock.Any(), adaActor, int64(42)).Return(nil, apperr.NewForbidden("a viewer cannot read payloads and response previews", nil))
		}, http.StatusForbidden, apperr.CodeForbidden, true},
		{"expired payload is gone", http.MethodGet, "/v1/messages/42/payload", "", func(f *fixture) {
			f.delivery.EXPECT().GetMessagePayload(gomock.Any(), adaActor, int64(42)).Return(nil, apperr.NewGone("payload has expired", nil))
		}, http.StatusGone, apperr.CodeGone, true},
		{"not found", http.MethodGet, "/v1/messages/42/payload", "", func(f *fixture) {
			f.delivery.EXPECT().GetMessagePayload(gomock.Any(), adaActor, int64(42)).Return(nil, apperr.NewNotFound("message", nil))
		}, http.StatusNotFound, apperr.CodeNotFound, true},
		{"bad id", http.MethodGet, "/v1/messages/zero/payload", "", nil, http.StatusBadRequest, apperr.CodeBadRequest, false},
	})
	t.Run("the body is the payload byte for byte", func(t *testing.T) {
		f := newFixture(t)
		f.delivery.EXPECT().GetMessagePayload(gomock.Any(), adaActor, int64(42)).Return(payload, nil)
		rec := f.do(http.MethodGet, "/v1/messages/42/payload", "")
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
		assert.Equal(t, body, rec.Body.Bytes())
	})
	t.Run("a key needs payloads:read", func(t *testing.T) {
		f := newFixture(t)
		expectSenderKey(f, models.ScopeMessagesRead)
		req := httptest.NewRequest(http.MethodGet, "/v1/messages/42/payload", nil)
		req.Header.Set("Authorization", "Bearer hb_sender")
		rec := f.send(req)
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

		f = newFixture(t)
		expectSenderKey(f, models.ScopePayloadsRead)
		f.delivery.EXPECT().GetMessagePayload(gomock.Any(), services.Actor{WorkspaceID: senderKey.OrganizationID}, int64(42)).Return(payload, nil)
		req = httptest.NewRequest(http.MethodGet, "/v1/messages/42/payload", nil)
		req.Header.Set("Authorization", "Bearer hb_sender")
		rec = f.send(req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	})
}
