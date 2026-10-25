# PaperclipPodCrashLooping

**Severity:** critical

A Paperclip server container restarted more than twice in 10 minutes.

## Diagnose

```bash
kubectl -n "$NS" get pods -l app.kubernetes.io/instance="$NAME"
kubectl -n "$NS" logs <pod> -c paperclip --previous --tail=200
kubectl -n "$NS" describe pod <pod>
```

Common causes: invalid configuration or secrets (for example a malformed
database connection string), the database being unreachable, missing
environment variables after a spec change, or the container being OOM killed
(see [PaperclipPodOOMKilled](../PaperclipPodOOMKilled/README.md)).

## Resolve

Fix the configuration shown in the previous container's logs. If a recent
image update caused the crash, pin `spec.image.tag` to the last working
version.
