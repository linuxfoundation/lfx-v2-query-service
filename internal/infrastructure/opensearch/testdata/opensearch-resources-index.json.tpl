{{/*
Verbatim copy of the resources index mapping the indexer creates:
  lfx-v2-indexer-service/charts/lfx-v2-indexer-service/files/opensearch-resources-index.json.tpl
  at commit 2ede26d8bd416849ffeeea1e157ce6df91c601de (main, 2026-10-23).
The carrier-probe integration test renders it with the two Helm values
(number_of_shards, number_of_replicas) substituted for a throwaway index.
Re-copy it when the indexer changes the mapping; the test proves the probe
against the mapping production indexes, so the copy must stay verbatim.
*/}}
{
  "settings": {
    "index": {
      "number_of_shards": {{ .Values.opensearch.indexingJob.numberOfShards }},
      "number_of_replicas": {{ .Values.opensearch.indexingJob.numberOfReplicas }},
      "refresh_interval": "1s"
    }
  },
  "mappings": {
    "properties": {
      "object_ref": { "type": "keyword" },
      "object_type": { "type": "keyword" },
      "object_id": { "type": "keyword" },
      "parent_refs": { "type": "keyword" },
      "sort_name": { "type": "keyword" },
      "name_and_aliases": { "type": "search_as_you_type" },
      "tags": { "type": "keyword" },
      "public": { "type": "boolean" },
      "access_check_query": {
        "type": "text",
        "fields": {
          "keyword": { "type": "keyword", "ignore_above": 256 }
        }
      },
      "access_check_object": { "type": "keyword" },
      "access_check_relation": { "type": "keyword" },
      "history_check_query": {
        "type": "text",
        "fields": {
          "keyword": { "type": "keyword", "ignore_above": 256 }
        }
      },
      "history_check_object": { "type": "keyword" },
      "history_check_relation": { "type": "keyword" },
      "latest": { "type": "boolean" },
      "created_at": { "type": "date" },
      "created_by": { "type": "keyword" },
      "created_by_principals": { "type": "keyword" },
      "created_by_emails": { "type": "keyword" },
      "updated_at": { "type": "date" },
      "updated_by": { "type": "keyword" },
      "updated_by_principals": { "type": "keyword" },
      "updated_by_emails": { "type": "keyword" },
      "deleted_at": { "type": "date" },
      "deleted_by": { "type": "keyword" },
      "deleted_by_principals": { "type": "keyword" },
      "deleted_by_emails": { "type": "keyword" },
      "data": {
        "type": "flat_object"
      },
      "fulltext": {
        "type": "match_only_text"
      },
      "contacts": {
        "type": "nested",
        "properties": {
          "lfx_principal": { "type": "search_as_you_type" },
          "name": { "type": "search_as_you_type" },
          "emails": { "type": "search_as_you_type" },
          "bot": { "type": "boolean" },
          "profile": { "type": "flat_object" }
        }
      },
      "v1_data": {
        "type": "flat_object"
      }
    }
  }
}
