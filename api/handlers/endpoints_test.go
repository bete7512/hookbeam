package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/api/gen/openapi"
	"github.com/bete7512/hookbeam/api/models"
	"github.com/bete7512/hookbeam/api/services"
	"github.com/bete7512/hookbeam/pkg/apperr"
)

func TestEndpoints(t *testing.T) {
	endpoint := &models.Endpoint{ID: 3, ApplicationID: 7, URL: "https://receiver.example.com/hooks", Revision: 1, EventTypes: []string{"order.shipped"}, CreatedAt: ada.AuthenticatedAt, Health: models.EndpointHealthStatus{State: "healthy", ObservedAt: &ada.AuthenticatedAt}}
	in := services.CreateEndpointInput{ApplicationID: 7, URL: endpoint.URL, Description: "prod", EventTypes: []string{"order.shipped"}}
	paused := *endpoint
	now := time.Now()
	paused.PausedAt = &now
	runEndpoints(t, []endpointCase{
		{"create", http.MethodPost, "/v1/applications/7/endpoints", `{"url":"https://receiver.example.com/hooks","description":"prod","eventTypes":["order.shipped"]}`, func(f *fixture) {
			f.delivery.EXPECT().CreateEndpoint(gomock.Any(), adaActor, in).Return(endpoint, "whsec_abc", int32(1), nil)
		}, http.StatusCreated, "", true},
		{"create for an unknown application", http.MethodPost, "/v1/applications/7/endpoints", `{"url":"https://receiver.example.com/hooks"}`, func(f *fixture) {
			f.delivery.EXPECT().CreateEndpoint(gomock.Any(), adaActor, services.CreateEndpointInput{ApplicationID: 7, URL: endpoint.URL}).Return(nil, "", int32(0), apperr.NewNotFound("application", nil))
		}, http.StatusNotFound, apperr.CodeNotFound, true},
		{"create with a bad application id", http.MethodPost, "/v1/applications/zero/endpoints", `{"url":"https://x"}`, nil, http.StatusBadRequest, apperr.CodeBadRequest, false},
		{"create with a bad body", http.MethodPost, "/v1/applications/7/endpoints", `{"url":1}`, nil, http.StatusBadRequest, apperr.CodeBadRequest, false},
		{"get", http.MethodGet, "/v1/endpoints/3", "", func(f *fixture) {
			f.delivery.EXPECT().GetEndpoint(gomock.Any(), adaActor, int64(3)).Return(endpoint, nil)
		}, http.StatusOK, "", true},
		{"get an unknown endpoint", http.MethodGet, "/v1/endpoints/3", "", func(f *fixture) {
			f.delivery.EXPECT().GetEndpoint(gomock.Any(), adaActor, int64(3)).Return(nil, apperr.NewNotFound("endpoint", nil))
		}, http.StatusNotFound, apperr.CodeNotFound, true},
		{"pause", http.MethodPost, "/v1/endpoints/3/pause", "", func(f *fixture) {
			f.delivery.EXPECT().PauseEndpoint(gomock.Any(), adaActor, int64(3)).Return(&paused, nil)
		}, http.StatusOK, "", true},
		{"pause an archived endpoint", http.MethodPost, "/v1/endpoints/3/pause", "", func(f *fixture) {
			f.delivery.EXPECT().PauseEndpoint(gomock.Any(), adaActor, int64(3)).Return(nil, apperr.NewConflict("endpoint is archived", nil))
		}, http.StatusConflict, apperr.CodeConflict, true},
		{"update url and subscriptions", http.MethodPatch, "/v1/endpoints/3", `{"url":"https://new.example.com/hooks","eventTypes":["order.paid"]}`, func(f *fixture) {
			url, types := "https://new.example.com/hooks", []string{"order.paid"}
			updated := *endpoint
			updated.URL, updated.Revision, updated.EventTypes = url, 2, types
			f.delivery.EXPECT().UpdateEndpoint(gomock.Any(), adaActor, services.UpdateEndpointInput{EndpointID: 3, URL: &url, EventTypes: &types}).Return(&updated, nil)
		}, http.StatusOK, "", true},
		{"update with an empty body is a validation error", http.MethodPatch, "/v1/endpoints/3", `{}`, func(f *fixture) {
			f.delivery.EXPECT().UpdateEndpoint(gomock.Any(), adaActor, services.UpdateEndpointInput{EndpointID: 3}).Return(nil, apperr.NewValidation(map[string]string{"body": "x"}))
		}, http.StatusUnprocessableEntity, apperr.CodeValidation, true},
		{"update an archived endpoint", http.MethodPatch, "/v1/endpoints/3", `{"description":"x"}`, func(f *fixture) {
			f.delivery.EXPECT().UpdateEndpoint(gomock.Any(), adaActor, gomock.Any()).Return(nil, apperr.NewConflict("endpoint is archived", nil))
		}, http.StatusConflict, apperr.CodeConflict, true},
		{"update with a bad body", http.MethodPatch, "/v1/endpoints/3", `{"url":1}`, nil, http.StatusBadRequest, apperr.CodeBadRequest, false},
		{"rotate with the default overlap", http.MethodPost, "/v1/endpoints/3/rotate-secret", "", func(f *fixture) {
			f.delivery.EXPECT().RotateEndpointSecret(gomock.Any(), adaActor, int64(3), 24*time.Hour).Return(&models.SecretRotated{Endpoint: endpoint, SigningSecret: "whsec_new", SecretVersion: 2, RetireAt: now}, nil)
		}, http.StatusOK, "", true},
		{"emergency rotation", http.MethodPost, "/v1/endpoints/3/rotate-secret", `{"retireIn":"0s"}`, func(f *fixture) {
			f.delivery.EXPECT().RotateEndpointSecret(gomock.Any(), adaActor, int64(3), time.Duration(0)).Return(&models.SecretRotated{Endpoint: endpoint, SigningSecret: "whsec_new", SecretVersion: 2, RetireAt: now}, nil)
		}, http.StatusOK, "", true},
		{"rotate with an unparsable duration", http.MethodPost, "/v1/endpoints/3/rotate-secret", `{"retireIn":"tomorrow"}`, nil, http.StatusUnprocessableEntity, apperr.CodeValidation, true},
		{"rotate an archived endpoint", http.MethodPost, "/v1/endpoints/3/rotate-secret", "", func(f *fixture) {
			f.delivery.EXPECT().RotateEndpointSecret(gomock.Any(), adaActor, int64(3), 24*time.Hour).Return(nil, apperr.NewConflict("endpoint is archived", nil))
		}, http.StatusConflict, apperr.CodeConflict, true},
		{"resume", http.MethodPost, "/v1/endpoints/3/resume", "", func(f *fixture) {
			f.delivery.EXPECT().ResumeEndpoint(gomock.Any(), adaActor, int64(3)).Return(endpoint, nil)
		}, http.StatusOK, "", true},
		{"resume an archived endpoint", http.MethodPost, "/v1/endpoints/3/resume", "", func(f *fixture) {
			f.delivery.EXPECT().ResumeEndpoint(gomock.Any(), adaActor, int64(3)).Return(nil, apperr.NewConflict("endpoint is archived", nil))
		}, http.StatusConflict, apperr.CodeConflict, true},
		{"archive", http.MethodDelete, "/v1/endpoints/3", "", func(f *fixture) {
			archived := *endpoint
			archived.ArchivedAt = &now
			f.delivery.EXPECT().ArchiveEndpoint(gomock.Any(), adaActor, int64(3)).Return(&archived, int64(2), nil)
		}, http.StatusOK, "", true},
		{"archive an unknown endpoint", http.MethodDelete, "/v1/endpoints/3", "", func(f *fixture) {
			f.delivery.EXPECT().ArchiveEndpoint(gomock.Any(), adaActor, int64(3)).Return(nil, int64(0), apperr.NewNotFound("endpoint", nil))
		}, http.StatusNotFound, apperr.CodeNotFound, true},
		{"list an application's endpoints", http.MethodGet, "/v1/applications/7/endpoints", "", func(f *fixture) {
			f.delivery.EXPECT().ListEndpoints(gomock.Any(), adaActor, int64(7)).Return([]models.Endpoint{*endpoint}, nil)
		}, http.StatusOK, "", true},
		{"update with an expected revision that moved", http.MethodPatch, "/v1/endpoints/3", `{"description":"x","expectedRevision":2}`, func(f *fixture) {
			desc, rev := "x", int32(2)
			f.delivery.EXPECT().UpdateEndpoint(gomock.Any(), adaActor, services.UpdateEndpointInput{EndpointID: 3, Description: &desc, ExpectedRevision: &rev}).Return(nil, apperr.NewConflict("endpoint changed since it was loaded", nil))
		}, http.StatusConflict, apperr.CodeConflict, true},
		{"create with custom headers", http.MethodPost, "/v1/applications/7/endpoints", `{"url":"https://receiver.example.com/hooks","headers":{"X-Api-Key":"CANARY-hdr"}}`, func(f *fixture) {
			withHeaders := *endpoint
			withHeaders.HeaderNames = []string{"x-api-key"}
			f.delivery.EXPECT().CreateEndpoint(gomock.Any(), adaActor, services.CreateEndpointInput{ApplicationID: 7, URL: endpoint.URL, Headers: map[string]string{"X-Api-Key": "CANARY-hdr"}}).Return(&withHeaders, "whsec_abc", int32(1), nil)
		}, http.StatusCreated, "", true},
		{"create with a refused header", http.MethodPost, "/v1/applications/7/endpoints", `{"url":"https://receiver.example.com/hooks","headers":{"host":"x"}}`, func(f *fixture) {
			f.delivery.EXPECT().CreateEndpoint(gomock.Any(), adaActor, gomock.Any()).Return(nil, "", int32(0), apperr.NewValidation(map[string]string{"headers": `header "host" is set by Hookbeam`}))
		}, http.StatusUnprocessableEntity, apperr.CodeValidation, true},
		{"create with headers that are not strings", http.MethodPost, "/v1/applications/7/endpoints", `{"url":"https://receiver.example.com/hooks","headers":{"x-a":1}}`, nil, http.StatusBadRequest, apperr.CodeBadRequest, false},
		{"update merges headers: values are set, nulls are removed", http.MethodPatch, "/v1/endpoints/3", `{"headers":{"x-api-key":"CANARY-hdr","x-old":null,"x-gone":null}}`, func(f *fixture) {
			updated := *endpoint
			updated.Revision, updated.HeaderNames = 2, []string{"x-api-key"}
			f.delivery.EXPECT().UpdateEndpoint(gomock.Any(), adaActor, services.UpdateEndpointInput{
				EndpointID:    3,
				SetHeaders:    map[string]string{"x-api-key": "CANARY-hdr"},
				RemoveHeaders: []string{"x-gone", "x-old"},
			}).Return(&updated, nil)
		}, http.StatusOK, "", true},
		{"update with only removals", http.MethodPatch, "/v1/endpoints/3", `{"headers":{"x-old":null}}`, func(f *fixture) {
			f.delivery.EXPECT().UpdateEndpoint(gomock.Any(), adaActor, services.UpdateEndpointInput{
				EndpointID:    3,
				SetHeaders:    map[string]string{},
				RemoveHeaders: []string{"x-old"},
			}).Return(endpoint, nil)
		}, http.StatusOK, "", true},
		{"list an endpoint's deliveries with filters", http.MethodGet, "/v1/endpoints/3/deliveries?state=failed&limit=5&offset=10&sort_by=id&sort_dir=desc", "", func(f *fixture) {
			f.delivery.EXPECT().ListEndpointDeliveries(gomock.Any(), workspace, services.ListEndpointDeliveriesInput{EndpointID: 3, State: "failed", Limit: 5, Offset: 10, SortBy: "id", SortDir: "desc"}).Return(nil, 0, nil)
		}, http.StatusOK, "", true},
		{"list an endpoint's deliveries with defaults", http.MethodGet, "/v1/endpoints/3/deliveries", "", func(f *fixture) {
			f.delivery.EXPECT().ListEndpointDeliveries(gomock.Any(), workspace, services.ListEndpointDeliveriesInput{EndpointID: 3, Limit: 20}).Return(nil, 0, nil)
		}, http.StatusOK, "", true},
		{"deliveries of an unknown endpoint", http.MethodGet, "/v1/endpoints/3/deliveries", "", func(f *fixture) {
			f.delivery.EXPECT().ListEndpointDeliveries(gomock.Any(), workspace, gomock.Any()).Return(nil, 0, apperr.NewNotFound("endpoint", nil))
		}, http.StatusNotFound, apperr.CodeNotFound, true},
		{"deliveries with a limit the data plane refuses", http.MethodGet, "/v1/endpoints/3/deliveries?limit=500", "", func(f *fixture) {
			f.delivery.EXPECT().ListEndpointDeliveries(gomock.Any(), workspace, gomock.Any()).Return(nil, 0, apperr.NewValidation(map[string]string{"limit": "x"}))
		}, http.StatusUnprocessableEntity, apperr.CodeValidation, true},
		{"deliveries with a state outside the enum", http.MethodGet, "/v1/endpoints/3/deliveries?state=lost", "", func(f *fixture) {
			f.delivery.EXPECT().ListEndpointDeliveries(gomock.Any(), workspace, gomock.Any()).Return(nil, 0, apperr.NewValidation(map[string]string{"state": "x"}))
		}, http.StatusUnprocessableEntity, apperr.CodeValidation, true},
		{"deliveries with a limit that is not a number", http.MethodGet, "/v1/endpoints/3/deliveries?limit=many", "", nil, http.StatusBadRequest, apperr.CodeBadRequest, false},
		{"deliveries without a workspace", http.MethodGet, "/v1/endpoints/3/deliveries", "", func(f *fixture) {
			f.auth.principal.OrganizationID = ""
		}, http.StatusForbidden, apperr.CodeForbidden, false},
		{"send a test delivery", http.MethodPost, "/v1/endpoints/3/test", "", func(f *fixture) {
			f.delivery.EXPECT().SendTestMessage(gomock.Any(), adaActor, int64(3)).Return(&models.Message{ID: 42, WebhookID: "msg_t", ApplicationID: 7, Type: "hookbeam.test", AcceptedAt: ada.AuthenticatedAt, DeliveryCount: 1}, &models.Delivery{ID: 9, MessageID: 42, EndpointID: 3, State: "queued", DeadlineAt: ada.ExpiresAt, CreatedAt: ada.AuthenticatedAt}, nil)
		}, http.StatusAccepted, "", true},
		{"test on an archived endpoint", http.MethodPost, "/v1/endpoints/3/test", "", func(f *fixture) {
			f.delivery.EXPECT().SendTestMessage(gomock.Any(), adaActor, int64(3)).Return(nil, nil, apperr.NewConflict("endpoint is archived", nil))
		}, http.StatusConflict, apperr.CodeConflict, true},
	})
	t.Run("an api key is refused before the handler", func(t *testing.T) {
		f := newFixture(t)
		req := httptest.NewRequest(http.MethodPost, "/v1/endpoints/3/pause", nil)
		req.Header.Set("Authorization", "Bearer hb_sender")
		rec := f.send(req)
		require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
		require.Contains(t, rec.Body.String(), "does not accept api keys")
	})
	t.Run("deliveries body carries the event and the latest attempt, never a payload", func(t *testing.T) {
		f := newFixture(t)
		status, class, external := int32(503), "timeout", "order-817"
		items := []models.EndpointDelivery{
			{
				Delivery:    models.Delivery{ID: 9, MessageID: 42, EndpointID: 3, State: "retry_scheduled", AttemptCount: 2, DeadlineAt: ada.ExpiresAt, CreatedAt: ada.AuthenticatedAt},
				Message:     models.EndpointDeliveryMessage{ID: 42, WebhookID: "msg_abc", Type: "order.shipped", ExternalID: &external, AcceptedAt: ada.AuthenticatedAt},
				LastAttempt: &models.LastAttempt{Number: 2, StartedAt: now, LatencyMS: 31, StatusCode: &status},
			},
			{
				Delivery:    models.Delivery{ID: 8, MessageID: 41, EndpointID: 3, State: "failed", DeadlineAt: ada.ExpiresAt, CreatedAt: ada.AuthenticatedAt},
				Message:     models.EndpointDeliveryMessage{ID: 41, WebhookID: "msg_abb", Type: "order.created", AcceptedAt: ada.AuthenticatedAt},
				LastAttempt: &models.LastAttempt{Number: 1, StartedAt: now, LatencyMS: 10000, ErrorClass: &class},
			},
			{
				Delivery: models.Delivery{ID: 7, MessageID: 40, EndpointID: 3, State: "queued", DeadlineAt: ada.ExpiresAt, CreatedAt: ada.AuthenticatedAt},
				Message:  models.EndpointDeliveryMessage{ID: 40, WebhookID: "msg_aba", Type: "order.created", AcceptedAt: ada.AuthenticatedAt},
			},
		}
		f.delivery.EXPECT().ListEndpointDeliveries(gomock.Any(), workspace, gomock.Any()).Return(items, 43, nil)
		rec := f.do(http.MethodGet, "/v1/endpoints/3/deliveries", "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var got openapi.EndpointDeliveryList
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.Equal(t, 43, got.Total)
		require.Len(t, got.Items, 3)
		assert.Equal(t, int64(9), got.Items[0].Delivery.Id)
		assert.Equal(t, int64(42), got.Items[0].Message.Id)
		assert.Equal(t, "msg_abc", got.Items[0].Message.WebhookId)
		assert.Equal(t, "order.shipped", got.Items[0].Message.Type)
		assert.Equal(t, "order-817", *got.Items[0].Message.ExternalId)
		require.NotNil(t, got.Items[0].LastAttempt)
		assert.Equal(t, int32(503), *got.Items[0].LastAttempt.StatusCode)
		assert.Equal(t, int32(31), got.Items[0].LastAttempt.LatencyMs)
		assert.Equal(t, "timeout", *got.Items[1].LastAttempt.ErrorClass)
		assert.Nil(t, got.Items[1].LastAttempt.StatusCode)
		assert.Nil(t, got.Items[2].LastAttempt, "never attempted")
		assert.NotContains(t, rec.Body.String(), "payload\"")
	})
	t.Run("a key with messages:read lists an endpoint's deliveries", func(t *testing.T) {
		f := newFixture(t)
		expectSenderKey(f, models.ScopeMessagesRead)
		f.delivery.EXPECT().ListEndpointDeliveries(gomock.Any(), senderKey.OrganizationID, gomock.Any()).Return(nil, 0, nil)
		req := httptest.NewRequest(http.MethodGet, "/v1/endpoints/3/deliveries", nil)
		req.Header.Set("Authorization", "Bearer hb_sender")
		require.Equal(t, http.StatusOK, f.send(req).Code)
	})
	t.Run("a key without messages:read is refused", func(t *testing.T) {
		f := newFixture(t)
		expectSenderKey(f, models.ScopeMessagesWrite)
		req := httptest.NewRequest(http.MethodGet, "/v1/endpoints/3/deliveries", nil)
		req.Header.Set("Authorization", "Bearer hb_sender")
		require.Equal(t, http.StatusForbidden, f.send(req).Code)
	})
	t.Run("header names come back, header values never do", func(t *testing.T) {
		f := newFixture(t)
		withHeaders := *endpoint
		withHeaders.HeaderNames = []string{"authorization", "x-api-key"}
		f.delivery.EXPECT().CreateEndpoint(gomock.Any(), adaActor, gomock.Any()).Return(&withHeaders, "whsec_abc", int32(1), nil)
		rec := f.do(http.MethodPost, "/v1/applications/7/endpoints", `{"url":"https://receiver.example.com/hooks","headers":{"Authorization":"Bearer CANARY-hdr","X-Api-Key":"CANARY-hdr"}}`)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		var got openapi.EndpointCreated
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, []string{"authorization", "x-api-key"}, got.Endpoint.HeaderNames)
		assert.NotContains(t, rec.Body.String(), "CANARY-hdr")
	})
	t.Run("an endpoint without custom headers answers an empty list, not null", func(t *testing.T) {
		f := newFixture(t)
		f.delivery.EXPECT().GetEndpoint(gomock.Any(), adaActor, int64(3)).Return(endpoint, nil)
		rec := f.do(http.MethodGet, "/v1/endpoints/3", "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.Contains(t, rec.Body.String(), `"headerNames":[]`)
	})
	t.Run("detail body carries waiting and secret fingerprints", func(t *testing.T) {
		f := newFixture(t)
		waiting := int32(5)
		detailed := *endpoint
		detailed.Waiting = &waiting
		detailed.Secrets = []models.EndpointSecretInfo{{Version: 2, Fingerprint: "abcd", CreatedAt: now}, {Version: 1, Fingerprint: "0123", CreatedAt: now, RetireAt: &now}}
		f.delivery.EXPECT().GetEndpoint(gomock.Any(), adaActor, int64(3)).Return(&detailed, nil)
		rec := f.do(http.MethodGet, "/v1/endpoints/3", "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var got openapi.Endpoint
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.NotNil(t, got.Waiting)
		assert.Equal(t, int32(5), *got.Waiting)
		require.NotNil(t, got.Secrets)
		require.Len(t, *got.Secrets, 2)
		assert.Equal(t, "abcd", (*got.Secrets)[0].Fingerprint)
		assert.NotNil(t, (*got.Secrets)[1].RetireAt)
	})
	t.Run("test delivery body", func(t *testing.T) {
		f := newFixture(t)
		f.delivery.EXPECT().SendTestMessage(gomock.Any(), adaActor, int64(3)).Return(&models.Message{ID: 42, WebhookID: "msg_t", ApplicationID: 7, Type: "hookbeam.test", AcceptedAt: ada.AuthenticatedAt, DeliveryCount: 1}, &models.Delivery{ID: 9, MessageID: 42, EndpointID: 3, State: "queued", DeadlineAt: ada.ExpiresAt, CreatedAt: ada.AuthenticatedAt}, nil)
		rec := f.do(http.MethodPost, "/v1/endpoints/3/test", "")
		require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
		var got openapi.TestMessageSent
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, "hookbeam.test", got.Message.Type)
		assert.Equal(t, int64(9), got.Delivery.Id)
	})
	t.Run("get body carries the health", func(t *testing.T) {
		f := newFixture(t)
		unhealthy := *endpoint
		unhealthy.Health = models.EndpointHealthStatus{State: "unhealthy", ObservedAt: &now, Stale: true, ConsecutiveFailures: 4}
		f.delivery.EXPECT().GetEndpoint(gomock.Any(), adaActor, int64(3)).Return(&unhealthy, nil)
		rec := f.do(http.MethodGet, "/v1/endpoints/3", "")
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var got openapi.Endpoint
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, openapi.EndpointHealthState("unhealthy"), got.Health.State)
		assert.True(t, got.Health.Stale)
		assert.Equal(t, int32(4), got.Health.ConsecutiveFailures)
		assert.WithinDuration(t, now, *got.Health.ObservedAt, time.Second)
	})
	t.Run("rotate body", func(t *testing.T) {
		f := newFixture(t)
		f.delivery.EXPECT().RotateEndpointSecret(gomock.Any(), adaActor, int64(3), 24*time.Hour).Return(&models.SecretRotated{Endpoint: endpoint, SigningSecret: "whsec_new", SecretVersion: 2, RetireAt: now}, nil)
		rec := f.do(http.MethodPost, "/v1/endpoints/3/rotate-secret", "")
		require.Equal(t, http.StatusOK, rec.Code)
		var got openapi.SecretRotated
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, "whsec_new", got.SigningSecret)
		assert.Equal(t, int32(2), got.SecretVersion)
		assert.WithinDuration(t, now, got.RetireAt, time.Second)
	})
	t.Run("archive body", func(t *testing.T) {
		f := newFixture(t)
		archived := *endpoint
		archived.ArchivedAt = &now
		f.delivery.EXPECT().ArchiveEndpoint(gomock.Any(), adaActor, int64(3)).Return(&archived, int64(2), nil)
		rec := f.do(http.MethodDelete, "/v1/endpoints/3", "")
		require.Equal(t, http.StatusOK, rec.Code)
		var got openapi.EndpointArchived
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		assert.Equal(t, int64(2), got.CancelledDeliveries)
		assert.NotNil(t, got.Endpoint.ArchivedAt)
	})

	t.Run("create body carries the secret once", func(t *testing.T) {
		f := newFixture(t)
		f.delivery.EXPECT().CreateEndpoint(gomock.Any(), adaActor, services.CreateEndpointInput{ApplicationID: 7, URL: endpoint.URL}).Return(endpoint, "whsec_abc", int32(1), nil)
		rec := f.do(http.MethodPost, "/v1/applications/7/endpoints", `{"url":"https://receiver.example.com/hooks"}`)
		require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
		var created openapi.EndpointCreated
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
		assert.Equal(t, "whsec_abc", created.SigningSecret)
		assert.Equal(t, int32(1), created.SecretVersion)
		assert.Equal(t, int64(3), created.Endpoint.Id)
		assert.Equal(t, []string{"order.shipped"}, created.Endpoint.EventTypes)
		assert.Nil(t, created.Endpoint.PausedAt)
	})
}
