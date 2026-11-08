# PaperclipPodOOMKilled

**Severity:** warning

A Paperclip server container was terminated for exceeding its memory limit.

## Diagnose

```bash
kubectl -n "$NS" describe pod <pod> | grep -A3 "Last State"
kubectl -n "$NS" top pod -l app.kubernetes.io/instance="$NAME"
```

## Resolve

Raise the memory limit in `spec.resources.limits.memory` of the `Instance`
(and the request, if the node has headroom). The operator rolls the pods with
the new limits.
