<!-- Copyright The Linux Foundation and each contributor to LFX. -->
<!-- SPDX-License-Identifier: MIT -->

# Retained resource history query contract

`GET /query/resources/history?v=1` is a generic, authenticated read of immutable
mutation events from the indexer's private `resource-history-v1` index. It does
not query `resources`, use `latest:true`, or return current resource projections.
No assertion of history completeness is made until a publisher actually enables
the version 2 audited outbox and records its coverage start time. Previously
overwritten revisions cannot be reconstructed. This endpoint neither changes
FGA tuples on deletion nor defines a durable deleted-resource grant.

## Request

| Parameter | Meaning |
| --- | --- |
| `v` | Required; `1`. |
| `type` | Optional exact resource type (`object_type`). |
| `parent` | A resource reference (`type:uid`). Matches the referenced resource's own events (`object_type` + `object_id`) **or** events whose recorded `scope.before` or `scope.after` contains the reference. Can be combined with `type` to narrow the results. Unlike `/query/resources`, history `parent` includes the referenced resource itself. |
| `filters`, `filters_all` | `field:value` terms with AND semantics. V1 supports `action:created`, `action:updated`, `action:deleted`, and `object_id:<uid>`. Combine `type` with `object_id` for exact-resource history. |
| `filters_or` | The same allowed field terms with OR semantics, AND-ed with other request clauses. |
| `date_field` | Only `occurred_at` (publisher source time); optional without bounds, as in resource search. |
| `date_from`, `date_to` | Inclusive RFC3339 bounds with at most millisecond precision, or date-only bounds on `date_field`. Dates without a field, malformed inputs and sub-millisecond bounds return 400 without echoing the supplied value. Date-only `date_from` starts at 00:00:00 UTC; date-only `date_to` includes the final millisecond, 23:59:59.999 UTC, supported by the history index's `date` mapping. Unlike current-resource search, the final fraction of that day is included. |
| `page_size` | Raw history hits per page, 1–100, default 50. |
| `page_token` | Opaque continuation bound to **all filters, page size, version and principal**. A mismatched, malformed, or foreign-endpoint token returns 400. |

Millisecond fractions in RFC3339 bounds are preserved, not rounded down to
whole seconds. Finer precision cannot be evaluated exactly by the history
index and is rejected, not silently truncated. These rules are scoped to
history reads; current-resource date parsing is unchanged.

Require at least one of `type` or `parent`. There is no `id`, `tags`,
`tags_all`, `scope_ref`, `action`, `occurred_from` or `occurred_to` parameter. No broad
index-wide listing, arbitrary `data.*`/`audit.*` filtering, CEL filtering,
direct-grants filtering, name search, history counts, or sorting
choice is supported. Filter expressions are whitelisted before constructing
OpenSearch queries, not interpolated into field names.

Examples using **existing** indexed history fields:

```text
# Exact-resource timeline:
GET /query/resources/history?v=1&type=committee_member&filters=object_id:member-uuid

# Combined committee/settings/member timeline:
GET /query/resources/history?v=1&parent=committee:committee-uuid

# Source-time and action filtering:
GET /query/resources/history?v=1&parent=committee:committee-uuid&filters=action:updated&date_field=occurred_at&date_from=2026-11-01&date_to=2026-11-09
```

## Response and pagination

```json
{
  "events": [{
    "event_id": "committee-service:event-123",
    "resource": {"type": "committee_member", "uid": "member-uuid"},
    "action": "updated",
    "occurred_at": "2026-11-09T14:30:00Z",
    "resource_revision": 4,
    "attribution": {
      "actor_ref": "service:example",
      "effective_actor_ref": "service:example"
    },
    "content": {
      "schema": "example.audit.v1",
      "representation": "snapshots",
      "before": {"name": "Earlier"},
      "after": {"name": "Later"}
    }
  }],
  "page_token": "opaque-if-more-raw-hits"
}
```

Only mutation events have history records. The response includes the indexer's
approved captured actor refs/profiles (name, avatar URL, email, username, with
explicit nulls for unavailable attributes) and publisher-allowlisted snapshots.
Creates have only `after`; deletes only `before`. Never resolve actor profiles
against live accounts or return transport headers, relay JWTs, `fingerprint`,
ingestion time, private history-check fields, or raw current-resource `data`.
Erased events are excluded. Render captured strings as text, not trusted HTML;
profile email is private history data, not a public-search field.

Sort by `occurred_at` ascending then `publisher` and `event_id` ascending.
Source time is not necessarily ingestion order. Equal timestamps remain distinct;
revisions order only events **for the same resource**. Keyset pagination follows
the raw last hit even when access filtering shrinks a page. An entirely denied
raw page is walked for up to `1 + SEARCH_DENIED_PAGE_WALK` pages to reduce the
existence oracle, as on `/query/resources`. After the walk cap, an empty page
may carry a token; clients continue until it is absent. Reads are not a
point-in-time snapshot; grants are rechecked on every request.

## Authorization, failure and cache policy

Heimdall's history route requires OIDC (no anonymous authenticator). The
service independently rejects an anonymous principal. It batches distinct
`history_check_object#history_check_relation@user:<principal>` requests to
`lfx.access_check.request` and matches replies by token, not reply order.
**Every event** is classified: neither a public current resource nor a
`viewer` grant nor the recorded actor identity skips the history check.
Missing or malformed metadata, a `viewer` history relation, absent replies and
denied grants hide the event. Publishers must supply a history-only relation;
the generic reader never accepts an ordinary viewer grant as history policy.
OpenSearch partial results, failed FGA requests, and malformed private history
documents fail the whole request (503), never report a smaller complete page.
The body is always `Cache-Control: private, no-store`; debug payload/body
logging is not installed, and HTTP request tracing is disabled on this route
until URL query attributes can be scrubbed. Invalid parameters/tokens return 400.

This uses current grants and thus reflects revocation on the next read. It
does not infer access from the stored scope, grant a project auditor based on a
matching parent, or special-case deleted resources: a check is evaluated against
whatever FGA state remains. A producer deciding to move a resource between
projects must choose the event's history authorization metadata according to
its approved policy; the query service cannot infer that policy from scope.

## Deployment and producer handoff

The indexer's [`resource-history-v1` mapping and `HistoryRecord`](https://github.com/linuxfoundation/lfx-v2-indexer-service/blob/main/docs/audit-history-design.md)
already index `object_type`, `object_id`, `scope.before`, and `scope.after`.
No history tag field or indexer mapping change is required. The publisher must
include `committee:<uid>` in the **before and/or after audit scope** of settings
and member events for the combined `parent=committee:<uid>` timeline; the
committee's own events match by resource identity. A move remains searchable
under both captured scopes but is authorized independently. The committee
settings publisher currently
records `committee_settings:<uid>#auditor`, but the OpenFGA model has no
`committee_settings` type; it must supply a valid history anchor (such as
`committee:<uid>#auditor`) before settings events can be read.

Provision a private reader role with access to `resource-history-v1` for this
service, separate from generic current-resource reader credentials. If the
cluster requires basic authentication, supply `OPENSEARCH_USERNAME` and
`OPENSEARCH_PASSWORD` together through a Kubernetes Secret (never plain chart
values); the same restricted identity must have current `resources` read access.
The Helm
rule is OIDC-only, but the service still checks every event. Set
`OPENSEARCH_HISTORY_INDEX` only when the index name differs from
`resource-history-v1`. The route may be deployed before an audited publisher,
but it cannot return history until the private index and reader role exist.
Verify publisher snapshot schemas/scope, authorization anchors, and outbox
coverage before claiming that any timeline is complete.
