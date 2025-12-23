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

## API Features

### Date Range Filtering

The query service supports filtering resources by date ranges on fields within the `data` object.

**Query Parameters:**

- `date_field` (string, optional): Date field to filter on (automatically prefixed with `"data."`)
- `date_from` (string, optional): Start date (inclusive, gte operator)
- `date_to` (string, optional): End date (inclusive, lte operator)

**Supported Date Formats:**

1. **ISO 8601 datetime**: `2025-01-10T15:30:00Z` (time used as provided)
2. **Date-only**: `2025-01-10` (converted to start/end of day UTC)
   - `date_from` → `2025-01-10T00:00:00Z` (start of day)
   - `date_to` → `2025-01-10T23:59:59Z` (end of day)

**Examples:**

```bash
# Date range with date-only format
GET /query/resources?v=1&date_field=updated_at&date_from=2025-01-10&date_to=2025-01-28

# Date range with ISO 8601 format
GET /query/resources?v=1&date_field=created_at&date_from=2025-01-10T15:30:00Z&date_to=2025-01-28T18:45:00Z

# Open-ended range (only start date)
GET /query/resources?v=1&date_field=created_at&date_from=2025-01-01

# Combined with other filters
GET /query/resources?v=1&type=project&tags=active&date_field=updated_at&date_from=2025-01-01&date_to=2025-03-31
```

**Implementation Details:**

- Date parsing logic: `cmd/service/converters.go` (`parseDateFilter()` function)
- Domain model: `internal/domain/model/search_criteria.go` (DateField, DateFrom, DateTo)
- OpenSearch query: `internal/infrastructure/opensearch/template.go` (range query with gte/lte)
- API design: `design/query-svc.go` (Goa design specification)
- Test coverage: `cmd/service/converters_test.go` (17 comprehensive test cases)
