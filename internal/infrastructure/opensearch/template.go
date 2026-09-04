// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package opensearch

// criteriaSource holds the bool-query clauses shared by every request the
// searcher renders. It is a named sub-template: queryResourceSource embeds
// it for /_search and /_count, and countAggregationSource embeds it for the
// aggregation-only bodies of the count route, so the criteria are rendered
// byte-identically on every path.
//
// The "criteriaMust" block renders the members of "must" (starting with the
// fixed latest:true clause); "criteriaShould" renders the optional
// should/minimum_should_match pair used by the OR-tags parameter.
const criteriaSource = `
{{- define "criteriaMust" }}
        {
          "term": {"latest": true}
        }
        {{- if .PublicOnly }},
        {
          "term": {"public": true}
        }
        {{- end }}
        {{- if .PrivateOnly }},
        {
          "bool": {
            "must_not": {
              "term": {"public": true}
            }
          }
        }
        {{- end }}
        {{- if .ResourceType }},
        {
          "term": {
            "object_type": {{ .ResourceType | quote }}
          }
        }
        {{- end }}
        {{- if .Parent }},
        {
          "term": {
            "parent_refs": {{ .Parent | quote }}
          }
        }
        {{- end }}
        {{- if .Name }},
        {
          "multi_match": {
            "query": {{ .Name | quote }},
            "type": "bool_prefix",
            "fields": [
              "name_and_aliases",
              "name_and_aliases._2gram",
              "name_and_aliases._3gram"
            ]
          }
        }
        {{- end }}
        {{- if .TagsAll }}
        {{- range .TagsAll }},
        {
          "term": {
            "tags": {{ . | quote }}
          }
        }
        {{- end }}
        {{- end }}
        {{- if and .DateField (or .DateFrom .DateTo) }},
        {
          "range": {
            {{ .DateField | quote }}: {
              {{- if .DateFrom }}
              "gte": {{ .DateFrom | quote }}
              {{- end }}
              {{- if and .DateFrom .DateTo }},{{- end }}
              {{- if .DateTo }}
              "lte": {{ .DateTo | quote }}
              {{- end }}
            }
          }
        }
        {{- end }}
        {{- if .Filters }}
        {{- range .Filters }},
        {
          "term": {
            {{ .Field | quote }}: {{ .Value | quote }}
          }
        }
        {{- end }}
        {{- end }}
        {{- if .FiltersAll }}
        {{- range .FiltersAll }},
        {
          "term": {
            {{ .Field | quote }}: {{ .Value | quote }}
          }
        }
        {{- end }}
        {{- end }}
        {{- if .ObjectRefs }},
        {
          "terms": {
            "object_ref": [
              {{- $first := true -}}
              {{- range .ObjectRefs -}}
              {{- if $first -}}{{- $first = false -}}{{- else }},{{- end }}
              {{ . | quote }}
              {{- end }}
            ]
          }
        }
        {{- end }}
        {{- if .FiltersOr }},
        {
          "bool": {
            "should": [
              {{- $first := true -}}
              {{- range .FiltersOr -}}
              {{- if $first -}}
              {{- $first = false -}}
              {{- else }},
              {{- end }}
              {
                "term": {
                  {{ .Field | quote }}: {{ .Value | quote }}
                }
              }
              {{- end }}
            ],
            "minimum_should_match": 1
          }
        }
        {{- end }}
{{- end }}
{{- define "criteriaShould" }}
      {{- if .Tags }},
      "minimum_should_match": 1,
      "should": [
        {{- $first := true -}}
        {{- range .Tags -}}
        {{- if $first -}}
        {{- $first = false -}}
        {{- else }},
        {{- end }}
        {
          "term": {
            "tags": {{ . | quote }}
          }
        }
        {{- end }}
      ]
      {{- end }}
{{- end }}`

// queryResourceSource renders the /_search and /_count bodies from a
// model.SearchCriteria.
const queryResourceSource = `{
  {{- if ge .PageSize 0 }}
  "size": {{ .PageSize }},
  {{- end }}
  "query": {
    "bool": {
      "must": [
        {{- template "criteriaMust" . }}
      ]
      {{- template "criteriaShould" . }}
    }
  }
  {{- if .SearchAfter }},
  "search_after": {{ .SearchAfter }}
  {{- end }}
  {{- if gt .PageSize 0 }},
  "sort": [
    {
      {{ .SortBy | quote }}: {
        "order": {{ .SortOrder | quote }}
        {{- if ne .SortBy "_score" }},
        "missing": "_last"
        {{- end }}
      }
    },
    {"_id": "asc"}
  ]
  {{- end }}
}`

// countAggregationSource renders the size-0 aggregation bodies of the count
// route from a countAggregationParams. Three shapes are possible:
//
//   - the access-key walk: composite over the access-check field over
//     private resources (Criteria.PrivateOnly), paged with "after";
//   - the grouped count: terms over tags restricted to one prefix;
//   - the cardinality walk: composite over tags starting just after the bare
//     prefix, paged with "after".
//
// The last two run over the authorized set: public resources and/or private
// resources whose access-check key is in AuthorizedKeys, expressed as a
// "filter" bool with should/minimum_should_match so it does not interact
// with the OR-tags should clause on the outer bool.
const countAggregationSource = `{
  "size": 0,
  "query": {
    "bool": {
      "must": [
        {{- template "criteriaMust" .Criteria }}
      ]
      {{- template "criteriaShould" .Criteria }}
      {{- if .AuthorizedFilter }},
      "filter": {
        "bool": {
          "should": [
            {{- $first := true -}}
            {{- if .IncludePublic -}}
            {{- $first = false }}
            {
              "term": {"public": true}
            }
            {{- end }}
            {{- if .AuthorizedKeys }}
            {{- if not $first }},{{- end }}
            {
              "terms": {
                {{ .AccessKeyField | quote }}: [
                  {{- $firstKey := true -}}
                  {{- range .AuthorizedKeys -}}
                  {{- if $firstKey -}}{{- $firstKey = false -}}{{- else }},{{- end }}
                  {{ . | quote }}
                  {{- end }}
                ]
              }
            }
            {{- end }}
          ],
          "minimum_should_match": 1
        }
      }
      {{- end }}
    }
  },
  "aggs": {
    {{- if .AccessWalk }}
    "access_keys": {
      "composite": {
        "size": {{ .PageSize }},
        "sources": [
          {
            "access_key": {
              "terms": {
                "field": {{ .AccessKeyField | quote }}
              }
            }
          }
        ]
        {{- if .After }},
        "after": {
          "access_key": {{ .After | quote }}
        }
        {{- end }}
      }
    }
    {{- end }}
    {{- if .GroupByPrefix }}
    "group_by": {
      "terms": {
        "field": "tags",
        "size": {{ .GroupBySize }},
        "include": {{ .GroupByInclude | quote }}
      }
    }
    {{- end }}
    {{- if .CardinalityPrefix }}
    "tags": {
      "composite": {
        "size": {{ .PageSize }},
        "sources": [
          {
            "tag": {
              "terms": {
                "field": "tags"
              }
            }
          }
        ],
        "after": {
          "tag": {{ .After | quote }}
        }
      }
    }
    {{- end }}
  }
}`
