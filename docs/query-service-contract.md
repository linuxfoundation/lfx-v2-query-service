<!-- Copyright The Linux Foundation and each contributor to LFX. -->
<!-- SPDX-License-Identifier: MIT -->

# Query Service Contract

The query-service provides a generic HTTP search API over OpenSearch. It does not know
about individual resource types: it queries a shared `resources` index and delegates
access control to fga-sync. Indexer-service populates that index from NATS events
published by resource services.

## How the Three Services Connect

```text
Resource Service
    → publishes lfx.index.{type}       → indexer-service → OpenSearch (resources index)
    → publishes lfx.fga-sync.*         → fga-sync → OpenFGA

Client Request
    → GET /query/resources
        → query-service queries OpenSearch
        → for each non-public result: batch access check via NATS → fga-sync
        → drops resources where access is denied
        → returns filtered results
```

The `access_check_object` and `access_check_relation` fields written into each
OpenSearch document by the indexer (from the resource service's `IndexingConfig`) are
what the query-service uses to build the FGA check. New messages with missing
access fields are rejected by the indexer. If a legacy or manually inserted
non-public document is missing either field, query-service cannot construct an
access-check message for it and treats it as unauthorized: the resource is
excluded from results (it does not bypass access filtering).

## HTTP API

### GET /query/resources

| Parameter | Type | Description |
| --- | --- | --- |
| `v` | int (required) | API version, must be `1` |
| `name` | string | Typeahead/prefix search on `name_and_aliases` |
| `type` | string | Filter by `object_type` (e.g. `committee`) |
| `parent` | string | Filter by `parent_refs` (format: `project:uuid`) |
| `tags` | []string | OR filter, any tag matches |
| `tags_all` | []string | AND filter, all tags must match |
| `filters` | []string | AND filter, exact field filters (format: `field:value`, against `data.*`). Prefer `filters_all` going forward |
| `filters_all` | []string | AND filter, all provided filters must match. Explicit preferred alias for `filters` |
| `filters_or` | []string | OR filter, at least one provided filter must match (format: `field:value`, against `data.*`) |
| `date_field` | string | Field within `data` to range-filter on |
| `date_from` / `date_to` | string | ISO 8601 or `YYYY-MM-DD` |
| `cel_filter` | string | CEL expression for in-process filtering (applied after OpenSearch, before access check) |
| `filter_grants` | string | `direct` filters to resources where the authenticated user has direct FGA tuples. Requires `type` |
| `sort` | string | `name_asc` (default), `name_desc`, `updated_asc`, `updated_desc`, `best_match` |
| `page_size` | int | 1–1000, default 50 |
| `page_token` | string | Opaque pagination token (keyset-based) |

**Response**:

```json
{
  "resources": [
    { "type": "committee", "id": "uuid", "data": { ... } }
  ],
  "page_token": "opaque-token-or-omitted",
  "cache_control": "public, max-age=300"
}
```

`data` is the full resource object as stored by the indexer: schema-free, no
migration needed for new fields.

**Required parameters:** at least one of `name`, `parent`, `type`, `tags`, or
`filter_grants` must be provided. A request that supplies only `cel_filter`,
`date_*`, `filters*`, `sort`, or pagination parameters (with none of the five
above) is rejected with a `400 Bad Request` ("at least one search parameter
must be provided: name, parent, type, tags, or filter_grants"). In addition,
`filter_grants` requires `type` (otherwise a `400` is returned). Validation
lives in `validateSearchCriteria` in `internal/service/resource_search.go`.

### GET /query/resources/count

Same parameters as `GET /query/resources` except `cel_filter`,
`filter_grants`, `sort`, `page_size`, and `page_token`. Returns:

```json
{ "count": 42, "has_more": false }
```

Count queries use two OpenSearch requests for authenticated principals:

- A `_count` request for public resources.
- An aggregation over private resources grouped by
  `access_check_query.keyword`, followed by one batched FGA access check over
  those aggregation keys.

The service adds authorized private bucket counts to the public count. The
private aggregation is capped at `constants.DefaultBucketSize` (100) distinct
`access_check_query` buckets. If the private aggregation overflows that bucket
size, `has_more` is `true` and callers should issue a narrower count query.

## Anonymous vs Authenticated Requests

| | Anonymous (`_anonymous`) | Authenticated |
| --- | --- | --- |
| OpenSearch filter | `public: true` only | All documents |
| FGA check | Skipped | Batch check via NATS for non-public results only (`public: true` results skip the check) |
| Cache-Control | `public, max-age=300` | Not set |

Anonymous users are identified by the `_anonymous` principal. They only see
resources where `public: true` was set in the `IndexingConfig`. Authenticated
users query all matching documents first, then the service removes results that
fail the FGA check.

## Access Control Flow

For authenticated requests, the query-service:

1. Runs the OpenSearch query, gets back all matching documents regardless of permissions
2. Builds a batch access check message, one line per non-public resource:

   ```text
   committee:abc-123#viewer@user:alice
   project:xyz-789#viewer@user:alice
   ```

   (format: `{access_check_object}#{access_check_relation}@user:{principal}`)
3. Sends to fga-sync via NATS request/reply:
   - Subject: `lfx.access_check.request`
   - Timeout: 15 seconds
4. Parses the tab-separated response:

   ```text
   committee:abc-123#viewer@user:alice\ttrue
   project:xyz-789#viewer@user:alice\tfalse
   ```

5. Drops any resource where the response is `false` or missing

The query-service deduplicates by `object_ref` so each FGA object is checked at most once
per request.

### Direct grant filtering

`filter_grants=direct` narrows a query to resources where the authenticated user
has direct OpenFGA tuples for the requested `type`.

- Requires `type`, because fga-sync reads tuples by object type.
- Requires an authenticated principal. Anonymous requests fail validation.
- Calls `lfx.access_check.read_tuples` through fga-sync, then pre-filters
  OpenSearch by the returned `object_ref` values.
- The normal access-check pass still runs after OpenSearch returns resources.
- This is a direct-grant filter only. It does not expand inherited permissions
  through parent projects or committees.

## OpenSearch Document Fields Used by Query Service

These fields must be correctly populated by the indexer (via `IndexingConfig`) for a
resource to be discoverable and accessible:

| Field | Purpose | If missing/wrong |
| --- | --- | --- |
| `object_ref` | Stable `{type}:{id}` ref used for access-check dedupe and `filter_grants=direct` pre-filtering | Duplicate checks or direct-grant filters can behave incorrectly |
| `object_type` | Type filtering (`type=` param) | Resource won't match type queries |
| `object_id` | Debug lookup and warning-log context for the source resource ID | Harder to trace an indexed document back to the owning resource |
| `parent_refs` | Parent filtering (`parent=` param) | Resource won't appear in parent queries |
| `name_and_aliases` | Typeahead search (`name=` param) | Resource won't appear in name searches |
| `tags` | Tag filtering | Resource won't match tag queries |
| `public` | Marks the resource as public: skips the FGA check for all callers (`BuildMessage` returns it without checking when `public` is true) and lets anonymous callers see it via the `public: true` filter | Anonymous users can't see it; setting it incorrectly exposes a private resource to everyone |
| `access_check_object` | Identifies FGA object to check | Non-public resource treated as unauthorized and excluded from results (legacy/malformed document) |
| `access_check_relation` | FGA relation to check (e.g. `viewer`) | Non-public resource treated as unauthorized and excluded from results (legacy/malformed document) |
| `access_check_query` | `{access_check_object}#{access_check_relation}` used by count aggregations | Authenticated counts can undercount private resources |
| `sort_name` | Sorting by name | May sort incorrectly |
| `updated_at` | Sorting by `updated_asc` / `updated_desc` | Updated-date sorting may be wrong or place records last |
| `data` | Returned as-is in the response | Missing fields in response |
| `latest` | Always filtered to `true` | Old versions hidden (correct behavior) |

**The most common debugging gotcha**: if a user can't see a resource they should
have access to, check that the indexed document has current access fields and
that the owning service is publishing a valid `IndexingConfig`. New malformed
messages are rejected by the indexer, but old or manually inserted documents can
still be missing access fields. Query OpenSearch directly:

```bash
curl "$OPENSEARCH_URL/$OPENSEARCH_INDEX/_search" -H 'Content-Type: application/json' -d '{
  "query": {"bool": {"must": [
    {"term": {"object_id": "<uid>"}},
    {"term": {"latest": true}}
  ]}},
  "_source": ["access_check_object", "access_check_relation", "public"]
}'
```

## tags vs filters vs cel_filter

| Mechanism | Use for | How it works |
| --- | --- | --- |
| `tags` / `tags_all` | Values in the `tags` field (exact match) | OpenSearch `term` query |
| `filters` / `filters_all` | AND logic, all filters must match; values inside `data` (format: `field:value`) | Individual `term` clauses in OpenSearch `must` |
| `filters_or` | OR logic, at least one filter must match; same format as `filters` | Nested `bool/should` with `minimum_should_match: 1` inside `must` |
| `cel_filter` | Complex expressions not expressible via tags/filters | Applied in-process after OpenSearch, before access check |

Resource services control what appears in `tags` via their indexing contract and
domain model. Use [`indexed-data-types.md`](indexed-data-types.md) to look up
the owning service and NATS subject for a type, then read that service's
`docs/indexer-contract.md` before changing emitted fields or tags.

## Page Size

The `page_size` and `page_token` query parameters are owned by this service's
Goa design and OpenSearch adapter: range 1-1000, default 50, opaque keyset
`page_token` returned only when the raw OpenSearch page is full.

Query-service specifics:

- The keyset `page_token` is generated by the query-service (it encodes the
  OpenSearch `search_after`/sort cursor via `paging.EncodePageToken`) only when
  `len(hits) == pageSize` (otherwise no more pages).
- `cel_filter` and access-control checks run after OpenSearch returns a page,
  so a page may shrink to fewer than `page_size` results. Callers should keep
  paginating until `page_token` is absent rather than stopping at the first
  short page.
- Implementation files: `internal/infrastructure/opensearch/client.go`
  (token generation), `internal/domain/model/search_criteria.go` (`PageSize`
  field), `pkg/constants/query.go` (`DefaultPageSize`, `MaxPageSize`).

## Date Range Filtering

The query service supports filtering resources by date ranges on fields within
the `data` object.

- `date_field` (string, optional): date field to filter on (automatically
  prefixed with `"data."`)
- `date_from` (string, optional): start date (inclusive, gte operator)
- `date_to` (string, optional): end date (inclusive, lte operator)

Supported formats:

1. **ISO 8601 datetime**: `2025-01-10T15:30:00Z` (time used as provided)
2. **Date-only**: `2025-01-10` (converted to start/end of day UTC)
   - `date_from` → `2025-01-10T00:00:00Z`
   - `date_to` → `2025-01-10T23:59:59Z`

Examples:

```bash
GET /query/resources?v=1&date_field=updated_at&date_from=2025-01-10&date_to=2025-01-28
GET /query/resources?v=1&date_field=created_at&date_from=2025-01-10T15:30:00Z&date_to=2025-01-28T18:45:00Z
GET /query/resources?v=1&date_field=created_at&date_from=2025-01-01
GET /query/resources?v=1&type=project&tags=active&date_field=updated_at&date_from=2025-01-01&date_to=2025-03-31
```

Implementation:

- Date parsing: `cmd/service/converters.go` (`parseDateFilter()`)
- Domain model: `internal/domain/model/search_criteria.go` (`DateField`,
  `DateFrom`, `DateTo`)
- OpenSearch query: `internal/infrastructure/opensearch/template.go` (range
  query with `gte` / `lte`)
- API design: `design/query-svc.go`
- Tests: `cmd/service/converters_test.go`

## CEL Filter

The service supports Common Expression Language (CEL) filtering for post-query
resource filtering. CEL is safe and non-Turing complete; the filter is applied
after the OpenSearch query but before access control checks.

Location: `internal/infrastructure/filter/cel_filter.go`.

Key components:

- **ResourceFilter Interface**: `internal/domain/port/filter.go`
- **CELFilter Implementation**: uses `google/cel-go` for evaluation.
- **Expression Caching**: TTL-bounded map cache for compiled CEL programs (100
  max entries, 5-minute TTL). There is no LRU eviction: when the cache is full
  it first drops expired entries, and if it is still full it stops caching new
  programs (they are recompiled on each use until space frees up).
- **Security**: max expression length 1000 chars, evaluation timeout 100ms per
  resource.

Integration point: `internal/service/resource_search.go` (CEL filter applied
after OpenSearch query and before access control checks; reduces the number of
access control checks needed).

Available variables:

- `data` (map): resource data object
- `resource_type` (string): resource type
- `id` (string): resource ID

`type` is a reserved word in CEL, use `resource_type` instead.

Example:

```bash
GET /query/resources?type=project&cel_filter=data.slug == "tlf"
```

Common operations:

- Equality: `data.status == "active"`
- Comparison: `data.priority > 5`
- Boolean: `data.status == "active" && data.priority > 5`
- String ops: `data.name.contains("LF")`
- List membership: `data.category in ["security", "networking"]`
- Field existence: `has(data.archived)`

**Important limitation:** CEL filters apply per OpenSearch page, after the raw
page is fetched and before access control. A matching resource that falls
outside the first raw page is still returned once the caller follows
`page_token` to that page, so callers must keep paginating until `page_token`
is absent (see [Page Size](#page-size)). The trade-off is that a CEL expression
which significantly reduces a page can yield short or empty pages, so it is not
a substitute for narrowing the OpenSearch query itself. Always pair `cel_filter`
with specific primary search criteria (`type`, `name`, `parent`) to keep the raw
result set small.

## Query Clause Limits

OpenSearch enforces a configurable hard limit on clauses per query
(`maxClauseCount`). Exceeding it returns a `400 Bad Request` to the API caller
with a message indicating the query exceeded the maximum clause limit.

Clause-count rules:

- Each `tags`, `tags_all`, `filters`, `filters_all` value is 1 clause.
- `filters_or` adds 1 wrapping clause plus 1 per value.
- `type`, `parent`, `name`, and the date range each add 1 clause.
- Every request adds 1 fixed clause (`latest: true`).
- Anonymous queries and count subqueries add public/private clauses as needed.
- `filter_grants=direct` adds one `object_ref` terms clause populated from
  fga-sync's direct tuple response.

Error handling implementation: `internal/infrastructure/opensearch/client.go`
detects `opensearch.StructError` where any `RootCause` entry has
`Type == "too_many_nested_clauses"` (OpenSearch wraps it inside a
`search_phase_execution_exception`) and converts it to `errors.Validation` so
`wrapError()` in `cmd/service/error.go` maps it to HTTP 400. Applied
consistently across `Search()`, `AggregationSearch()`, and `Count()`.
