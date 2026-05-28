# Resource Catalog

This document is the index of all resource types searchable via the Query
Service, organized by the service that indexes them, and a cookbook of common
query patterns.

For the NATS subjects and source files where each type is published, see
[`docs/indexed-data-types.md`](indexed-data-types.md).

Each service owns its indexer contract, which is the authoritative reference
for data schemas, tags, access control, and parent references for its resource
types. When a resource type changes, only that service's contract needs
updating.

---

## Services

| Service | Resource Types | Indexer Contract |
|---|---|---|
| [lfx-v2-project-service](https://github.com/linuxfoundation/lfx-v2-project-service) | Project, Project Settings, Project Link, Project Folder, Project Document | [indexer-contract.md](https://github.com/linuxfoundation/lfx-v2-project-service/blob/main/docs/indexer-contract.md) |
| [lfx-v2-committee-service](https://github.com/linuxfoundation/lfx-v2-committee-service) | Committee, Committee Settings, Committee Member, Committee Invite, Committee Application, Committee Document, Committee Link, Committee Link Folder | [indexer-contract.md](https://github.com/linuxfoundation/lfx-v2-committee-service/blob/main/docs/indexer-contract.md) |
| [lfx-v2-meeting-service](https://github.com/linuxfoundation/lfx-v2-meeting-service) | V1 Meeting, V1 Meeting Registrant, V1 Meeting RSVP, V1 Meeting Attachment, V1 Past Meeting, V1 Past Meeting Participant, V1 Past Meeting Recording, V1 Past Meeting Transcript, V1 Past Meeting Summary, V1 Past Meeting Attachment | [indexer-contract.md](https://github.com/linuxfoundation/lfx-v2-meeting-service/blob/main/docs/indexer-contract.md) |
| [lfx-v2-mailing-list-service](https://github.com/linuxfoundation/lfx-v2-mailing-list-service) | Groups.io Service, Groups.io Service Settings, Groups.io Mailing List, Groups.io Mailing List Settings, Groups.io Member, Groups.io Artifact | [indexer-contract.md](https://github.com/linuxfoundation/lfx-v2-mailing-list-service/blob/main/docs/indexer-contract.md) |
| [lfx-v2-voting-service](https://github.com/linuxfoundation/lfx-v2-voting-service) | Vote, Vote Response | [indexer-contract.md](https://github.com/linuxfoundation/lfx-v2-voting-service/blob/main/docs/indexer-contract.md) |
| [lfx-v2-survey-service](https://github.com/linuxfoundation/lfx-v2-survey-service) | Survey, Survey Response, Survey Template | [indexer-contract.md](https://github.com/linuxfoundation/lfx-v2-survey-service/blob/main/docs/indexer-contract.md) |

---

## Not Currently Queryable

`lfx-v2-member-service` has architecture notes for member-domain indexed types,
but its current implementation does not publish `lfx.index.*` messages. Do not
query `membership_tier`, `project_membership`, `key_contact`, or `b2b_org`
through this service until member-service lands real indexer publishing code and
an indexer contract.

## Adding a New Service

When a new service starts indexing data:

1. Add a `docs/indexer-contract.md` to that service's repo following the [committee-service pattern](https://github.com/linuxfoundation/lfx-v2-committee-service/blob/main/docs/indexer-contract.md)
2. Add a row to the table above with the service name, resource types, and a link to its contract

---

## Common Query Patterns

The examples below use `/query/resources`. All requests require `v=1` and a
Heimdall principal. Authenticated principals receive FGA-filtered results;
`_anonymous` principals only receive documents indexed with `public: true`.

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
GET /query/resources?v=1&type=committee_member&tags_all=committee_uid:<committee_uid>&tags_all=voting_status:Voting Rep
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
# Find committees in a specific category. Anonymous callers only see public committees.
GET /query/resources?v=1&type=committee&tags=project_uid:<project_uid>&cel_filter=data.category=="TSC"
```

### Find resources with direct grants

```bash
GET /query/resources?v=1&type=committee&filter_grants=direct
```

### Find a project by slug

```bash
GET /query/resources?v=1&type=project&tags=project_slug:<slug>
```

### Find project documents for a project

```bash
GET /query/resources?v=1&type=project_document&tags=project_uid:<project_uid>
```

### Find committee documents for a committee

```bash
GET /query/resources?v=1&type=committee_document&tags=committee_uid:<committee_uid>
```

### Find committee links in a folder

```bash
GET /query/resources?v=1&type=committee_link&tags=folder_uid:<folder_uid>
```

### Find all meetings for a project

```bash
GET /query/resources?v=1&type=v1_meeting&tags=project_uid:<project_uid>
```

### Find past meetings for an active meeting (all occurrences)

```bash
GET /query/resources?v=1&type=v1_past_meeting&tags=meeting_id:<meeting_id>
```

### Find participants of a past meeting

```bash
GET /query/resources?v=1&type=v1_past_meeting_participant&tags=meeting_and_occurrence_id:<meeting_and_occurrence_id>
```

### Find attendees of a past meeting

```bash
GET /query/resources?v=1&type=v1_past_meeting_participant&tags_all=meeting_and_occurrence_id:<meeting_and_occurrence_id>&tags_all=is_attended:true
```

### Find all mailing lists for a project

```bash
GET /query/resources?v=1&type=groupsio_mailing_list&tags=project_uid:<project_uid>
```

### Find members of a mailing list

```bash
GET /query/resources?v=1&type=groupsio_member&tags=mailing_list_uid:<mailing_list_uid>
```

### Find votes for a committee

```bash
GET /query/resources?v=1&type=vote&tags=committee_uid:<committee_uid>
```

### Find responses for a vote

```bash
GET /query/resources?v=1&type=vote_response&tags=vote_uid:<vote_uid>
```

### Find surveys for a project

```bash
GET /query/resources?v=1&type=survey&tags=project_uid:<project_uid>
```

### Find surveys for a committee

```bash
GET /query/resources?v=1&type=survey&tags=committee_uid:<committee_uid>
```

### Find responses for a survey

```bash
GET /query/resources?v=1&type=survey_response&tags=survey_uid:<survey_uid>
```

For the full list of queryable fields and tags per resource type, refer to the service's indexer contract linked in the table above.
