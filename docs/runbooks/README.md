# Alert runbooks

Runbooks for the alerts in the PrometheusRule the operator creates when
`spec.observability.metrics.prometheusRule` is enabled. Each alert's `runbook_url` annotation
points to the matching page. To host the runbooks somewhere else, set
`spec.observability.metrics.prometheusRule.runbookBaseURL`; the operator appends `/<AlertName>` to it.

| Alert | Severity |
| --- | --- |
| [PaperclipReconcileErrors](PaperclipReconcileErrors/README.md) | warning |
| [PaperclipInstanceNotReady](PaperclipInstanceNotReady/README.md) | critical |
| [PaperclipInstanceDegraded](PaperclipInstanceDegraded/README.md) | critical |
| [PaperclipSlowReconciliation](PaperclipSlowReconciliation/README.md) | warning |
| [PaperclipPodCrashLooping](PaperclipPodCrashLooping/README.md) | critical |
| [PaperclipPodOOMKilled](PaperclipPodOOMKilled/README.md) | warning |
| [PaperclipPVCNearlyFull](PaperclipPVCNearlyFull/README.md) | warning |
