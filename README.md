# Paperclip Kubernetes Operator

[![CI](https://github.com/paperclipinc/paperclip-operator/actions/workflows/ci.yaml/badge.svg)](https://github.com/paperclipinc/paperclip-operator/actions/workflows/ci.yaml)
[![Go Report Card](https://goreportcard.com/badge/github.com/paperclipinc/paperclip-operator)](https://goreportcard.com/report/github.com/paperclipinc/paperclip-operator)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

A Kubernetes operator for deploying and managing [Paperclip](https://github.com/paperclipai/paperclip) instances, the open-source AI agent orchestration platform.

## Overview

The Paperclip Operator automates the full lifecycle of Paperclip instances on Kubernetes. Define your desired state in a single `Instance` custom resource, and the operator handles provisioning, configuration, scaling, and day-2 operations.

**What it manages:**

- Paperclip server as a StatefulSet with configurable replicas, health probes, and persistent storage
- PostgreSQL database (operator-managed, external, or embedded PGlite)
- Networking: Service, Ingress with WebSocket support, NetworkPolicy
- Security: Restricted pod security, RBAC, network isolation
- Scaling: HorizontalPodAutoscaler, PodDisruptionBudget, topology spread
- Secrets: auto-generated database credentials, LLM API key injection, encrypted secrets management

## Quick Start

### Prerequisites

- Kubernetes 1.28+
- Helm 3.x (recommended) or kubectl

### Install the operator

```bash
# Via Helm (recommended)
helm install paperclip-operator oci://ghcr.io/paperclipinc/charts/paperclip-operator

# Or via kubectl
kubectl apply -f https://github.com/paperclipinc/paperclip-operator/releases/latest/download/install.yaml
```

### Create required secrets

```bash
# Auth secret (required for authenticated mode)
kubectl create secret generic paperclip-auth \
  --from-literal=BETTER_AUTH_SECRET="$(openssl rand -hex 32)"

# LLM API keys (optional)
kubectl create secret generic paperclip-api-keys \
  --from-literal=ANTHROPIC_API_KEY="sk-ant-..." \
  --from-literal=OPENAI_API_KEY="sk-..."
```

### Deploy a Paperclip instance

```yaml
apiVersion: paperclip.inc/v1alpha1
kind: Instance
metadata:
  name: my-paperclip
spec:
  image:
    tag: latest
  deployment:
    mode: authenticated
  database:
    mode: managed
  auth:
    secretRef:
      name: paperclip-auth
      key: BETTER_AUTH_SECRET
  adapters:
    apiKeysSecretRef:
      name: paperclip-api-keys
  storage:
    persistence:
      enabled: true
      size: 5Gi
```

```bash
kubectl apply -f my-paperclip.yaml
```

### Check status

```bash
kubectl get instances
# or use the shorthand:
kubectl get pci
```

```
NAME           PHASE     ENDPOINT                                          AGE
my-paperclip   Running   http://my-paperclip.default.svc.cluster.local:3100   5m
```

## Features

| Feature | Description |
|---------|-------------|
| **Managed PostgreSQL** | Provisions PostgreSQL 17 with auto-generated credentials, data checksums, and graceful shutdown |
| **External Database** | Connect to existing PostgreSQL via connection string or Secret reference |
| **Horizontal Scaling** | Configurable replicas with automatic heartbeat leader election (only pod-0 runs the scheduler) |
| **Auto-scaling** | HPA with CPU/memory utilization targets |
| **Smart Health Probes** | Automatically uses TCP probes in authenticated mode (where `/api/health` requires credentials) |
| **Ingress + WebSocket** | Full Ingress support with TLS and WebSocket annotations |
| **Network Policies** | Deny-all baseline with selective allow rules for the app and database |
| **Pod Disruption Budget** | Availability protection during node maintenance |
| **LLM API Keys** | Inject Anthropic, OpenAI, and other provider keys from Kubernetes Secrets |
| **Secrets Management** | Paperclip's built-in encrypted secrets with master key support |
| **S3 Object Storage** | S3, MinIO, or R2 for multi-replica file storage |
| **Backup and Restore** | Scheduled backups to S3 with configurable retention |
| **Custom Sidecars** | Add sidecar containers, init containers, extra volumes and volume mounts |
| **Observability** | Prometheus metrics, ServiceMonitor, configurable log levels |

## Configuration Reference

See [config/samples/](config/samples/) for complete examples.

### Database Modes

| Mode | Use Case |
|------|----------|
| `managed` (default) | Operator provisions PostgreSQL 17 as a StatefulSet with PVC and credentials. Suitable for development and small deployments. |
| `external` | Connect to an existing PostgreSQL instance. Recommended for production HA deployments (e.g., Amazon RDS, Cloud SQL, Azure Database). |
| `embedded` | Uses PGlite (in-process SQLite-compatible storage). Single-node only, good for local development. |

### Deployment Modes

| Mode | Description |
|------|-------------|
| `authenticated` (default) | Login required via Better Auth. Requires `BETTER_AUTH_SECRET`. |
| `open` | No authentication. Only works with loopback binding (`HOST=127.0.0.1`). |
| `single-tenant` | Single-user mode with authentication. |

### Scaling

```yaml
spec:
  availability:
    replicas: 3
    autoScaling:
      enabled: true
      minReplicas: 2
      maxReplicas: 10
      targetCPUUtilizationPercentage: 70
    podDisruptionBudget:
      enabled: true
      minAvailable: 1
```

When running multiple replicas, the operator ensures only the first pod (ordinal 0) runs the heartbeat scheduler. For multi-replica deployments, use `database.mode: external` with a production-grade PostgreSQL service and configure S3 object storage for shared file access.

### Networking

```yaml
spec:
  networking:
    service:
      type: ClusterIP
      port: 3100
    ingress:
      enabled: true
      ingressClassName: nginx
      hosts:
        - paperclip.example.com
      tls:
        - hosts:
            - paperclip.example.com
          secretName: paperclip-tls
      annotations:
        nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"
        nginx.ingress.kubernetes.io/proxy-send-timeout: "3600"
```

### Full CRD Specification

For the complete list of configurable fields, see the [Instance CRD types](api/v1alpha1/paperclipinstance_types.go) or run:

```bash
kubectl explain instance.spec
```

## Architecture

```
api/v1alpha1/          CRD types (Instance)
internal/controller/   Reconciliation logic (single controller)
internal/resources/    Pure resource builder functions (StatefulSet, Service, etc.)
config/crd/bases/      Generated CRD YAML
charts/                Helm chart
```

The operator follows a clean separation of concerns: the controller orchestrates reconciliation, while all Kubernetes resource construction happens in pure functions inside `internal/resources/`. This makes builders easy to unit test without envtest.

## Development

### Prerequisites

- Go 1.24+
- Docker
- kubectl
- A Kubernetes cluster (Kind, minikube, or remote)

### Build and run locally

```bash
make install      # Install CRDs into current cluster
make run          # Run operator locally against current kubeconfig
```

### Run tests

```bash
make test                              # Unit + integration tests (envtest)
go test ./internal/resources/ -v       # Fast unit tests (no envtest needed)
make bench                             # Benchmarks for resource builders
```

### Build Docker image

```bash
make docker-build IMG=my-registry/paperclip-operator:dev
```

### After changing CRD types

```bash
make generate          # Regenerate deepcopy methods
make manifests         # Regenerate CRD YAML and RBAC
make sync-chart-crds   # Sync CRDs into Helm chart
```

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feat/my-feature`)
3. Commit using [conventional commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`, `docs:`, etc.)
4. Push and open a pull request

All PRs require passing CI checks (lint, test, security scan, E2E) and one approval.

## License

[Apache License 2.0](LICENSE)
