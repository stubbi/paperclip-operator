# PaperclipInstanceNotReady

**Severity:** critical

The `Instance` has reported `Ready=false` for 10 minutes.

## Diagnose

Set `NS` and `NAME` to the namespace and name of the affected `Instance`.

```bash
kubectl -n "$NS" get instance "$NAME" -o yaml          # spec + status.conditions
kubectl -n "$NS" get events --sort-by=.lastTimestamp | tail -30
kubectl -n paperclip-operator-system logs deploy/paperclip-operator --since=30m | grep "$NAME"
```

```bash
kubectl -n "$NS" get pods -l app.kubernetes.io/instance="$NAME"
kubectl -n "$NS" describe pod <pod>
kubectl -n "$NS" logs <pod> -c paperclip --tail=200
```

Check `status.conditions` for the condition that is not `True`. Typical causes
are pods stuck in `Pending` (insufficient resources, unbound PVC), image pull
errors, a failing database connection, or a failing readiness probe on
`GET /api/health`.

## Resolve

Address the failing condition. See also
[PaperclipPodCrashLooping](../PaperclipPodCrashLooping/README.md) if the
pods restart repeatedly.
