# PaperclipPVCNearlyFull

**Severity:** warning

The data PVC of the `Instance` is more than 80% full.

## Diagnose

```bash
kubectl -n "$NS" get pvc | grep "$NAME"
kubectl -n "$NS" exec <pod> -c paperclip -- df -h /paperclip
```

## Resolve

- If the StorageClass allows volume expansion, raise
  `spec.storage.persistence.size`, or patch the PVC's
  `spec.resources.requests.storage` directly.
- Otherwise free space (old run logs or workspace data under `/paperclip`), or
  move file storage to S3-compatible object storage with `spec.objectStorage`.
