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
| `name` | string | Typeahead/prefix search on `name_and_aliases`. Every term must match; the last term matches as a prefix. See [Name Search](#name-search) |
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
| `page_token` | string | Opaque pagination token (keyset-based), minted from the raw OpenSearch page. A raw page that leaves the caller **no visible resource** (after `cel_filter` and the access check) is not returned as `[] + token`: the service walks up to `SEARCH_DENIED_PAGE_WALK` further raw pages until the caller can see a resource or the result set is exhausted, so within the walk "exists but not visible to you" and "does not exist" are both `[]` with no token (no existence oracle for result sets exhaustible within the walk, e.g. unique exact-tag lookups). Past the walk limit an empty page keeps its token so paging can continue — the token then reveals only that further raw matches exist. |

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
`filter_grants`, `sort`, `page_size`, and `page_token`, plus:

| Parameter | Type | Description |
| --- | --- | --- |
| `group_by` | string | Tag prefix (`^[a-z][a-z0-9_]*$`, max 64). Groups the count by the value after `<prefix>:` in each document's `tags`, e.g. `group_by=project_uid` |
| `group_by_size` | int | 1–1000, default 100. Maximum number of groups returned; requires `group_by` (otherwise `400`, including with `metric`) |
| `metric` | string | `cardinality:<tag_prefix>` (max 80). Number of distinct `<tag_prefix>:…` tag values across the authorized documents, e.g. `metric=cardinality:email`. Any other shape, including `sum:…`, is a `400` |

`group_by` and `metric` cannot be combined (`400`: "metric per group is not
supported; group first, then count each group with tags"). To get a metric per
group, call once with `group_by`, then once per group with `metric` and
`tags=<prefix>:<value>`.
A bare `<prefix>:` tag with no value is neither a group nor a distinct value.

Grouped response (`group_by=project_uid`):

```json
{
  "count": 42,
  "has_more": false,
  "groups": [{ "key": "a1b2", "count": 30 }, { "key": "c3d4", "count": 12 }],
  "groups_complete": true,
  "group_count_error_upper_bound": 0
}
```

Cardinality response (`metric=cardinality:email`, without `group_by`):

```json
{
  "count": 42,
  "has_more": false,
  "metric_value": 17,
  "metric_complete": true
}
```

| Field | Present | Meaning |
| --- | --- | --- |
| `count` | always | Matching documents the caller may see |
| `has_more` | always | `true` when the count is not guaranteed exhaustive: the access-bucket walk stopped at `COUNT_MAX_ACCESS_BUCKETS`, or OpenSearch returned a full page without a continuation cursor (logged as a warning) |
| `groups` | with `group_by`, when non-empty | One entry per group, key = tag value with the prefix stripped, ordered by count descending then key ascending. A document carrying several `<prefix>:` tags counts once per tag. Per-group counts may be lower bounds; they are exact within the walked authorized set only when `group_count_error_upper_bound` is 0. Omitted (not `[]`) when no group matched; `groups_complete` is the signal that `group_by` was honoured |
| `groups_complete` | with `group_by` | `true` when every group is present; `false` when more groups exist than `group_by_size`, **or** when `has_more` is `true` (the groups were computed over a truncated authorized set) |
| `group_count_error_upper_bound` | with `group_by` | Maximum possible undercount per returned group from the distributed terms aggregation; 0 means exact within the walked authorized set. Independent of `groups_complete`; if `has_more` is true, unwalked access buckets are still excluded |
| `metric_value` | with `metric` | The cardinality |
| `metric_complete` | with `metric` | `true` when the distinct-value walk finished; `false` when it stopped at `COUNT_MAX_ACCESS_BUCKETS` distinct values, **or** when `has_more` is `true` |

#### How a count is computed

For an anonymous principal: one `_count` request over `public: true` documents,
with `Cache-Control: public, max-age=300`. Groups and metrics, if requested,
run over public documents only.

For an authenticated principal:

1. **Public part** — the same `_count` over `public: true` documents.
2. **Access-bucket walk** — over private documents (`must_not public:true`), a
   composite aggregation on the access-check field (see
   [Mapping the count route depends on](#mapping-the-count-route-depends-on))
   returns `COUNT_ACCESS_BUCKET_PAGE` distinct `access_check_query` values per
   page. Each page is one batched fga-sync check (`<key>@user:<principal>`
   lines, same format as the search route); the document counts of the granted
   keys are added to the count. A page with fewer buckets than the page size
   ends the walk. After a full page, once `COUNT_MAX_ACCESS_BUCKETS` buckets
   have been walked, the walk stops without requesting the next page and
   `has_more` is `true`; pages are never split. The walk stops on
   request-context cancellation; per-check timeouts bound each page's access
   check, and startup validation allows at most 100 pages per count.
   A failed access check is a
   `503`: a count is never returned as if complete when part of the authorized
   set is unknown. Likewise an OpenSearch response with a failed shard or a
   timeout is a `503` (`allow_partial_search_results=false` is sent), never a
   smaller number.
3. **Groups / metric** — skipped when nothing is visible to the caller (no
   public document matched and no private key was granted: empty groups,
   metric 0). Otherwise a second aggregation search filtered to the
   *authorized set*: `public: true` OR `access_check_query` in the granted keys
   (fewer than `COUNT_MAX_ACCESS_BUCKETS + COUNT_ACCESS_BUCKET_PAGE` values
   because whole pages are never split, always fewer than 11000). This is below
   OpenSearch's `index.max_terms_count` default of 65536; deployments with a
   lower index setting must allow for the whole-page overshoot. `group_by` is a `terms`
   aggregation on `tags` with `include: "<prefix>:.+"` and
   `shard_size = min(group_by_size × 5, 5000)` to reduce, not eliminate,
   distributed count error. OpenSearch's `doc_count_error_upper_bound` is
   returned as `group_count_error_upper_bound`: 0 means exact counts within
   the walked authorized set; a non-zero value means counts may be lower
   bounds. `groups_complete` still measures group truncation
   (`sum_other_doc_count == 0`, provided `has_more` is false), not count accuracy.
   `metric`
   is a composite walk over `tags` starting just after the bare `<prefix>:` key
   and stopping at the first key outside the prefix; like the access-bucket
   walk it is exact by construction and needs no scripting.

Resource families whose access key is per object (past meetings:
`v1_past_meeting:{id}#viewer`, and their participants, which inherit it)
produce one bucket per meeting, so a count over N meetings walks ⌈N/100⌉
pages with the defaults. A walk longer than five pages is logged at `Info`
with its page count and wall time.

Environment variables (defaults live in code; no values file needs to set them):

| Variable | Default | Meaning |
| --- | --- | --- |
| `ACCESS_CHECK_TIMEOUT` | `15s` | Timeout of each batched fga-sync access check (search, count and summary routes) |
| `READ_TUPLES_TIMEOUT` | `15s` | Timeout of the `filter_grants=direct` tuple read |
| `COUNT_ACCESS_BUCKET_PAGE` | `100` | Access-key buckets fetched and checked per page (1–1000) |
| `SEARCH_DENIED_PAGE_WALK` | `10` | Extra raw pages `/query/resources` fetches when a page leaves the caller no visible resource (1–25); see [Page Size](#page-size) |
| `COUNT_MAX_ACCESS_BUCKETS` | `5000` | Access-key walk cap (page size..10000, validated at startup); at most 100 pages per count (`ceil(cap/page) <= 100`); a full page can overshoot by at most page size minus one |
| `SUMMARY_MAX_RECORDS` | `5000` | Membership records read by `GET /query/memberships/summary` before it folds what it has and reports `complete: false` (1..50000, validated at startup); the cap is checked after a whole page, so it can be overshot by up to one page, and while the caller has seen nothing it yields to the denied-page walk, so the worst case for such a read is the larger of the cap rounded up to whole pages (`ceil(cap/page)`) and one full page more than `SEARCH_DENIED_PAGE_WALK` |

#### Not supported

- `sum:<field>`, `avg`, or any metric or `group_by` over `data.*`: `data` is a
  `flat_object` and cannot be aggregated (see below). `400` names the reason.
- A metric per group, `group_by` on two prefixes, paging of groups beyond
  `group_by_size`.

### Mapping the count route depends on

No repository owns an index template for `resources`; the only mapping text in
the workspace is the fallback in `lfx-v2-mockdata/scripts/reset-data.sh`, used
when that script cannot read an existing index. The count route depends on
three facts about the live mapping:

| Field | Required mapping | Why |
| --- | --- | --- |
| `tags` | `keyword` | The **only aggregatable dimension**. `group_by` and `metric=cardinality:` both aggregate on it; a tag prefix is the way to expose a groupable attribute |
| `data` | `flat_object` | Never aggregatable, never summable, no numeric operations. This is why `sum` and `group_by` on `data.*` are declined rather than attempted |
| `access_check_query` | `keyword`, **or** `text` with a `keyword` subfield | The access-bucket walk aggregates on it. The searcher reads `GET /<index>/_mapping` and resolves **every** backing index to `access_check_query` (keyword) or `access_check_query.keyword` (text + keyword subfield). Only agreement across supported mappings is cached; `Info` is logged on initial resolution or when the resolved field changes. An unsupported shape (including a missing field), disagreeing alias targets, or a failed read **fails closed**: authenticated counts answer `503` and a warning identifies the mapping problem; resolution is retried after 30 seconds. Anonymous public counts and aggregations do not read the mapping and are unaffected. Guessing the field would return the public count as if exhaustive on a plain-keyword index — the silent zero this route exists to remove |

The resolution is revalidated every 5 minutes; a failed, unsupported, or
disagreeing revalidation fails closed like the first read instead of reusing
an expired field. This bounds stale-mapping time but is not a transactional
snapshot of alias changes.

The mockdata fallback mapping declares `access_check_query` as plain `keyword`;
an index whose field was created by dynamic mapping carries `text` +
`.keyword`. Before this resolution the field name was hardcoded to
`.keyword`, and on a plain-`keyword` index the aggregation returned zero
buckets with HTTP 200, so every authenticated count silently equalled the
public count. If `access_check_query` is mapped `text` **without** a `keyword`
subfield, or is absent, authenticated counts now return `503` with the warning
`access_check_query mapping unsupported`. This supersedes QS-1 spec §3.5's
original fallback-plus-warning rule: guessing an unmapped subfield must never
turn private resources into a successful public-only count. Plain `keyword`
(the mockdata fallback mapping) remains supported.

### GET /query/memberships/summary

Summarizes the membership records (`project_membership`) of an organization, a
project, or both into one summary per organization and project.

The summary is a fold, not an aggregation: status, tier name and the membership
dates live in `data`, which is a `flat_object` and therefore never aggregatable
(see [Mapping the count route depends on](#mapping-the-count-route-depends-on)).
The route reads the matching records page by page and folds them in process, so
callers do not have to drain every page and fold the history themselves.

| Parameter | Type | Description |
| --- | --- | --- |
| `v` | string (required) | API version, must be `1` |
| `project_uid` | string | Summarize the memberships on this project |
| `b2b_org_uid` | string | Summarize the memberships of this organization |
| `page_token` | string | Continue an earlier read of the same scope from the organization it stopped before (see the record cap below) |

At least one of `project_uid` and `b2b_org_uid` must be provided; a request with
neither is a `400 Bad Request` naming both ("at least one summary parameter must
be provided: project_uid or b2b_org_uid"). Given together they restrict the read
to the memberships of that organization on that project. There are no other
filters. The read is whole by definition; a read that stops at the record cap
returns a `page_token`, and passing it back with the same `project_uid` and
`b2b_org_uid` continues where it stopped. A token
passed with another scope is a `400 Bad Request`.

**Response**:

```json
{
  "summaries": [
    {
      "b2b_org_uid": "org-1",
      "company_name": "Example Corp",
      "project_uid": "proj-1",
      "project_slug": "example-project",
      "term_count": 2,
      "first_start": "2023-01-01T00:00:00Z",
      "last_end": "2025-01-01T00:00:00Z",
      "current_status": "Active",
      "current_tier_name": "Gold Membership",
      "current_start": "2024-01-01T00:00:00Z",
      "current_end": "2025-01-01T00:00:00Z",
      "current_membership_uid": "m-2",
      "tier_names": ["Silver Membership", "Gold Membership"],
      "statuses": ["Expired", "Active"],
      "terms": [
        {
          "membership_uid": "m-1",
          "status": "Expired",
          "tier_name": "Silver Membership",
          "start_date": "2023-01-01T00:00:00Z",
          "end_date": "2024-01-01T00:00:00Z"
        },
        {
          "membership_uid": "m-2",
          "status": "Active",
          "tier_name": "Gold Membership",
          "tier": "Gold",
          "start_date": "2024-01-01T00:00:00Z",
          "end_date": "2025-01-01T00:00:00Z"
        }
      ]
    }
  ],
  "terms_total": 2,
  "complete": true
}
```

| Field | Present | Meaning |
| --- | --- | --- |
| `summaries` | always | One entry per organization and project, ordered by company name, project slug, organization UID and project UID. Empty when nothing matched or nothing was visible |
| `terms_total` | always | Membership records folded into the summaries |
| `complete` | always | `true` when every matching record was read; `false` when the read stopped at the record cap, so the summaries cover part of the history and `page_token` continues it |
| `page_token` | when the read stopped at the record cap | Opaque token; pass it back with the same scope to continue the read where it stopped, at the next organization or inside the one run that filled the read. Absent when the read is complete |
| `cache_control` | anonymous callers | Response header, as on the other reads (see [Anonymous vs Authenticated Requests](#anonymous-vs-authenticated-requests)) |

Fields of one summary:

| Field | Present | Meaning |
| --- | --- | --- |
| `b2b_org_uid` / `project_uid` | always | Identifiers of the pair; empty when the records carry none |
| `company_name` / `project_slug` | always | Labels as stored on the current record |
| `term_count` | always | Membership records folded into this summary |
| `first_start` / `last_end` | when a record carries one | Earliest start date and latest end date across the records |
| `current_status`, `current_tier_name`, `current_start`, `current_end`, `current_membership_uid` | when the current record carries one | Attributes of the current record (see the fold rules below); each is omitted when there is no current record and when the current record carries no such value |
| `tier_names` / `statuses` | always | Distinct tier product names and statuses in first-appearance order |
| `terms` | always | The membership records themselves, oldest first: `membership_uid`, `status`, `tier_name`, `tier` (the tier label, omitted when the record has none), `start_date`, `end_date` (each omitted when the record carries none) |

Dates are returned exactly as stored on the record; the route neither parses nor
normalizes them.

#### How a summary is computed

1. **Read** — the scope becomes a search for `type=project_membership` carrying
   the `project_uid:` and `b2b_org_uid:` tags requested (the index tags rather
   than `data` filters: the same keyword terms the `project_membership`
   catalog recipes use, and the cheapest scope for the read), in whole pages,
   in organization order: the records are sorted on the record's sortable
   name, which the member service indexes as the company name lowercased (see
   the member-service indexer contract), with the record id as tiebreaker, so
   the records of one organization are read together.
   The route continues from the keyset cursor of the previous page until a
   page carries none. A record served on more than one
   page, because it was re-indexed while the read was between pages, is folded
   once, as the copy read last: that is the one the re-index wrote.
2. **Visibility** — identical to the plain search: each page is access-checked
   in one batched fga-sync request, built and matched exactly as
   [Access Control Flow](#access-control-flow) describes, and only the records
   the caller may see reach the fold. A failed access check is a `503`, never a
   partial summary: a summary is never returned as if whole while part of the
   caller's visibility is unknown. While the caller has seen nothing, the read
   keeps walking past the record cap as far as the plain search walks denied
   pages (`SEARCH_DENIED_PAGE_WALK`) before it exposes a continuation, so a
   scope the caller cannot see and a scope that does not exist stay
   indistinguishable to the same extent as on the plain search.
3. **Record cap** — the read stops at a configured record cap
   (`SUMMARY_MAX_RECORDS`, validated at startup like the count route's bucket
   cap) and reports `complete: false`. The cap is checked after a whole page,
   so pages are never split and the cap can be overshot by up to one page;
   while the caller has seen nothing the cap yields to the denied-page walk
   described above, so the worst case for such a read is the larger of the
   cap rounded up to whole pages and one full page more than
   `SEARCH_DENIED_PAGE_WALK`.
   Because the records arrive in organization order, the read then folds every
   organization it has read whole, leaves out the organization it stopped
   inside (its records may continue on the next page), and returns a
   `page_token` holding the cursor at that organization; the next read with
   the same scope and that token starts with it. The boundary is found over
   every record read, visible or not, so a caller who cannot see the last
   organization still resumes at the right place. A run is the records that
   share one sortable name, so it is usually one organization, but
   organizations sharing a company name form one run and are cut and resumed
   together. When the whole read fell inside a single run there is no
   boundary to cut at: rather than strand the organizations that sort after
   it, the read keeps the run as far as it was read, which may be more than
   one summary, and returns a token that continues inside it, so that run
   may go on in the next read and its summaries then continue there. An
   organization whose records carry company names that differ after
   lowercasing sorts as more than one run and can be split across reads into
   more than one summary; a record without a sortable name sorts last. A
   continued read is not a snapshot: like the pages of the plain search, each
   call queries the live index, so a record re-indexed under another company
   name between two calls can appear in both or in neither. A caller that
   needs an exact roster across such a change re-reads it.
4. **Fold** — the visible records are grouped and reduced (below). `terms_total`
   counts the records that were folded, not the records that were read.

#### Fold rules

- **Grouping.** A record folds under its organization UID, or under its company
  name when it carries no organization UID, and under its project UID, or under
  its project slug when it carries no project UID. Labels are trimmed and
  case-folded so one organization or project keeps one summary. A record with
  neither identifier nor label on a side folds under the empty value: no record
  is dropped for want of an identifier. This matters because a membership on a
  project that is not onboarded in LFX v2 carries a slug but no project UID.
- **Order within a group.** Records are ordered oldest first by start date, then
  creation date, then record UID, all compared as the strings they are stored
  as.
- **Dates.** `first_start` is the earliest start date a record carries,
  `last_end` the latest end date; records without one are skipped rather than
  treated as empty.
- **Current record.** The latest-starting record whose status is active, or the
  latest-starting record when none is active. It supplies the `current_*` fields
  and the `company_name` and `project_slug` labels of the summary.
- **Tier names and statuses.** Distinct values in first-appearance order, empty
  values skipped.

For an anonymous caller the read runs with the `public: true` filter, as every
read does. Membership records are indexed private, so an anonymous caller
receives an empty `summaries` list with `complete: true` and the anonymous
`Cache-Control` header set.

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
2. Builds a batch access check message, one line per unique
   `access_check_object#access_check_relation` key among non-public resources
   (resources that share a key, e.g. meeting registrants sharing their parent
   meeting, collapse to a single line):

   ```text
   committee:abc-123#viewer@user:alice
   project:xyz-789#viewer@user:alice
   ```

   (format: `{access_check_object}#{access_check_relation}@user:{principal}`)
3. Sends to fga-sync via NATS request/reply:
   - Subject: `lfx.access_check.request`
   - Timeout: `ACCESS_CHECK_TIMEOUT` (default 15 seconds)
4. Parses the tab-separated response:

   ```text
   committee:abc-123#viewer@user:alice\ttrue
   project:xyz-789#viewer@user:alice\tfalse
   ```

5. Drops any resource where the response is `false` or missing

The query-service deduplicates by `access_check_object#access_check_relation`, not by
`object_ref`, so each distinct FGA object/relation pair is checked at most once per request
regardless of how many resources share it.

### Direct grant filtering

`filter_grants=direct` narrows a query to resources where the authenticated user
has direct OpenFGA tuples for the requested `type`.

- Requires `type`, because fga-sync reads tuples by object type.
- Requires an authenticated principal. Anonymous requests fail validation.
- Calls `lfx.access_check.read_tuples` through fga-sync (timeout
  `READ_TUPLES_TIMEOUT`, default 15 seconds), then pre-filters OpenSearch by
  the returned `object_ref` values.
- The normal access-check pass still runs after OpenSearch returns resources.
- This is a direct-grant filter only. It does not expand inherited permissions
  through parent projects or committees.

## OpenSearch Document Fields Used by Query Service

These fields must be correctly populated by the indexer (via `IndexingConfig`) for a
resource to be discoverable and accessible:

| Field | Purpose | If missing/wrong |
| --- | --- | --- |
| `object_ref` | Stable `{type}:{id}` ref used for `filter_grants=direct` pre-filtering | Direct-grant filters can behave incorrectly |
| `object_type` | Type filtering (`type=` param) | Resource won't match type queries |
| `object_id` | Debug lookup and warning-log context for the source resource ID | Harder to trace an indexed document back to the owning resource |
| `parent_refs` | Parent filtering (`parent=` param) | Resource won't appear in parent queries |
| `name_and_aliases` | Typeahead search (`name=` param) | Resource won't appear in name searches |
| `tags` | Tag filtering; the only aggregatable dimension (`group_by`, `metric=cardinality:`) | Resource won't match tag queries or appear in grouped counts |
| `public` | Marks the resource as public: skips the FGA check for all callers (`BuildMessage` returns it without checking when `public` is true) and lets anonymous callers see it via the `public: true` filter | Anonymous users can't see it; setting it incorrectly exposes a private resource to everyone |
| `access_check_object` | Identifies FGA object to check | Non-public resource treated as unauthorized and excluded from results (legacy/malformed document) |
| `access_check_relation` | FGA relation to check (e.g. `viewer`) | Non-public resource treated as unauthorized and excluded from results (legacy/malformed document) |
| `access_check_query` | `{access_check_object}#{access_check_relation}`; the count route's access-bucket walk aggregates on it (field resolved from the mapping, see [Mapping the count route depends on](#mapping-the-count-route-depends-on)) | Authenticated counts can undercount private resources |
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

## Name Search

The `name` parameter runs a `multi_match` of type `bool_prefix` with
`operator: and` against `name_and_aliases` and its `_2gram` / `_3gram` shingle
subfields. Every term in the query must match the document; the final term
matches as a prefix so the field behaves as a typeahead while the user types.

Because terms are AND-ed, adding a word can never broaden the result set. The
matches for `Cloud Native Computing` are a subset of those for `Cloud Native` —
usually a smaller one, and the same set when every existing match happens to
contain the added term too.

Two consequences are worth knowing before you build on this:

- **Pass `sort=best_match` when you care about ordering.** `sort` defaults to
  `name_asc`, which maps to `sort_name: asc`. When OpenSearch receives an
  explicit non-`_score` sort it discards relevance entirely, so a caller that
  passes `name` without `sort` gets alphabetical results and the closest match
  can land on any page.
- **There is no synonym or acronym expansion.** Matching is limited to the
  strings a resource service puts in `name_and_aliases`. An acronym resolves
  only when the owning service indexes it as an alias.

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
- A raw page that shrinks to **zero** visible results is walked rather than
  returned with a token: the service follows the raw `search_after` cursor for up to
  `SEARCH_DENIED_PAGE_WALK` further pages (default 10) until the caller can
  see a resource or the result set is exhausted. Exhausted ⇒ `[]` with no
  token — identical to a query that matches nothing, so within the walk `page_token`
  presence reveals nothing about resources the caller may not read. Limit reached ⇒
  `[]` with the token, so a caller with sparse access can continue; the token then
  reveals only that further raw pages exist. Each extra page is
  one OpenSearch query plus one access-check batch, so a query whose first
  pages are entirely invisible costs up to `1 + SEARCH_DENIED_PAGE_WALK` round
  trips.
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
is absent (see [Page Size](#page-size)). A raw page the expression empties
entirely is walked server-side like a fully denied one (up to
`SEARCH_DENIED_PAGE_WALK` pages), so the caller sees the next page with a match
rather than an empty page. The trade-off is that a CEL expression which
significantly reduces a page can yield short pages and extra round trips, so it
is not a substitute for narrowing the OpenSearch query itself. Always pair `cel_filter`
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
- Anonymous queries and count subqueries add public/private clauses as needed;
  the count route's grouped/metric search adds one `terms` clause holding the
  granted access keys (fewer than `COUNT_MAX_ACCESS_BUCKETS + COUNT_ACCESS_BUCKET_PAGE` values because full pages are never split).
- `filter_grants=direct` adds one `object_ref` terms clause populated from
  fga-sync's direct tuple response.

Error handling implementation: `internal/infrastructure/opensearch/client.go`
detects `opensearch.StructError` where any `RootCause` entry has
`Type == "too_many_nested_clauses"` (OpenSearch wraps it inside a
`search_phase_execution_exception`) and converts it to `errors.Validation` so
`wrapError()` in `cmd/service/error.go` maps it to HTTP 400. Applied
consistently across `Search()`, `AggregationSearch()`, and `Count()`.
