# Resource Catalog

This document is the index of all resource types searchable via the Query Service, organized by the service that indexes them.

Each service owns its indexer contract — the authoritative reference for data schemas, tags, access control, and parent references for its resource types. When a resource type changes, only that service's contract needs updating.

---

## Services

| Service | Resource Types | Indexer Contract |
|---|---|---|
| [lfx-v2-committee-service](https://github.com/linuxfoundation/lfx-v2-committee-service) | Committee, Committee Settings, Committee Member, Committee Link, Committee Link Folder | [indexer-contract.md](https://github.com/linuxfoundation/lfx-v2-committee-service/blob/main/docs/indexer-contract.md) |

---

## Adding a New Service

When a new service starts indexing data:

1. Add a `docs/indexer-contract.md` to that service's repo following the [committee-service pattern](https://github.com/linuxfoundation/lfx-v2-committee-service/blob/main/docs/indexer-contract.md)
2. Add a row to the table above with the service name, resource types, and a link to its contract

---

## Common Query Patterns

The examples below use `/query/resources`. All requests require `v=1` and a valid JWT token.

### Find all committees for a project

```bash
GET /query/resources?v=1&type=committee&tags=project_uid:<project_uid>
```

### Find all members of a committee

```bash
GET /query/resources?v=1&type=committee_member&tags=committee_uid:<committee_uid>
```

### Find voting members of a committee

```bash
GET /query/resources?v=1&type=committee_member&tags=committee_uid:<committee_uid>&tags=voting_status:Voting Rep
```

### Find child committees of a parent committee

```bash
GET /query/resources?v=1&type=committee&tags=parent_uid:<parent_uid>
```

### Find members by organization

```bash
GET /query/resources?v=1&type=committee_member&tags=organization_name:<org_name>
```

### Advanced filtering with CEL

```bash
# Find public committees in a specific category
GET /query/resources?v=1&type=committee&tags=project_uid:<project_uid>&cel_filter=data.category=="TSC"&&data.public==true
```

For the full list of queryable fields and tags per resource type, refer to the service's indexer contract linked in the table above.
