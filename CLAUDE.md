# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

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

- **Dependency Injection**: Concrete implementations injected in `cmd/main.go`
- **Port/Adapter Pattern**: Domain interfaces (ports) with swappable implementations
- **Repository Pattern**: Search and access control abstracted behind interfaces

### API Design (Goa Framework)

- Design specifications in `design/` directory
- Generated code in `gen/` (DO NOT manually edit)
- After design changes, always run `make apigen`

### Request Flow

1. HTTP request → Goa generated server (`gen/http/query_svc/server/`)
2. Service layer (`cmd/query_svc/query_svc.go`)
3. Use case orchestration (`internal/service/resource_search.go`)
4. Domain interfaces called with concrete implementations
5. Response formatted and returned through Goa

### Configuration

Environment variables control implementation selection:

- `SEARCH_SOURCE`: "mock" or "opensearch"
- `ACCESS_CONTROL_SOURCE`: "mock" or "nats"
- Additional configs for OpenSearch and NATS connections

### Testing Strategy

- Unit tests use mock implementations
- Integration tests can switch between real and mock implementations
- Test files follow `*_test.go` pattern alongside implementation files

## CEL Filter Feature

The service supports Common Expression Language (CEL) filtering for post-query resource filtering.

### Overview

CEL filtering allows API consumers to filter resources on arbitrary data fields using a safe, non-Turing complete expression language. The filter is applied after the OpenSearch query but before access control checks.

### Implementation Details

**Location**: `internal/infrastructure/filter/cel_filter.go`

**Key Components**:
- **ResourceFilter Interface** (`internal/domain/port/filter.go`): Domain interface for filtering
- **CELFilter Implementation**: Uses `google/cel-go` library for expression evaluation
- **Expression Caching**: LRU cache with TTL for compiled CEL programs
- **Security Features**: Max expression length (1000 chars), evaluation timeout (100ms per resource)

**Integration Point**: `internal/service/resource_search.go` (lines 84-102)
- CEL filter applied after OpenSearch query
- Filters resources before access control checks
- Reduces number of access control checks needed

### Available Variables in CEL Expressions

- `data` (map): Resource data object
- `resource_type` (string): Resource type
- `id` (string): Resource ID

Note: `type` is a reserved word in CEL, so we use `resource_type` instead.

### Example Usage

```go
// API call
GET /query/resources?type=project&cel_filter=data.slug == "tlf"

// Expression is evaluated against each resource after OpenSearch query
// Only matching resources proceed to access control checks
```

### Adding CEL Filter Tests

When writing tests that involve resource search:

```go
// Use MockResourceFilter for testing
mockFilter := mock.NewMockResourceFilter()

// Pass to service constructor
service := service.NewResourceSearch(mockSearcher, mockAccessChecker, mockFilter)
```

### Common CEL Operations

- Equality: `data.status == "active"`
- Comparison: `data.priority > 5`
- Boolean logic: `data.status == "active" && data.priority > 5`
- String operations: `data.name.contains("LF")`
- List membership: `data.category in ["security", "networking"]`
- Field existence: `has(data.archived)`

### Performance Considerations

- Compiled CEL programs are cached (100 max entries, 5-minute TTL)
- Each resource evaluation has 100ms timeout
- Post-query filtering means pagination may return fewer results than page size
- For best performance, use specific OpenSearch criteria first, then CEL for refinement

### Important Limitations

**Pagination**: CEL filters apply only to results from each OpenSearch page. If the target resource is not in the first page of OpenSearch results, it won't be found even if it matches the CEL filter. Always use specific primary search criteria (`type`, `name`, `parent`) to narrow OpenSearch results first.
