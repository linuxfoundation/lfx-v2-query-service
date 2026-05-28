# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> **Central LFX skills:**
> - Start with `lfx-skills:lfx` for cross-repo tasks, "where does X live" questions, owner/peer repo routing, or missing checkouts.
> - Use `lfx-skills:lfx-platform-architecture` after routing when you need platform composition, V2 service classes, write/read/access-check flows, cross-service responsibilities, NATS/KV ownership, or handoff points across FGA, indexer, query, Heimdall, OpenFGA, Helm, or ArgoCD.
> - The repo-local `query-service-dev` skill auto-attaches on Go paths (`**/*.go`, `design/**`, `cmd/**`, `internal/**`, `pkg/**`, `gen/**`, `go.mod`, `go.sum`, `Makefile`) and owns repo-local Go conventions plus query-specific implementation truth: post-page-shrinkage pagination, CEL-vs-OpenSearch split, batched access-check pattern.
> - The repo-local `query-resource` skill owns the per-resource query workflow (new query parameter, OpenSearch field changes, debugging "user can't see a resource").
> - This repo OWNS query behavior: indexed reads over OpenSearch, access filtering via batched fga-sync calls, CEL post-filtering, and post-page-shrinkage pagination semantics. Treat repo-local truth as authoritative; the central platform-architecture skill describes the cross-service shape, not these mechanics.
> - Repo-owned docs under `docs/` are canonical for query behavior, active indexed resource inventory, and query cookbook examples. If the plugin is missing, install with `/plugin marketplace add linuxfoundation/lfx-skills` then `/plugin install lfx-skills@lfx-skills`.

## Essential Commands

### Build and Development

```bash
# Install dependencies and development tools
make setup
make deps

# Generate API code from Goa design (required after design changes)
make apigen
# Or directly: goa gen github.com/linuxfoundation/lfx-v2-query-service/design

# Build the application
make build

# Run locally with mock implementations (development)
SEARCH_SOURCE=mock ACCESS_CONTROL_SOURCE=mock go run ./cmd

# Run with OpenSearch and NATS (production-like)
SEARCH_SOURCE=opensearch ACCESS_CONTROL_SOURCE=nats \
OPENSEARCH_URL=http://localhost:9200 \
OPENSEARCH_INDEX=resources \
NATS_URL=nats://localhost:4222 \
go run ./cmd
```

### Testing and Validation

```bash
# Run tests
make test
# Or: go test -v -race -coverprofile=coverage.out ./...

# Run linting
make lint
# Or: golangci-lint run ./...

# Run specific test
go test -v -run TestResourceSearch ./internal/service/
```

### Docker Operations

```bash
# Build Docker image
make docker-build

# Run Docker container
make docker-run
```

## Architecture Overview

This service follows clean architecture principles with clear separation of concerns:

## Agent Guidance

Repo-owned guidance is split: convention references live under `.claude/skills/query-service-dev/references/`; contract and ownership docs live under `docs/`:

- `docs/query-service-contract.md` - query API, access filtering,
  OpenSearch fields, tags, filters, and CEL caveats.
- `docs/indexed-data-types.md` - active queryable resource types
  and where their service-owned contracts live.
- `docs/resource-catalog.md` - human-readable query cookbook companion to
  `indexed-data-types.md`, organized by indexing service.

Read these before changing query contracts or helping another repo consume the
query service.

## Consumed Cross-Repo Contracts

This repo depends on contracts owned elsewhere. Do not copy or infer them from
local examples. Read the owner file before changing indexed read behavior,
access filtering, or resource-type assumptions.

- Generic indexer event contract and OpenSearch document shape:
  `lfx-v2-indexer-service/docs/indexer-contract.md`
- Generic FGA envelope and access-check contract:
  `lfx-v2-fga-sync/docs/fga-sync-contract.md`
- Per-resource indexer emission contracts:
  `<resource-service>/docs/indexer-contract.md`
- Per-resource FGA emission contracts:
  `<resource-service>/docs/fga-contract.md`

Use `lfx-skills:lfx` if an owner repo is missing locally, the path has moved,
or the task needs additional peer repos.

### Layer Structure

1. **Domain Layer** (`internal/domain/`)
   - `model/`: Core business entities (Resource, SearchCriteria, AccessCheck)
   - `port/`: Interfaces defining contracts (ResourceSearcher, AccessControlChecker)

2. **Service Layer** (`internal/service/`)
   - Business logic orchestration
   - Coordinates between domain and infrastructure

3. **Infrastructure Layer** (`internal/infrastructure/`)
   - `opensearch/`: OpenSearch implementation for resource search
   - `nats/`: NATS implementation for access control
   - `mock/`: Mock implementations for testing

4. **Presentation Layer** (`gen/`, `cmd/`)
   - Generated Goa code for HTTP endpoints
   - Service implementation connecting Goa to domain logic

### Key Design Patterns

- **Dependency Injection**: Concrete implementations selected in `cmd/main.go` and wired in `cmd/service/providers.go`
- **Port/Adapter Pattern**: Domain interfaces (ports) with swappable implementations
- **Repository Pattern**: Search and access control abstracted behind interfaces

### API Design (Goa Framework)

- Design specifications in `design/` directory
- Generated code in `gen/` (DO NOT manually edit)
- After design changes, always run `make apigen`

### Request Flow

1. HTTP request → Goa generated server (`gen/http/query_svc/server/`)
2. Goa endpoints wiring (`cmd/service/service.go`) and DI providers
   (`cmd/service/providers.go`)
3. Use case orchestration (`internal/service/resource_search.go`)
4. Domain interfaces called with concrete implementations
5. Response formatted and returned through Goa

### Configuration

Environment variables control implementation selection:

- `SEARCH_SOURCE`: "mock" or "opensearch"
- `ACCESS_CONTROL_SOURCE`: "mock" or "nats"
- Additional configs for OpenSearch and NATS connections

### Testing Strategy

Repo-owned test conventions (mock interfaces, table-driven tests, co-located `*_test.go`, one test function per exported method) live in path-scoped `query-service-dev` guidance.

Query-service specifics: integration tests can switch between real and mock implementations via the `SEARCH_SOURCE` and `ACCESS_CONTROL_SOURCE` env vars.

## API Features

Detailed API behavior (page size, date range filtering, CEL filter, query
clause limits) lives in
[`docs/query-service-contract.md`](docs/query-service-contract.md).
Read that file before changing API parameters, OpenSearch query construction,
CEL behavior, or error mapping.
