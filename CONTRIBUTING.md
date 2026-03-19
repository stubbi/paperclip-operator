# Contributing to Paperclip Kubernetes Operator

Thank you for considering contributing to the Paperclip Kubernetes Operator!

## How Can I Contribute?

### Reporting Bugs

Before creating bug reports, please check the existing issues to avoid duplicates.

**Great bug reports include:**
- A quick summary and/or background
- Steps to reproduce (be specific!)
- What you expected would happen
- What actually happens
- Kubernetes version, operator version, and other relevant environment details

### Pull Requests

1. Fork the repo and create your branch from `main`
2. If you've added code that should be tested, add tests
3. If you've changed APIs, update the documentation
4. Ensure the test suite passes
5. Make sure your code lints
6. Issue that pull request!

## Development Setup

### Prerequisites

- Go 1.25+
- Docker
- kubectl
- Kind (for local testing)
- Make

### Getting Started

```bash
# Clone your fork
git clone https://github.com/YOUR_USERNAME/k8s-operator.git
cd k8s-operator

# Install dependencies
go mod download

# Generate code and manifests
make generate manifests

# Run tests
make test

# Run linter
make lint
```

### Running Locally

```bash
# Create a Kind cluster
kind create cluster

# Install CRDs
make install

# Run the operator locally (outside the cluster)
make run
```

### Testing Changes

```bash
# Run unit tests (fast, no envtest)
go test ./internal/resources/ -v

# Run all tests (unit + integration)
make test

# Run linter
make lint

# Run E2E tests (requires Kind)
make test-e2e

# Run benchmarks
make bench
```

## Style Guidelines

### Git Commit Messages

Use [Conventional Commits](https://www.conventionalcommits.org/):
- `feat:` for new features
- `fix:` for bug fixes
- `docs:` for documentation changes
- `chore:` for maintenance tasks
- `test:` for test additions/changes
- `refactor:` for code refactoring
- `ci:` for CI/CD changes

### Go Code Style

- Follow [Effective Go](https://golang.org/doc/effective_go) guidelines
- Run `make fmt` and `make lint` before committing
- Use `0o644` (not `0644`) for octal literals
- Wrap errors: `fmt.Errorf("context: %w", err)`
- Use the generic `Ptr[T]` helper for pointer values

### CRD API Changes

After modifying types in `api/v1alpha1/instance_types.go`:
1. Run `make generate` (regenerates deepcopy methods)
2. Run `make manifests` (regenerates CRD YAML)
3. Run `make sync-chart-crds` (syncs CRDs into Helm chart)
4. Commit the generated files

### Kubernetes Resources

- Follow [Kubernetes API conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md)
- Always use `controllerutil.CreateOrUpdate` for managed resources (never bare `r.Update()`)
- Set `controllerutil.SetControllerReference` on all managed resources

## Project Structure

```
.
├── api/v1alpha1/          # CRD type definitions
├── cmd/                   # Main entrypoint
├── config/                # Kubernetes manifests
│   ├── crd/              # CRD definitions
│   ├── manager/          # Operator deployment
│   ├── rbac/             # RBAC configuration
│   └── samples/          # Example CRs
├── internal/
│   ├── controller/       # Reconciliation logic
│   └── resources/        # Resource builders
├── charts/               # Helm chart
├── bundle/               # OLM bundle
└── test/e2e/            # E2E tests
```

## Review Process

1. All submissions require review from a maintainer
2. CI must pass before merging
3. At least one approval is required

## License

By contributing, you agree that your contributions will be licensed under the Apache License 2.0.
