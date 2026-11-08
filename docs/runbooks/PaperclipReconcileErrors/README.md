# PaperclipReconcileErrors

**Severity:** warning

The operator failed to reconcile the `Instance` at least once in the last 5 minutes.

## Diagnose

Set `NS` and `NAME` to the namespace and name of the affected `Instance`.

```bash
kubectl -n "$NS" get instance "$NAME" -o yaml          # spec + status.conditions
kubectl -n "$NS" get events --sort-by=.lastTimestamp | tail -30
kubectl -n paperclip-operator-system logs deploy/paperclip-operator --since=30m | grep "$NAME"
```

The operator logs show the failing step (for example creating a StatefulSet,
Secret, Ingress or PrometheusRule). Common causes:

- A referenced Secret or ConfigMap does not exist, or lacks the expected key.
- RBAC: the operator's ClusterRole is missing a permission (for example after
  upgrading the operator image without upgrading the chart).
- An admission webhook or policy engine (Kyverno, Gatekeeper) rejects a
  managed object.
- An optional CRD (ServiceMonitor, PrometheusRule, HTTPRoute) is referenced in
  the spec but not installed.

## Resolve

Fix the root cause shown in the logs. The operator retries automatically; the
alert clears once reconciles succeed again.
