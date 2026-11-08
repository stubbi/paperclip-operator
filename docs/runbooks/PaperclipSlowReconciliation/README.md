# PaperclipSlowReconciliation

**Severity:** warning

The p99 reconcile duration for the `Instance` exceeds 30 seconds.

## Diagnose

Slow reconciles usually mean the Kubernetes API server is slow or throttling
the operator, or an external call made during reconcile (for example the
Paperclip API for self-configuration) is slow to respond.

```bash
kubectl -n paperclip-operator-system logs deploy/paperclip-operator --since=30m | grep -i "throttl\|timeout\|deadline"
kubectl -n paperclip-operator-system top pod
```

## Resolve

- Raise the operator's CPU/memory limits if it is being throttled.
- Check API server health and latency.
- If an external endpoint is slow, check its availability from the operator
  namespace (NetworkPolicies, DNS).
