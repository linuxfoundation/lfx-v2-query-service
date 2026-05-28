---
name: query-service-dev
description: Auto-attaches when editing Go code, Goa design, NATS request/reply, OpenSearch, CEL filter, pagination, request context, error mapping, generated code, tests, lint, or license headers inside lfx-v2-query-service. Owns repo-local Go coding conventions plus query-specific implementation truth (post-page-shrinkage pagination, CEL vs OpenSearch split, batched access-check pattern). Defers central platform composition, service classes, and write/read/access-check architecture to `lfx-skills:lfx-platform-architecture`.
paths:
  - "**/*.go"
  - "go.mod"
  - "go.sum"
  - "Makefile"
  - "design/**"
  - "cmd/**"
  - "internal/**"
  - "pkg/**"
  - "gen/**"
  - ".claude/skills/query-service-dev/**"
allowed-tools: Read, Glob, Grep, Edit, Write, Bash
---

# Development Conventions

Repo-owned Go coding conventions for `lfx-v2-query-service`, plus the
query-specific implementation truths (pagination, CEL placement, batched
access checks) that every change here has to respect.

Use this skill alongside, not instead of:

- `lfx-skills:lfx-platform-architecture`: platform composition, V2 service
  classes, write/read/access-check at the platform level, cross-repo handoff.
- The peer `query-resource` skill in this repo: the per-resource query
  workflow (new query parameter, OpenSearch field changes, debugging).
- `docs/query-service-contract.md` and
  `docs/indexed-data-types.md`: query API contract and
  indexed type catalog.

This skill wins only on repo-local Go conventions and the query-service's
own implementation patterns. The platform-level explainer wins on cross-
service architecture, the peer skill wins on per-resource workflow, and the
reference docs win on contract.

## Repo layout

```text
design/                       Goa design (edit, then `make apigen`)
gen/                          Generated. Do not hand-edit.
cmd/                          main, http wiring, Goa service implementation,
                              converters between Goa payloads and domain.
internal/domain/model/        Domain entities (SearchCriteria, SearchResult,
                              Resource, CountResult).
internal/domain/port/         Interfaces: ResourceSearcher,
                              AccessControlChecker, ResourceFilter,
                              Authenticator.
internal/service/             Business logic (resource_search.go is the
                              canonical orchestrator).
internal/infrastructure/      Adapters: opensearch, nats, clearbit, auth,
                              filter (CEL), mock.
internal/middleware/          Request-context middleware (request_id, etc.).
pkg/constants/                Subjects, principals, header names, page-size
                              constants. Add new constants here, not at
                              call sites.
pkg/errors/                   Typed domain errors. Use these; do not invent
                              a parallel sentinel family.
pkg/paging/                   Opaque page-token codec.
pkg/log/                      `log/slog` context helpers.
```

## Inline Go conventions

Every Go file in this repo follows the rules below. The deep version with
do/don't lists lives in `references/go-development-conventions.md`; read it
when in doubt or when adding a new package.

- **License header.** Every new `.go` file starts with the two-line header:

  ```go
  // Copyright The Linux Foundation and each contributor to LFX.
  // SPDX-License-Identifier: MIT
  ```

- **Generated code.** Never hand-edit `gen/`. Change `design/` first, then
  run `make apigen`. Keep handwritten code in `cmd/`, `internal/`, `pkg/`.
- **Logging.** Use `log/slog` with the `*Context` variants
  (`slog.InfoContext`, `slog.DebugContext`, `slog.ErrorContext`). Include
  stable fields: `error`, `principal`, `object_type`, `object_id`,
  `object_ref`, `request_id`. Never log JWTs, raw bearer headers, or full
  request bodies. Honor `LOG_LEVEL` and the `-d` debug flag.
- **Errors.** Construct domain errors via `pkg/errors` (`NewValidation`,
  `NewNotFound`, `NewConflict`, etc.) so `cmd/service/error.go::wrapError`
  maps them to the right HTTP status. Wrap upstream errors with `%w` so
  `errors.Is` and `errors.As` still work. Translate at the Goa boundary,
  not in deeper layers.
- **Request context.** Middleware in `internal/middleware/` owns context
  setup (request ID, principal). Read context with the typed keys in
  `pkg/constants/http.go` (`PrincipalContextID`, `RequestIDHeader`); do not
  introduce bare-string context keys.
- **NATS request/reply.** Query-service is a NATS client, not a subscriber.
  Keep subjects in `pkg/constants/access_control.go`. Access checks go
  through `NATSClient.CheckAccess` using the caller-provided timeout
  (`15*time.Second` from the service layer today); direct tuple reads use
  `RequestWithContext` in `NATSClient.ReadTuples`. Close the connection
  through `NATSAccessControlChecker.Close()` from the graceful-shutdown path
  in `cmd/main.go`.
- **Pagination.** Use `page_size` and opaque `page_token` from
  `pkg/constants/query.go` (`DefaultPageSize = 50`, `MaxPageSize = 1000`)
  and the codec in `pkg/paging`. Treat the token as opaque to callers; do
  not let downstream code parse it.
- **Tests.** Mock external systems through the interfaces in
  `internal/domain/port/` and the mocks in `internal/infrastructure/mock/`.
  Use table-driven tests with one test function per exported method. Co-
  locate `*_test.go`. Run `make test` (race detector is on).
- **Format and lint.** Run `make fmt` and `make lint`. CI uses
  `mega-linter-runner`. Follow `revive.toml` for exported-symbol
  docstrings.

## Query-specific implementation truths

These patterns are easy to get wrong and must be preserved by every
change to the query pipeline. Pull this section into review whenever you
touch `resource_search.go`, the OpenSearch adapter, the CEL filter, or
pagination.

### 1. Post-page-shrinkage pagination

The OpenSearch page comes back full, then CEL filter and access-control
filtering shrink it. The page-token decision is made on the raw OpenSearch
page, not on the post-shrink result.

- `internal/infrastructure/opensearch/client.go`: emits a `page_token` only
  when `len(hits) == pageSize`. The token encodes the keyset cursor via
  `pkg/paging.EncodePageToken` plus the secret from
  `pkg/global.PageTokenSecret`.
- `internal/service/resource_search.go`: applies CEL filter, then access
  control, then attaches the (already decided) `PageToken` to
  `SearchResult`. A returned page may have fewer than `page_size` items.
- Callers must keep paging until `page_token` is absent rather than
  stopping at the first short page. Document this in any new external
  contract.

Anti-pattern: hiding the page token when the filtered result count drops
below `page_size`. That breaks pagination because the unfiltered next page
may still have matches.

### 2. CEL vs OpenSearch query split

OpenSearch does the narrowing. CEL does the per-resource refinement.

- OpenSearch handles `type`, `name`, `parent`, `tags`/`tags_all`,
  `filters`/`filters_all`/`filters_or`, date range,
  `filter_grants=direct` object-ref pre-filtering, and the implicit
  `latest: true` clause. Template lives in
  `internal/infrastructure/opensearch/template.go`.
- CEL runs after OpenSearch and before access checks in
  `internal/service/resource_search.go` (see the `criteria.CelFilter`
  block). Implementation in `internal/infrastructure/filter/cel_filter.go`
  enforces 1000-char max expression length, 100ms per-resource timeout,
  and an LRU cache (100 entries, 5-minute TTL).
- Add new structured filters to the OpenSearch template, not to CEL. CEL
  is for caller-supplied expressions over `data.*`.
- CEL cannot rescue a resource that was not in the OpenSearch page.
  Always narrow with primary criteria first.

Each OpenSearch clause counts against `maxClauseCount`. Rules in
`docs/query-service-contract.md`; the conversion to a 400
happens in `internal/infrastructure/opensearch/client.go::hasTooManyClauses`
plus `pkg/errors.NewValidation` plus `cmd/service/error.go::wrapError`.

### 3. Batched access-check pattern

Access checks are one batched NATS request/reply per query, not per
resource.

- `internal/service/resource_search.go::BuildMessage` walks the result set
  once, dedupes by `ObjectRef`, skips `public: true` resources, and writes
  one line per remaining resource in the format
  `{access_check_object}#{access_check_relation}@user:{principal}`.
- `CheckAccess` sends the batch on `lfx.access_check.request`
  (`constants.AccessCheckSubject`) with a 15s timeout via
  `internal/infrastructure/nats/access_control.go`.
- Response is tab-separated `key\tbool` lines. Resources whose response is
  `false` or missing are dropped. Order is not guaranteed; match on the
  request-line token, not on index.
- For anonymous principals (`constants.AnonymousPrincipal`), skip the
  batch entirely and rely on the OpenSearch `public: true` filter
  (`criteria.PublicOnly = true`). Also set the
  `AnonymousCacheControlHeader` on the response.
- The `filter_grants=direct` path is the only place where this service
  reads FGA tuples directly (via `ReadTuplesSubject`). It still uses a
  single NATS round-trip per request.

### 4. Count-query access pattern

`GET /query/resources/count` does not fetch resource hits and then filter
them. It counts public resources directly, then aggregates private resources
by `access_check_query.keyword` and access-checks those aggregation keys.

- Criteria are built in `cmd/service/converters.go`:
  `payloadToCountPublicCriteria()` sets `PublicOnly`, while
  `payloadToCountAggregationCriteria()` sets `PrivateOnly`,
  `GroupBy: "access_check_query.keyword"`, and `PageSize: 0`.
- `internal/service/resource_search.go::BuildCountMessage` emits one access
  check per aggregation bucket, then `CheckCountAccess` adds only allowed
  bucket doc counts to the public count.
- `HasMore` is set when `SumOtherDocCount > 0`; callers need a narrower
  count query if they require an exact number beyond the aggregation bucket
  window.

Anti-patterns: looking up tuples synchronously per result, issuing one
access check per resource, or forgetting to dedupe by `ObjectRef`.

## When to escalate

- Cross-service flow or service-class question: switch to
  `lfx-skills:lfx-platform-architecture`.
- Cross-repo routing or "which repo owns X": switch to `lfx-skills:lfx`.
- Per-resource query workflow (new param, debugging "user can't see X"):
  switch to the peer `query-resource` skill.
- Contract details (parameters, response fields, OpenSearch field
  meanings, CEL caveats, clause-limit rules): read
  `docs/query-service-contract.md`.
- Indexed type to owning service mapping: read
  `docs/indexed-data-types.md`.

## References

- `references/go-development-conventions.md`, Contents: deep do/don't lists
  for generated code, logging, errors, request context, pagination, NATS,
  tests, formatting. Read when adding a new package or when the inline
  section above is not enough.
