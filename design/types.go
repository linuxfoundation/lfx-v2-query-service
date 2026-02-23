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
