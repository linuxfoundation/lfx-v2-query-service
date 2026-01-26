// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package opensearch

const queryResourceSource = `{
  {{- if ge .PageSize 0 }}
  "size": {{ .PageSize }},
  {{- end }}
  "query": {
    "bool": {
      "must": [
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
      ]
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
      }
    },
    {"_id": "asc"}
  ]
  {{- end }}
  {{- if .GroupBy }},
  "aggs": {
    "group_by": {
      "terms": {
        "field": {{ .GroupBy | quote }},
        "size": {{ .GroupBySize }}
      }
    }
  }
  {{- end }}
}`
