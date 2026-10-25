# PaperclipInstanceDegraded

**Severity:** critical

The `Instance` has been in the `Failed` or `Degraded` phase for 5 minutes.

## Diagnose

Set `NS` and `NAME` to the namespace and name of the affected `Instance`.

```bash
kubectl -n "$NS" get instance "$NAME" -o yaml          # spec + status.conditions
kubectl -n "$NS" get events --sort-by=.lastTimestamp | tail -30
kubectl -n paperclip-operator-system logs deploy/paperclip-operator --since=30m | grep "$NAME"
```

`status.phase` and `status.conditions` name the component that is unhealthy
(server pods, managed database, backup, bootstrap Job). Inspect the matching
resources:

```bash
kubectl -n "$NS" get statefulset,deploy,job,pvc -l app.kubernetes.io/instance="$NAME"
```

## Resolve

Fix the failing component. The phase returns to `Running` on the next
successful reconcile.
