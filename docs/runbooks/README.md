# Runbooks

One file per alert rule provisioned in Grafana (`deploy/observability/grafana/provisioning/alerting/rules.yaml`).
Each names the symptom, the query and the dashboard panel to confirm it, the safe action, and the escalation.

- [Outbox lag](outbox-lag.md)
- [Oldest due age](oldest-due-age.md)
- [Repeated failures](repeated-failures.md)
- [Stalled workers](stalled-workers.md)
- [Notification pipeline failing](notification-lag.md)

`make obs-faults` makes them fire on the local stack: it stops the delivery worker, fails deliveries at the demo
receiver, then stops LocalStack and the control-plane worker, and polls Grafana's alert API for each rule.
