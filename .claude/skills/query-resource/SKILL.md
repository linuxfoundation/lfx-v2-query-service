---
name: query-resource
description: Use when editing the query-service `design/` Goa files, `internal/service/resource_search.go`, anything under `internal/infrastructure/opensearch/`, the access-control NATS integration in `internal/infrastructure/nats/`, or the CEL filter implementation. Also fires when adding a new query parameter, changing pagination, or debugging "user can't see a resource".
allowed-tools: Read, Glob, Grep, Edit, Write, Bash
paths: ['design/**', 'internal/service/resource_search.go', 'internal/infrastructure/opensearch/**', 'internal/infrastructure/nats/**', 'internal/infrastructure/filter/**', 'cmd/service/converters*.go', 'pkg/constants/query.go', 'docs/query-service-contract.md', 'docs/indexed-data-types.md', 'docs/resource-catalog.md', '.claude/skills/query-resource/**']
---

# Query Resource

The query-service is a generic search/read aggregator over OpenSearch with NATS access checks. It does not know about individual resource types. Follow this workflow for any change to the API contract or pipeline.

For Go coding conventions (test layout, error handling, logging, license
headers, generated code rules), see the repo-local `query-service-dev`
skill, which auto-attaches on Go paths.

## Workflow

1. **Read the authoritative patterns** at
   `references/query-service-patterns.md` (link to
   `docs/query-service-contract.md`). Confirm the change
   matches anonymous-vs-authenticated semantics, the access-check flow, and
   clause-count rules.
2. **For new query parameters**:
   - Add to the Goa design (`design/query-svc.go` and `design/types.go`).
   - Decide whether the count endpoint also supports the parameter; if yes,
     wire both `QueryResourcesPayload` and `QueryResourcesCountPayload`.
   - Run `make apigen`.
   - Wire through `cmd/service/converters.go` (`payloadToCriteria()`).
   - Add the field to `internal/domain/model/search_criteria.go`.
   - Update the OpenSearch query in
     `internal/infrastructure/opensearch/template.go`.
   - Check clause-count cost; update `pkg/constants/query.go` if a limit
     needs adjusting.
   - Add tests in `cmd/service/converters_test.go`.
   - Update `docs/query-service-contract.md`; update
     `docs/resource-catalog.md` only when the change affects cookbook
     examples.
3. **For OpenSearch field changes**: the indexer-service owns top-level
   field naming. Coordinate with indexer-service before relying on a new
   top-level field; otherwise put the field inside `data` and use `cel_filter`
   or a dynamic mapping.
4. **For access-control changes**: this service issues
   `lfx.access_check.request` to fga-sync with one line per non-public
   resource. Order is not guaranteed, match on the request token. Default
   timeout 15s.
5. **For debugging "user can't see resource"**: first confirm the type is in
   `docs/indexed-data-types.md` as an active publisher, then check
   OpenSearch directly for the resource. If `access_check_object` or
   `access_check_relation` is empty, the indexer publisher (resource service)
   sent a malformed `IndexingConfig` or the document is legacy/manual.
   Otherwise check FGA tuples.
6. **For CEL filter changes**: respect the per-resource 100ms timeout and
   1000-char expression length. Remember CEL applies after OpenSearch
   pagination, narrow with `type`, `name`, `parent` first.
7. **For pagination**: page tokens are opaque, keyset-based, generated when
   `len(hits) == pageSize`. Don't expose internal cursor structure.

## Anti-patterns

- Adding type-specific code paths in the query-service. Type-specific
  behavior belongs in the publisher (`IndexingConfig`) or in `cel_filter`.
- Updating OpenSearch documents directly: only the indexer writes.
- Looking up tuples synchronously per result. Always use the batch
  `lfx.access_check.request` and deduplicate by `object_ref`.
- Relying on `cel_filter` to find a resource not in the first OpenSearch
  page.

## References

- `references/query-service-patterns.md`: query API, access flow, CEL,
  clause limits, debug gotchas.
