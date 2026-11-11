package deliveryclient

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bete7512/hookbeam/api/models"
	deliveryv1 "github.com/bete7512/hookbeam/contracts/gen/go/hookbeam/delivery/v1"
)

func toApplication(a *deliveryv1.Application) *models.Application {
	return &models.Application{ID: a.GetId(), WorkspaceID: a.GetWorkspaceId(), EnvironmentID: a.GetEnvironmentId(), EnvironmentKind: a.GetEnvironmentKind(), Name: a.GetName(), CreatedBy: a.GetCreatedBy(), ResponsePreviewsEnabled: a.GetResponsePreviewsEnabled(), CreatedAt: a.GetCreatedAt().AsTime(), ArchivedAt: optionalTime(a.ArchivedAt)}
}

func toEnvironment(e *deliveryv1.Environment) *models.Environment {
	return &models.Environment{ID: e.GetId(), WorkspaceID: e.GetWorkspaceId(), Name: e.GetName(), Kind: e.GetKind(), CreatedBy: e.GetCreatedBy(), CreatedAt: e.GetCreatedAt().AsTime()}
}

func toEndpoint(e *deliveryv1.Endpoint) *models.Endpoint {
	types := e.GetEventTypes()
	if types == nil {
		types = []string{}
	}
	health := models.EndpointHealthStatus{State: "unknown"}
	if h := e.GetHealth(); h != nil {
		health = models.EndpointHealthStatus{State: h.GetState(), ObservedAt: optionalTime(h.ObservedAt), Stale: h.GetStale(), ConsecutiveFailures: h.GetConsecutiveFailures()}
	}
	out := &models.Endpoint{
		ID: e.GetId(), ApplicationID: e.GetApplicationId(), URL: e.GetUrl(), Description: e.GetDescription(), Revision: e.GetRevision(),
		EventTypes: types, PausedAt: optionalTime(e.PausedAt), ArchivedAt: optionalTime(e.ArchivedAt), CreatedAt: e.GetCreatedAt().AsTime(), Health: health, Waiting: e.Waiting,
	}
	if e.Secrets != nil {
		out.Secrets = make([]models.EndpointSecretInfo, 0, len(e.Secrets))
		for _, sec := range e.Secrets {
			out.Secrets = append(out.Secrets, models.EndpointSecretInfo{Version: sec.GetVersion(), Fingerprint: sec.GetFingerprint(), CreatedAt: sec.GetCreatedAt().AsTime(), RetireAt: optionalTime(sec.RetireAt)})
		}
	}
	return out
}

func toMessage(m *deliveryv1.Message) *models.Message {
	return &models.Message{ID: m.GetId(), WebhookID: m.GetPublicId(), ApplicationID: m.GetApplicationId(), EnvironmentID: m.GetEnvironmentId(), EnvironmentKind: m.GetEnvironmentKind(), Type: m.GetEventType(), ExternalID: m.SenderEventId, AcceptedAt: m.GetAcceptedAt().AsTime(), DeliveryCount: m.GetDeliveryCount(), PayloadExpiresAt: optionalTime(m.PayloadExpiresAt)}
}

var deliveryStates = map[deliveryv1.DeliveryState]string{
	deliveryv1.DeliveryState_DELIVERY_STATE_QUEUED:          "queued",
	deliveryv1.DeliveryState_DELIVERY_STATE_IN_FLIGHT:       "in_flight",
	deliveryv1.DeliveryState_DELIVERY_STATE_RETRY_SCHEDULED: "retry_scheduled",
	deliveryv1.DeliveryState_DELIVERY_STATE_SUCCEEDED:       "succeeded",
	deliveryv1.DeliveryState_DELIVERY_STATE_FAILED:          "failed",
	deliveryv1.DeliveryState_DELIVERY_STATE_CANCELLED:       "cancelled",
}

func toDelivery(d *deliveryv1.Delivery) models.Delivery {
	return models.Delivery{
		ID: d.GetId(), MessageID: d.GetMessageId(), EndpointID: d.GetEndpointId(), State: deliveryStates[d.GetState()], AttemptCount: d.GetAttemptCount(), EndpointRevision: d.GetEndpointRevision(),
		DueAt: optionalTime(d.DueAt), DeadlineAt: d.GetDeadlineAt().AsTime(), FailureReason: d.FailureReason, ReplayOfDeliveryID: d.ReplayOfDeliveryId,
		CreatedAt: d.GetCreatedAt().AsTime(), CompletedAt: optionalTime(d.CompletedAt),
	}
}

func toAttempt(a *deliveryv1.Attempt) models.Attempt {
	return models.Attempt{
		ID: a.GetId(), DeliveryID: a.GetDeliveryId(), Number: a.GetNumber(), StartedAt: a.GetStartedAt().AsTime(), CompletedAt: optionalTime(a.CompletedAt),
		StatusCode: a.StatusCode, ErrorClass: a.ErrorClass, LatencyMS: a.GetLatencyMs(), SecretVersion: a.GetSecretVersion(), EndpointRevision: a.GetEndpointRevision(), RequestURL: a.GetRequestUrl(),
		ResponsePreview: a.ResponsePreview, ResponseHeaders: a.GetResponseHeaders(),
	}
}

func toOverview(o *deliveryv1.Overview) *models.Overview {
	series := make([]models.OverviewBucket, 0, len(o.GetSeries()))
	for _, b := range o.GetSeries() {
		series = append(series, models.OverviewBucket{Hour: b.GetHour().AsTime(), Accepted: b.GetAccepted(), Succeeded: b.GetSucceeded(), Failed: b.GetFailed()})
	}
	return &models.Overview{
		Since: o.GetSince().AsTime(), ComputedAt: o.GetComputedAt().AsTime(), Accepted: o.GetAccepted(), Succeeded: o.GetSucceeded(), Exhausted: o.GetExhausted(), Backlog: o.GetBacklog(),
		OldestPendingDueAt: optionalTime(o.OldestPendingDueAt), EndpointsHealthy: o.GetEndpointsHealthy(), EndpointsUnhealthy: o.GetEndpointsUnhealthy(), EndpointsUnknown: o.GetEndpointsUnknown(), HealthObservedAt: optionalTime(o.HealthObservedAt),
		Series: series,
	}
}

func toWorkspaceLimits(resp *deliveryv1.GetWorkspaceLimitsResponse) *models.WorkspaceLimits {
	l := resp.GetLimits()
	out := &models.WorkspaceLimits{
		EndpointsPerApplication: int(l.GetEndpointsPerApplication()), FanoutPerMessage: int(l.GetFanoutPerMessage()), BacklogPerWorkspace: int(l.GetBacklogPerWorkspace()),
		Backlog: int(resp.GetBacklog()), Applications: make([]models.ApplicationEndpointCount, 0, len(resp.GetApplications())),
		Retention: models.Retention{Payload: resp.GetRetention().GetPayload().AsDuration(), IdempotencyWindow: resp.GetRetention().GetIdempotencyWindow().AsDuration(), Audit: resp.GetRetention().GetAudit().AsDuration()},
	}
	for _, a := range resp.GetApplications() {
		out.Applications = append(out.Applications, models.ApplicationEndpointCount{ApplicationID: a.GetApplicationId(), Name: a.GetName(), Endpoints: a.GetEndpoints()})
	}
	return out
}

func toHealthEpisode(e *deliveryv1.HealthEpisode) *models.HealthEpisode {
	return &models.HealthEpisode{ID: e.GetId(), EndpointID: e.GetEndpointId(), Revision: e.GetRevision(), ConsecutiveFailures: e.GetConsecutiveFailures(), OpenedAt: e.GetOpenedAt().AsTime(), ResolvedAt: optionalTime(e.ResolvedAt)}
}

func toMessageSummary(m *deliveryv1.MessageSummary) models.MessageSummary {
	c := m.GetCounts()
	return models.MessageSummary{
		Message:          *toMessage(m.GetMessage()),
		Counts:           models.DeliveryCounts{Queued: c.GetQueued(), InFlight: c.GetInFlight(), RetryScheduled: c.GetRetryScheduled(), Succeeded: c.GetSucceeded(), Failed: c.GetFailed(), Cancelled: c.GetCancelled()},
		PayloadExpiresAt: m.GetPayloadExpiresAt().AsTime(),
	}
}

func toAuditEvent(e *deliveryv1.AuditEvent) models.AuditEvent {
	return models.AuditEvent{ID: e.GetId(), WorkspaceID: e.GetWorkspaceId(), ActorID: e.GetActorId(), Action: e.GetAction(), ResourceType: e.GetResourceType(), ResourceID: e.ResourceId, RequestID: e.GetRequestId(), Details: e.GetDetails(), OccurredAt: e.GetOccurredAt().AsTime()}
}

func optionalTime(t *timestamppb.Timestamp) *time.Time {
	if t == nil {
		return nil
	}
	v := t.AsTime()
	return &v
}
