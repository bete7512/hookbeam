//go:build security

package services_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/bete7512/hookbeam/delivery/models"
	"github.com/bete7512/hookbeam/delivery/repos/repo_mocks"
	"github.com/bete7512/hookbeam/delivery/services"
	"github.com/bete7512/hookbeam/delivery/services/service_mocks"
	"github.com/bete7512/hookbeam/pkg/apperr"
	"github.com/bete7512/hookbeam/pkg/safehttp"
	"github.com/bete7512/hookbeam/pkg/secrets"
)

// TestSecurityCustomHeaderValuesNeverLeak: a custom header value is a credential for the receiver's system. It is
// stored encrypted, and no response, audit row or validation message ever carries it; names that would replace
// what Hookbeam sends and values that would inject a header are refused.
func TestSecurityCustomHeaderValuesNeverLeak(t *testing.T) {
	const canary = "CANARY-sec-hdr-7e11b0"
	ctrl := gomock.NewController(t)
	apps := repo_mocks.NewMockApplicationRepo(ctrl)
	apps.EXPECT().GetApplication(gomock.Any(), ws, int64(7)).Return(&models.Application{ID: 7, WorkspaceID: ws}, nil).AnyTimes()
	endpoints := repo_mocks.NewMockEndpointRepo(ctrl)
	endpoints.EXPECT().WithTx(gomock.Any()).Return(endpoints).AnyTimes()
	endpoints.EXPECT().CountActiveEndpoints(gomock.Any(), int64(7)).Return(0, nil).AnyTimes()
	deliveries := repo_mocks.NewMockDeliveryRepo(ctrl)
	deliveries.EXPECT().WithTx(gomock.Any()).Return(deliveries).AnyTimes()
	deliveries.EXPECT().CountWaitingForEndpoint(gomock.Any(), gomock.Any()).Return(int32(0), nil).AnyTimes()
	var audited strings.Builder
	audit := repo_mocks.NewMockAuditRepo(ctrl)
	audit.EXPECT().WithTx(gomock.Any()).Return(audit).AnyTimes()
	audit.EXPECT().CreateAuditEvent(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, e *models.AuditEvent) error {
		audited.Write(e.Details)
		return nil
	}).AnyTimes()
	secretRepo := repo_mocks.NewMockEndpointSecretRepo(ctrl)
	secretRepo.EXPECT().WithTx(gomock.Any()).Return(secretRepo).AnyTimes()
	secretRepo.EXPECT().CreateEndpointSecret(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	secretRepo.EXPECT().ListEndpointSecrets(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	subs := repo_mocks.NewMockSubscriptionRepo(ctrl)
	subs.EXPECT().WithTx(gomock.Any()).Return(subs).AnyTimes()
	subs.EXPECT().CreateSubscriptions(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	subs.EXPECT().ListEventTypes(gomock.Any(), gomock.Any()).Return([]string{}, nil).AnyTimes()
	health := service_mocks.NewMockHealthService(ctrl)
	health.EXPECT().Status(gomock.Any(), gomock.Any(), gomock.Any()).Return(&models.HealthStatus{State: models.HealthUnknown}, nil).AnyTimes()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	cipher, err := secrets.New(base64.StdEncoding.EncodeToString(key))
	require.NoError(t, err)
	svc, err := services.NewEndpointService(services.EndpointServiceDeps{
		Tx:            fakeTx{},
		Applications:  apps,
		Endpoints:     endpoints,
		Deliveries:    deliveries,
		Audit:         audit,
		Secrets:       secretRepo,
		Subscriptions: subs,
		Health:        health,
		Limits:        testLimits,
		Cipher:        cipher,
		KeyID:         "2026-11",
		URL:           safehttp.Config{AllowedPorts: []int{443}},
		Logger:        slog.New(slog.DiscardHandler),
	})
	require.NoError(t, err)
	ctx := context.Background()

	// stored is the row as the repo holds it: what a database dump would show.
	var stored models.Endpoint
	endpoints.EXPECT().CreateEndpoint(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, e *models.Endpoint) error {
		e.ID, e.Revision = 3, 1
		stored = *e
		return nil
	})
	created, _, _, err := svc.CreateEndpoint(ctx, scope, services.CreateEndpointInput{
		ApplicationID: 7,
		URL:           "https://receiver.example.com/hooks",
		Headers:       map[string]string{"Authorization": "Bearer " + canary},
	})
	require.NoError(t, err)
	require.NotEmpty(t, stored.HeadersCiphertext)
	require.NotContains(t, string(stored.HeadersCiphertext), canary, "encrypted at rest")

	endpoints.EXPECT().GetEndpoint(gomock.Any(), ws, int64(3)).DoAndReturn(func(context.Context, string, int64) (*models.Endpoint, error) {
		row := stored
		return &row, nil
	}).AnyTimes()
	endpoints.EXPECT().UpdateEndpoint(gomock.Any(), ws, int64(3), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, string, int64, string, string, *int32) (*models.Endpoint, error) {
		stored.Revision++
		row := stored
		return &row, nil
	}).AnyTimes()
	endpoints.EXPECT().SetEndpointHeaders(gomock.Any(), ws, int64(3), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, _ string, _ int64, ciphertext []byte, keyID string) error {
		stored.HeadersCiphertext, stored.HeadersKeyID = ciphertext, keyID
		return nil
	}).AnyTimes()
	updated, err := svc.UpdateEndpoint(ctx, scope, services.UpdateEndpointInput{EndpointID: 3, SetHeaders: map[string]string{"X-Api-Key": canary}})
	require.NoError(t, err)
	require.NotContains(t, string(stored.HeadersCiphertext), canary, "still encrypted after a merge")
	read, err := svc.GetEndpoint(ctx, scope, 3)
	require.NoError(t, err)

	for name, e := range map[string]*models.Endpoint{"create": created, "update": updated, "read": read} {
		require.Equal(t, "authorization", e.HeaderNames[0], name)
		// The ciphertext is a service-internal field that no mapping copies out; everything else must be clean.
		public := *e
		public.HeadersCiphertext = nil
		body, err := json.Marshal(public)
		require.NoError(t, err)
		require.NotContains(t, string(body), canary, "the %s answer", name)
	}
	require.Equal(t, []string{"authorization", "x-api-key"}, read.HeaderNames, "names are what a reader gets")
	require.Contains(t, audited.String(), `"header_names"`)
	require.NotContains(t, audited.String(), canary, "the audit trail names headers, never their values")

	refused := []struct {
		name   string
		header string
		value  string
	}{
		{name: "CR LF in a value would inject a second header", header: "x-api-key", value: canary + "\r\nwebhook-signature: v1,forged"},
		{name: "a bare LF in a value", header: "x-api-key", value: canary + "\n"},
		{name: "the signature", header: "Webhook-Signature", value: canary},
		{name: "the message id", header: "webhook-id", value: canary},
		{name: "the timestamp", header: "WEBHOOK-TIMESTAMP", value: canary},
		{name: "the content type", header: "Content-Type", value: canary},
		{name: "the host", header: "Host", value: canary},
		{name: "a hop-by-hop header", header: "Transfer-Encoding", value: canary},
		{name: "a proxy credential", header: "Proxy-Authorization", value: canary},
		{name: "a name with CR LF", header: "x-a\r\nx-b", value: canary},
	}
	before := audited.Len()
	for _, tt := range refused {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := svc.CreateEndpoint(ctx, scope, services.CreateEndpointInput{ApplicationID: 7, URL: "https://receiver.example.com/hooks", Headers: map[string]string{tt.header: tt.value}})
			var appErr *apperr.AppError
			require.ErrorAs(t, err, &appErr)
			require.Equal(t, 422, appErr.Code)
			require.Contains(t, appErr.Fields, "headers")
			require.NotContains(t, appErr.Error()+appErr.Fields["headers"], canary, "a refusal names the header, never its value")

			_, err = svc.UpdateEndpoint(ctx, scope, services.UpdateEndpointInput{EndpointID: 3, SetHeaders: map[string]string{tt.header: tt.value}})
			require.ErrorAs(t, err, &appErr)
			require.Equal(t, 422, appErr.Code)
			require.NotContains(t, appErr.Error()+appErr.Fields["headers"], canary)
		})
	}
	require.Equal(t, before, audited.Len(), "a refused header writes no audit row")
}
