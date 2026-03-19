# CLAUDE.md -- Paperclip Kubernetes Operator

## Project Overview

Go-based Kubernetes operator for managing Paperclip instances (the open-source AI agent orchestration platform), built with controller-runtime (kubebuilder). CRD API group is `paperclip.ai`, version `v1alpha1`.

- **Module:** `github.com/paperclipai/k8s-operator`
- **Go version:** 1.25
- **GitHub:** `paperclipai/k8s-operator` (GHCR org: `paperclipai`)

## Commands

```bash
make test          # Unit + integration tests (requires envtest binaries)
make lint          # golangci-lint
make build         # Build manager binary
make manifests     # Regenerate CRD YAML + RBAC after API type changes
make generate      # Regenerate deepcopy methods after API type changes
make install       # Install CRDs into current cluster
make run           # Run operator locally against current cluster
go test ./internal/resources/ -v   # Fast unit tests (no envtest needed)
go vet ./...       # Go vet check
```

## Architecture

```
api/v1alpha1/          -> CRD types (PaperclipInstance)
internal/controller/   -> Reconciliation logic (single controller)
internal/resources/    -> Pure resource builder functions (StatefulSet, Service, etc.)
config/crd/bases/      -> Generated CRD YAML (committed to git)
charts/                -> Helm chart
config/samples/        -> Example PaperclipInstance CRs
.github/workflows/     -> CI/CD pipelines
```

**Separation of concerns:** Controller logic (`internal/controller/`) only orchestrates reconciliation. All resource construction happens in pure functions in `internal/resources/`. This makes builders easy to unit test without envtest.

## Paperclip-Specific Notes

- Paperclip is a Node.js app running on port 3100 (not a Go binary)
- Health endpoint: `GET /api/health`
- Requires `HOST=0.0.0.0` and `SERVE_UI=true` for Kubernetes
- WebSocket support needed in Ingress for real-time UI updates
- Heartbeat scheduler runs in the server process -- only one instance should run it in multi-replica setups
- Database modes: embedded (PGlite), external (connection string), managed (operator-provisioned PostgreSQL)
- Data directory: `/paperclip` (mounted as PVC)

## Reconciliation Rules

These rules are enforced by CI (Reconcile Guard check):

### Always use `controllerutil.CreateOrUpdate` for managed resources

Never call `r.Update()` or `r.Create()` directly on managed resources. Always use:

```go
obj := &appsv1.StatefulSet{
    ObjectMeta: metav1.ObjectMeta{
        Name:      resources.StatefulSetName(instance),
        Namespace: instance.Namespace,
    },
}
_, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
    desired := resources.BuildStatefulSet(instance)
    obj.Labels = desired.Labels
    obj.Spec = desired.Spec
    return controllerutil.SetControllerReference(instance, obj, r.Scheme)
})
```

**Exception:** `r.Update(ctx, instance)` on the CR itself is allowed for finalizer management. Add `// reconcile-guard:allow` for any other legitimate exceptions.

### Explicitly set all Kubernetes default values in builders

### Preserve server-assigned fields

When updating resources, preserve fields assigned by the API server:
- Service: `ClusterIP`, `ClusterIPs`
- PVC: immutable after creation -- only create, never update

### Owner references

Set `controllerutil.SetControllerReference` on all managed resources.

## Coding Conventions

### Go style
- Use `0o644` (not `0644`) for octal literals
- Wrap errors: `fmt.Errorf("context: %w", err)`
- Use the generic `Ptr[T]` helper from `internal/resources/common.go` for pointer values
- Never use em dashes or en dashes -- use regular hyphens/dashes
- Run `make fmt` and `make lint` before committing

### Commit messages
Use conventional commits: `feat:`, `fix:`, `docs:`, `ci:`, `chore:`, `refactor:`, `test:`

### CRD API changes
After modifying types in `api/v1alpha1/paperclipinstance_types.go`:
1. Run `make generate` (regenerates `zz_generated.deepcopy.go`)
2. Run `make manifests` (regenerates CRD YAML in `config/crd/bases/`)
3. Commit the generated files

### Testing
- Resource builders: unit tests in `internal/resources/resources_test.go` (fast, no deps)
- Controller integration: envtest suite in `internal/controller/` (needs kubebuilder binaries)
- Always add tests when adding new resource builders or changing CRD fields
