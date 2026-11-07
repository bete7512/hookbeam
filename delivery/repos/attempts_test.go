//go:build integration

package repos_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/bete7512/hookbeam/delivery/models"
)

type AttemptRepoTestSuite struct {
	suite.Suite
	f *deliveryFixture
}

func TestAttemptRepoTestSuite(t *testing.T) {
	suite.Run(t, new(AttemptRepoTestSuite))
}

func (s *AttemptRepoTestSuite) SetupSuite()   { s.f = newDeliveryFixture(&s.Suite) }
func (s *AttemptRepoTestSuite) SetupSubTest() { s.f.truncate(&s.Suite) }

func (s *AttemptRepoTestSuite) TestCreateAttempt() {
	s.Run("records and lists in number order", func() {
		app, e := s.f.seedEndpoint(&s.Suite, "Customer A")
		m := s.f.seedMessage(&s.Suite, app.ID, "k1")
		d := s.f.seedDelivery(&s.Suite, m.ID, e.ID, time.Now())
		status := int32(503)
		preview := "try later"
		done := time.Now()
		second := &models.Attempt{DeliveryID: d.ID, Number: 2, EndpointRevision: 1, SecretVersion: 1, RequestURL: e.URL, StartedAt: time.Now(), CompletedAt: &done, LatencyMS: 12, StatusCode: &status, ResponseHeaders: map[string]string{"retry-after": "30"}, ResponsePreview: &preview}
		class := "timeout"
		first := &models.Attempt{DeliveryID: d.ID, Number: 1, EndpointRevision: 1, SecretVersion: 1, RequestURL: e.URL, StartedAt: time.Now(), LatencyMS: 10000, ErrorClass: &class}
		s.Require().NoError(s.f.attempts.CreateAttempt(context.Background(), second))
		s.Require().NoError(s.f.attempts.CreateAttempt(context.Background(), first))
		s.Positive(first.ID)
		s.Empty(first.ResponseHeaders)

		got, err := s.f.attempts.ListAttempts(context.Background(), d.ID)
		s.Require().NoError(err)
		s.Require().Len(got, 2)
		s.Equal(int32(1), got[0].Number)
		s.Equal("timeout", *got[0].ErrorClass)
		s.Equal(int32(503), *got[1].StatusCode)
		s.Equal("30", got[1].ResponseHeaders["retry-after"])
		s.Equal("try later", *got[1].ResponsePreview)
	})
	s.Run("attempt numbers are unique per delivery", func() {
		app, e := s.f.seedEndpoint(&s.Suite, "Customer A")
		m := s.f.seedMessage(&s.Suite, app.ID, "k1")
		d := s.f.seedDelivery(&s.Suite, m.ID, e.ID, time.Now())
		a := &models.Attempt{DeliveryID: d.ID, Number: 1, EndpointRevision: 1, SecretVersion: 1, RequestURL: e.URL, StartedAt: time.Now()}
		s.Require().NoError(s.f.attempts.CreateAttempt(context.Background(), a))
		b := *a
		s.Error(s.f.attempts.CreateAttempt(context.Background(), &b))
	})
}

func (s *AttemptRepoTestSuite) TestEndpointLookupsForTheWorker() {
	s.Run("subscribed endpoints and active secrets", func() {
		ctx := context.Background()
		app, shipped := s.f.seedEndpoint(&s.Suite, "Customer A", "order.shipped")
		all := &models.Endpoint{WorkspaceID: ws, ApplicationID: app.ID, URL: "https://all.example.com/", CreatedBy: actor}
		s.Require().NoError(s.f.endpoints.CreateEndpoint(ctx, all))
		created := &models.Endpoint{WorkspaceID: ws, ApplicationID: app.ID, URL: "https://created.example.com/", CreatedBy: actor}
		s.Require().NoError(s.f.endpoints.CreateEndpoint(ctx, created))
		s.Require().NoError(s.f.subs.CreateSubscriptions(ctx, created.ID, []string{"order.created"}))
		archived := &models.Endpoint{WorkspaceID: ws, ApplicationID: app.ID, URL: "https://archived.example.com/", CreatedBy: actor}
		s.Require().NoError(s.f.endpoints.CreateEndpoint(ctx, archived))
		_, err := s.f.db.Exec(ctx, `UPDATE endpoints SET archived_at = now() WHERE id = $1`, archived.ID)
		s.Require().NoError(err)

		got, err := s.f.endpoints.ListSubscribedEndpoints(ctx, app.ID, "order.shipped")
		s.Require().NoError(err)
		ids := []int64{}
		for _, e := range got {
			ids = append(ids, e.ID)
		}
		s.Equal([]int64{shipped.ID, all.ID}, ids, "the subscribed one and the catch-all, not the other type or the archived")

		byID, err := s.f.endpoints.GetEndpointByID(ctx, shipped.ID)
		s.Require().NoError(err)
		s.Equal(shipped.URL, byID.URL)

		for range 2 {
			s.Require().NoError(s.f.secrets.CreateEndpointSecret(ctx, &models.EndpointSecret{EndpointID: shipped.ID, Ciphertext: []byte("ct"), KeyID: "k", Fingerprint: "f"}))
		}
		_, err = s.f.db.Exec(ctx, `UPDATE endpoint_secrets SET retire_at = now() - interval '1 second' WHERE endpoint_id = $1 AND version = 1`, shipped.ID)
		s.Require().NoError(err)
		secrets, err := s.f.secrets.ListActiveEndpointSecrets(ctx, shipped.ID)
		s.Require().NoError(err)
		s.Require().Len(secrets, 1)
		s.Equal(int32(2), secrets[0].Version)
	})
}
