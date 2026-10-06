// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package design

import (
	"goa.design/goa/v3/dsl"
)

var SortValues = []any{
	"name_asc",
	"name_desc",
	// Note, "created_at" sorting is not currently possible, because we only
	// include it on the "created" trasaction, to better distinguish it from an
	// "updated" transaction. Adding it would slow down the indexing service
	// (perhaps it could be asyncronously added by the janitor?) since
	// propogating attributes from earlier revisions is not currently supported.
	"updated_asc",
	"updated_desc",
	// best_match orders results by relevance score (descending). Most useful
	// for typeahead/name searches; with no name query, scores are uniform and
	// the ordering is effectively undefined.
	"best_match",
}

var Sortable = dsl.Type("Sortable", func() {
	dsl.Attribute("sort", dsl.String, "Sort order for results", func() {
		dsl.Enum(SortValues...)
		dsl.Default("name_asc")
		dsl.Example("updated_desc")
	})
	dsl.Attribute("page_token", dsl.String, "Opaque token for pagination", func() {
		dsl.Example("****")
	})
	dsl.Attribute("page_size", dsl.Int, "Number of results per page", func() {
		dsl.Minimum(1)
		dsl.Maximum(1000)
		dsl.Default(50)
		dsl.Example(20)
	})
})

var Resource = dsl.Type("Resource", func() {
	dsl.Description("A resource is a universal representation of an LFX API resource for indexing.")

	dsl.Attribute("type", dsl.String, "Resource type", func() {
		dsl.Example("committee")
	})
	dsl.Attribute("id", dsl.String, "Resource ID (within its resource collection)", func() {
		dsl.Example("123")
	})
	dsl.Attribute("data", dsl.Any, "Resource data snapshot", func() {
		dsl.Example(CommitteeExampleStub{
			ID:          "123",
			Name:        "My committee",
			Description: "a committee",
		})
	})
})

// CountGroup is one group of a grouped count.
var CountGroup = dsl.Type("CountGroup", func() {
	dsl.Description("One group of a grouped resource count.")

	dsl.Attribute("key", dsl.String, "Group key: the tag value with the group_by prefix stripped", func() {
		dsl.Example("a1b2c3d4")
	})
	dsl.Attribute("count", dsl.UInt64, "Number of authorized resources in the group", func() {
		dsl.Example(30)
	})
	dsl.Required("key", "count")
})

// MembershipTerm is one membership record of an organization on a project.
var MembershipTerm = dsl.Type("MembershipTerm", func() {
	dsl.Description("One membership record of an organization on a project.")

	dsl.Attribute("membership_uid", dsl.String, "Membership record UID", func() {
		dsl.Example("m-1")
	})
	dsl.Attribute("status", dsl.String, "Membership status as stored on the record", func() {
		dsl.Example("Active")
	})
	dsl.Attribute("tier_name", dsl.String, "Membership tier product name as stored on the record", func() {
		dsl.Example("Gold Membership")
	})
	dsl.Attribute("tier", dsl.String, "Membership tier label as stored on the record; omitted when the record has none", func() {
		dsl.Example("Gold")
	})
	dsl.Attribute("start_date", dsl.String, "Start date of the membership record; omitted when the record carries none", func() {
		dsl.Example("2023-01-01T00:00:00Z")
	})
	dsl.Attribute("end_date", dsl.String, "End date of the membership record; omitted when the record carries none", func() {
		dsl.Example("2024-01-01T00:00:00Z")
	})
	dsl.Required("membership_uid", "status", "tier_name")
})

// MembershipTermSummary is the folded membership history of one organization
// on one project.
var MembershipTermSummary = dsl.Type("MembershipTermSummary", func() {
	dsl.Description("Membership history of one organization on one project, folded from the membership records.")

	dsl.Attribute("b2b_org_uid", dsl.String, "Organization UID; empty when the records carry no organization UID", func() {
		dsl.Example("org-1")
	})
	dsl.Attribute("company_name", dsl.String, "Organization name as stored on the current record", func() {
		dsl.Example("Example Corp")
	})
	dsl.Attribute("project_uid", dsl.String, "Project UID; empty when the records carry no project UID", func() {
		dsl.Example("proj-1")
	})
	dsl.Attribute("project_slug", dsl.String, "Project slug as stored on the current record", func() {
		dsl.Example("example-project")
	})
	dsl.Attribute("term_count", dsl.UInt64, "Number of membership records folded into this summary", func() {
		dsl.Example(2)
	})
	dsl.Attribute("first_start", dsl.String, "Earliest start date across the records; omitted when no record has one", func() {
		dsl.Example("2023-01-01T00:00:00Z")
	})
	dsl.Attribute("last_end", dsl.String, "Latest end date across the records; omitted when no record has one", func() {
		dsl.Example("2025-01-01T00:00:00Z")
	})
	dsl.Attribute("current_status", dsl.String, "Status of the current record; omitted when there is no current record or the current record carries none", func() {
		dsl.Example("Active")
	})
	dsl.Attribute("current_tier_name", dsl.String, "Tier product name of the current record; omitted when there is no current record or the current record carries none", func() {
		dsl.Example("Gold Membership")
	})
	dsl.Attribute("current_start", dsl.String, "Start date of the current record; omitted when there is no current record or the current record carries none", func() {
		dsl.Example("2024-01-01T00:00:00Z")
	})
	dsl.Attribute("current_end", dsl.String, "End date of the current record; omitted when there is no current record or the current record carries none", func() {
		dsl.Example("2025-01-01T00:00:00Z")
	})
	dsl.Attribute("current_membership_uid", dsl.String, "UID of the current record; omitted when there is no current record or the current record carries none", func() {
		dsl.Example("m-2")
	})
	dsl.Attribute("tier_names", dsl.ArrayOf(dsl.String), "Distinct tier product names in first appearance order", func() {
		dsl.Example([]string{"Silver Membership", "Gold Membership"})
	})
	dsl.Attribute("statuses", dsl.ArrayOf(dsl.String), "Distinct statuses in first appearance order", func() {
		dsl.Example([]string{"Expired", "Active"})
	})
	dsl.Attribute("terms", dsl.ArrayOf(MembershipTerm), "Membership records of this organization on this project, oldest first", func() {
		// Declared so the schema example agrees with the term_count example
		// above; Goa would otherwise synthesize an array of its own length.
		dsl.Example([]dsl.Val{{
			"membership_uid": "m-1",
			"status":         "Expired",
			"tier_name":      "Silver Membership",
			"start_date":     "2023-01-01T00:00:00Z",
			"end_date":       "2024-01-01T00:00:00Z",
		}, {
			"membership_uid": "m-2",
			"status":         "Active",
			"tier_name":      "Gold Membership",
			"tier":           "Gold",
			"start_date":     "2024-01-01T00:00:00Z",
			"end_date":       "2025-01-01T00:00:00Z",
		}})
	})
	dsl.Required("b2b_org_uid", "company_name", "project_uid", "project_slug", "term_count", "tier_names", "statuses", "terms")
})

// membershipTermSummaryExample is the one summary every example of the read
// shows: two records of one organization on one project.
var membershipTermSummaryExample = dsl.Val{
	"b2b_org_uid":            "org-1",
	"company_name":           "Example Corp",
	"project_uid":            "proj-1",
	"project_slug":           "example-project",
	"term_count":             2,
	"first_start":            "2023-01-01T00:00:00Z",
	"last_end":               "2025-01-01T00:00:00Z",
	"current_status":         "Active",
	"current_tier_name":      "Gold Membership",
	"current_start":          "2024-01-01T00:00:00Z",
	"current_end":            "2025-01-01T00:00:00Z",
	"current_membership_uid": "m-2",
	"tier_names":             []string{"Silver Membership", "Gold Membership"},
	"statuses":               []string{"Expired", "Active"},
	"terms": []dsl.Val{{
		"membership_uid": "m-1",
		"status":         "Expired",
		"tier_name":      "Silver Membership",
		"start_date":     "2023-01-01T00:00:00Z",
		"end_date":       "2024-01-01T00:00:00Z",
	}, {
		"membership_uid": "m-2",
		"status":         "Active",
		"tier_name":      "Gold Membership",
		"tier":           "Gold",
		"start_date":     "2024-01-01T00:00:00Z",
		"end_date":       "2025-01-01T00:00:00Z",
	}},
}

// BadRequestError is the DSL type for a bad request error.
var BadRequestError = dsl.Type("BadRequestError", func() {
	dsl.Attribute("message", dsl.String, "Error message", func() {
		dsl.Example("The request was invalid.")
	})
	dsl.Required("message")
})

// NotFoundError is the DSL type for a not found error.
var NotFoundError = dsl.Type("NotFoundError", func() {
	dsl.Attribute("message", dsl.String, "Error message", func() {
		dsl.Example("The requested resource was not found.")
	})
	dsl.Required("message")
})

// InternalServerError is the DSL type for an internal server error.
var InternalServerError = dsl.Type("InternalServerError", func() {
	dsl.Attribute("message", dsl.String, "Error message", func() {
		dsl.Example("An internal server error occurred.")
	})
	dsl.Required("message")
})

// ServiceUnavailableError is the DSL type for a service unavailable error.
var ServiceUnavailableError = dsl.Type("ServiceUnavailableError", func() {
	dsl.Attribute("message", dsl.String, "Error message", func() {
		dsl.Example("The service is unavailable.")
	})
	dsl.Required("message")
})

var Organization = dsl.Type("Organization", func() {
	dsl.Description("An organization is a universal representation of an LFX API organization.")

	dsl.Attribute("name", dsl.String, "Organization name", func() {
		dsl.Example("Linux Foundation")
	})
	dsl.Attribute("domain", dsl.String, "Organization domain", func() {
		dsl.Example("linuxfoundation.org")
	})
	dsl.Attribute("industry", dsl.String, "Organization industry classification", func() {
		dsl.Example("Non-Profit")
	})
	dsl.Attribute("sector", dsl.String, "Business sector classification", func() {
		dsl.Example("Technology")
	})
	dsl.Attribute("employees", dsl.String, "Employee count or range", func() {
		dsl.Example("100-499")
	})
})

var OrganizationSuggestion = dsl.Type("OrganizationSuggestion", func() {
	dsl.Description("An organization suggestion for the search.")

	dsl.Attribute("name", dsl.String, "Organization name", func() {
		dsl.Example("Linux Foundation")
	})
	dsl.Attribute("domain", dsl.String, "Organization domain", func() {
		dsl.Example("linuxfoundation.org")
	})
	dsl.Attribute("logo", dsl.String, "Organization logo URL", func() {
		dsl.Example("https://example.com/logo.png")
	})
	dsl.Required("name", "domain")
})

// Define an example cached LFX resource for the nested "data" attribute for
// resource searches. This example happens to be a committee to match the
// example value of "committee" for the "type" attribute of Resource.
type CommitteeExampleStub struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}
