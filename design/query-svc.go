// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package design

import (
	"goa.design/goa/v3/dsl"
)

var _ = dsl.API("lfx-v2-query-service", func() {
	dsl.Title("LFX V2 - Query Service")
	dsl.Description("Query indexed resources")
})

var JWTAuth = dsl.JWTSecurity("jwt", func() {
	dsl.Description("Heimdall authorization")
})

var _ = dsl.Service("query-svc", func() {
	dsl.Description("The query service provides resource and user queries.")

	dsl.Error("BadRequest", BadRequestError, "Bad request")
	dsl.Error("NotFound", NotFoundError, "Not found")
	dsl.Error("InternalServerError", InternalServerError, "Internal server error")
	dsl.Error("ServiceUnavailable", ServiceUnavailableError, "Service unavailable")

	dsl.Method("query-resources", func() {
		dsl.Description("Locate resources by their type or parent, or use typeahead search to query resources by a display name or similar alias.")

		dsl.Security(JWTAuth)

		dsl.Payload(func() {
			dsl.Extend(Sortable)
			dsl.Token("bearer_token", dsl.String, func() {
				dsl.Description("Token")
				dsl.Example("eyJhbGci...")
			})
			dsl.Attribute("version", dsl.String, "Version of the API", func() {
				dsl.Enum("1")
				dsl.Example("1")
			})
			dsl.Attribute("name", dsl.String, "Resource name or alias; supports typeahead", func() {
				dsl.Example("gov board")
				dsl.MinLength(1)
			})
			dsl.Attribute("parent", dsl.String, "Parent (for navigation; varies by object type)", func() {
				dsl.Example("project:123")
				dsl.Pattern(`^[a-zA-Z]+:[a-zA-Z0-9_-]+$`)
			})
			dsl.Attribute("type", dsl.String, "Resource type to search", func() {
				dsl.Example("committee")
			})
			dsl.Attribute("tags", dsl.ArrayOf(dsl.String), "Tags to search with OR logic - matches resources with any of these tags", func() {
				dsl.Example([]string{"active", "public"})
			})
			dsl.Attribute("tags_all", dsl.ArrayOf(dsl.String), "Tags to search with AND logic - matches resources that have all of these tags", func() {
				dsl.Example([]string{"governance", "security"})
			})
			dsl.Attribute("filters", dsl.ArrayOf(dsl.String), "Direct field filters with term clauses on data fields - format: 'field:value' (e.g., 'status:active'). Fields are automatically prefixed with 'data.'", func() {
				dsl.Example([]string{"status:active", "priority:high"})
			})
			dsl.Required("bearer_token", "version")
		})

		dsl.Result(func() {
			dsl.Attribute("resources", dsl.ArrayOf(Resource), "Resources found", func() {})
			dsl.Attribute("page_token", dsl.String, "Opaque token if more results are available", func() {
				dsl.Example("****")
			})
			dsl.Attribute("cache_control", dsl.String, "Cache control header", func() {
				dsl.Example("public, max-age=300")
			})
			dsl.Required("resources")
		})

		dsl.HTTP(func() {
			dsl.GET("/query/resources")
			dsl.Param("version:v")
			dsl.Param("name")
			dsl.Param("parent")
			dsl.Param("type")
			dsl.Param("tags")
			dsl.Param("tags_all")
			dsl.Param("filters")
			dsl.Param("sort")
			dsl.Param("page_token")
			dsl.Header("bearer_token:Authorization")
			dsl.Response(dsl.StatusOK, func() {
				dsl.Header("cache_control:Cache-Control")
			})
			dsl.Response("BadRequest", dsl.StatusBadRequest)
			dsl.Response("InternalServerError", dsl.StatusInternalServerError)
			dsl.Response("ServiceUnavailable", dsl.StatusServiceUnavailable)
		})
	})

	dsl.Method("query-resources-count", func() {
		dsl.Description("Count matching resources by query.")

		dsl.Security(JWTAuth)

		dsl.Payload(func() {
			dsl.Token("bearer_token", dsl.String, func() {
				dsl.Description("JWT token issued by Heimdall")
				dsl.Example("eyJhbGci...")
			})
			dsl.Attribute("version", dsl.String, "Version of the API", func() {
				dsl.Enum("1")
				dsl.Example("1")
			})
			dsl.Attribute("name", dsl.String, "Resource name or alias; supports typeahead", func() {
				dsl.Example("gov board")
				dsl.MinLength(1)
			})
			dsl.Attribute("parent", dsl.String, "Parent (for navigation; varies by object type)", func() {
				dsl.Example("project:123")
			})
			dsl.Attribute("type", dsl.String, "Resource type to search", func() {
				dsl.Example("committee")
			})
			dsl.Attribute("tags", dsl.ArrayOf(dsl.String), "Tags to search with OR logic - matches resources with any of these tags", func() {
				dsl.Example([]string{"active", "public"})
			})
			dsl.Attribute("tags_all", dsl.ArrayOf(dsl.String), "Tags to search with AND logic - matches resources that have all of these tags", func() {
				dsl.Example([]string{"governance", "security"})
			})
			dsl.Attribute("filters", dsl.ArrayOf(dsl.String), "Direct field filters with term clauses on data fields - format: 'field:value' (e.g., 'status:active'). Fields are automatically prefixed with 'data.'", func() {
				dsl.Example([]string{"status:active", "priority:high"})
			})
			dsl.Required("bearer_token", "version")
		})

		dsl.Result(func() {
			dsl.Attribute("count", dsl.UInt64, "Count of resources found", func() {
				dsl.Example(1234)
			})
			dsl.Attribute("has_more", dsl.Boolean, "True if count is not guaranteed to be exhaustive: client should request a narrower query", func() {
				dsl.Example(false)
			})
			dsl.Attribute("cache_control", dsl.String, "Cache control header", func() {
				dsl.Example("public, max-age=300")
			})
			dsl.Required("count", "has_more")
		})

		dsl.HTTP(func() {
			dsl.GET("/query/resources/count")
			dsl.Param("version:v")
			dsl.Param("name")
			dsl.Param("parent")
			dsl.Param("type")
			dsl.Param("tags")
			dsl.Param("tags_all")
			dsl.Param("filters")
			dsl.Header("bearer_token:Authorization")
			dsl.Response(dsl.StatusOK, func() {
				dsl.Header("cache_control:Cache-Control")
			})
			dsl.Response("BadRequest", dsl.StatusBadRequest)
			dsl.Response("InternalServerError", dsl.StatusInternalServerError)
			dsl.Response("ServiceUnavailable", dsl.StatusServiceUnavailable)
		})
	})

	dsl.Method("query-orgs", func() {
		dsl.Description("Locate a single organization by name or domain.")

		dsl.Security(JWTAuth)

		dsl.Payload(func() {
			dsl.Token("bearer_token", dsl.String, func() {
				dsl.Description("Token")
				dsl.Example("eyJhbGci...")
			})
			dsl.Attribute("version", dsl.String, "Version of the API", func() {
				dsl.Enum("1")
				dsl.Example("1")
			})
			dsl.Attribute("name", dsl.String, "Organization name", func() {
				dsl.Example("The Linux Foundation")
				dsl.MinLength(1)
			})
			dsl.Attribute("domain", dsl.String, "Organization domain or website URL", func() {
				dsl.Example("linuxfoundation.org")
				dsl.Pattern(`^[a-zA-Z0-9][a-zA-Z0-9-_.]*[a-zA-Z0-9]*\.[a-zA-Z]{2,}$`)
			})
			dsl.Required("bearer_token", "version")
		})

		dsl.Result(Organization)

		dsl.HTTP(func() {
			dsl.GET("/query/orgs")
			dsl.Param("version:v")
			dsl.Param("name")
			dsl.Param("domain")
			dsl.Header("bearer_token:Authorization")
			dsl.Response(dsl.StatusOK)
			dsl.Response("BadRequest", dsl.StatusBadRequest)
			dsl.Response("NotFound", dsl.StatusNotFound)
			dsl.Response("InternalServerError", dsl.StatusInternalServerError)
			dsl.Response("ServiceUnavailable", dsl.StatusServiceUnavailable)
		})
	})

	dsl.Method("suggest-orgs", func() {
		dsl.Description("Get organization suggestions for typeahead search based on a query.")

		dsl.Security(JWTAuth)

		dsl.Payload(func() {
			dsl.Token("bearer_token", dsl.String, func() {
				dsl.Description("Token")
				dsl.Example("eyJhbGci...")
			})
			dsl.Attribute("version", dsl.String, "Version of the API", func() {
				dsl.Enum("1")
				dsl.Example("1")
			})
			dsl.Attribute("query", dsl.String, "Search query for organization suggestions", func() {
				dsl.Example("linux")
				dsl.MinLength(1)
			})
			dsl.Required("bearer_token", "version", "query")
		})

		dsl.Result(func() {
			dsl.Attribute("suggestions", dsl.ArrayOf(OrganizationSuggestion), "Organization suggestions", func() {})
			dsl.Required("suggestions")
		})

		dsl.HTTP(func() {
			dsl.GET("/query/orgs/suggest")
			dsl.Param("version:v")
			dsl.Param("query")
			dsl.Header("bearer_token:Authorization")
			dsl.Response(dsl.StatusOK)
			dsl.Response("BadRequest", dsl.StatusBadRequest)
			dsl.Response("InternalServerError", dsl.StatusInternalServerError)
			dsl.Response("ServiceUnavailable", dsl.StatusServiceUnavailable)
		})
	})

	dsl.Method("readyz", func() {
		dsl.Description("Check if the service is able to take inbound requests.")
		dsl.Meta("swagger:generate", "false")
		dsl.Result(dsl.Bytes, func() {
			dsl.Example("OK")
		})
		dsl.Error("NotReady", func() {
			dsl.Description("Service is not ready yet")
			dsl.Temporary()
			dsl.Fault()
		})
		dsl.HTTP(func() {
			dsl.GET("/readyz")
			dsl.Response(dsl.StatusOK, func() {
				dsl.ContentType("text/plain")
			})
			dsl.Response("NotReady", dsl.StatusServiceUnavailable)
		})
	})

	dsl.Method("livez", func() {
		dsl.Description("Check if the service is alive.")
		dsl.Meta("swagger:generate", "false")
		dsl.Result(dsl.Bytes, func() {
			dsl.Example("OK")
		})
		dsl.HTTP(func() {
			dsl.GET("/livez")
			dsl.Response(dsl.StatusOK, func() {
				dsl.ContentType("text/plain")
			})
		})
	})

	// Serve the file gen/http/openapi3.json for requests sent to /openapi.json.
	dsl.Files("/_query/openapi.json", "gen/http/openapi.json", func() {
		dsl.Meta("swagger:generate", "false")
	})
	dsl.Files("/_query/openapi.yaml", "gen/http/openapi.yaml", func() {
		dsl.Meta("swagger:generate", "false")
	})
	dsl.Files("/_query/openapi3.json", "gen/http/openapi3.json", func() {
		dsl.Meta("swagger:generate", "false")
	})
	dsl.Files("/_query/openapi3.yaml", "gen/http/openapi3.yaml", func() {
		dsl.Meta("swagger:generate", "false")
	})
})
