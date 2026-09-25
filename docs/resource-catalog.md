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
| [lfx-v2-member-service](https://github.com/linuxfoundation/lfx-v2-member-service) | B2B Org, B2B Org Settings, Project Membership, Key Contact | [indexer-contract.md](https://github.com/linuxfoundation/lfx-v2-member-service/blob/main/docs/indexer-contract.md) |

---

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

### Count past meetings per project over a window

```bash
# past meetings held between 1 June and 31 August, grouped by project
GET /query/resources/count?v=1&type=v1_past_meeting&date_field=start_time&date_from=2026-06-01&date_to=2026-08-31&group_by=project_uid
```

`start_time` is the occurrence's start, so the window is "when the meeting
happened". The count is computed only over the meetings the caller may see.
`has_more` is `true` when the count is not guaranteed exhaustive: the access-bucket
walk stopped at `COUNT_MAX_ACCESS_BUCKETS`, or OpenSearch returned a full page
without a continuation cursor (logged as a warning).
`group_by` and `metric` cannot be combined: call
`group_by` once, then `metric` per group with `tags=project_uid:<uid>`. See
[Mapping the count route depends on](query-service-contract.md#mapping-the-count-route-depends-on)
for why only tag prefixes can be grouped on.

### Count distinct attendees of one past meeting

```bash
GET /query/resources/count?v=1&type=v1_past_meeting_participant&tags_all=meeting_and_occurrence_id:<meeting_and_occurrence_id>&tags_all=is_attended:true&metric=cardinality:email
```

Do **not** date-filter participant records by `created_at` to mean "attended in
a window": a participant record's `created_at` is when the record was written,
not when the meeting happened. Scope participants by meeting (as above) or by
`project_uid:` tag instead; the meeting's start time is not on the participant
document today.

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

### Find memberships for a project

```bash
GET /query/resources?v=1&type=project_membership&tags=project_uid:<project_uid>
```

### Find memberships by status

`status` is a `data` field on `project_membership` (not a tag), so combine a
tag filter with a `filters` clause.

```bash
GET /query/resources?v=1&type=project_membership&tags=project_uid:<project_uid>&filters=status:Active
```

### Find memberships for a company

```bash
GET /query/resources?v=1&type=project_membership&tags=b2b_org_uid:<b2b_org_uid>
```

### Summarize a membership history per organization and project

```bash
# every organization on one project
GET /query/memberships/summary?v=1&project_uid=<project_uid>
# every project of one organization
GET /query/memberships/summary?v=1&b2b_org_uid=<b2b_org_uid>
# one organization on one project
GET /query/memberships/summary?v=1&project_uid=<project_uid>&b2b_org_uid=<b2b_org_uid>
```

One summary per organization and project, folded from the membership records:
how many records, the earliest start and the latest end, the current record's
status, tier and dates, the tier names and statuses seen, and the records
themselves oldest first. At least one of `project_uid` and `b2b_org_uid` is
required.

Use this instead of paging `type=project_membership` and folding the history in
the client: `status`, `tier_name` and the dates are `data` fields, which cannot
be aggregated, so the fold has to happen over the records themselves. The
summaries cover only the records the caller may see. `complete` is `false`
when the read stopped at an organization boundary after reaching the configured
record cap; it then carries a `page_token` that resumes at the next run with
the same scope. The read groups an organization's records by its parent ref,
whatever company name they carry; organization-less records are read with
those of the same project, and ref-less records form the last run. If the cap
falls inside the first run, the read continues until a boundary appears or the
pages run out, returning whole runs rather than partial summaries. If neither a resumable
boundary nor end-of-results can be established within the hard ceiling of
50000 raw hits, the read fails with `503` and no summaries.

A `page_token` is accepted only from the same version of the summary read; a
token from an earlier version is rejected with `400` and the read must restart
without it.
See [GET /query/memberships/summary](query-service-contract.md#get-querymembershipssummary)
for the parameters, the result fields and the fold rules.

### Find key contacts for a membership

```bash
GET /query/resources?v=1&type=key_contact&tags=project_membership_uid:<membership_uid>
```

### Find which orgs a user has access to

The `b2b_org_settings` `member:` tag covers both writer and auditor roles.

```bash
GET /query/resources?v=1&type=b2b_org_settings&tags=member:<auth0|username>
```

For the full list of queryable fields and tags per resource type, refer to the service's indexer contract linked in the table above.
