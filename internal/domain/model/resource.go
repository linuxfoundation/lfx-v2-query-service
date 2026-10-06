// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package model

// Resource represents a domain resource entity
type Resource struct {
	// Resource type
	Type string
	// Resource ID (within its resource collection)
	ID string
	// Resource data snapshot
	Data any
	// Metadata about the resource
	TransactionBodyStub
	// NeedCheck indicates if access control check is needed
	NeedCheck bool
	// SortValues holds the hit's sort values as a JSON array, the keyset
	// cursor that continues right after this hit; empty when the search
	// carried no sort.
	SortValues string
}

// TransactionBodyStub is used to decode the response's "source".
// **Ensure the fields here align to the relevant `SourceIncludes`
// parameters**.
type TransactionBodyStub struct {
	ObjectRef            string `json:"object_ref"`
	ObjectType           string `json:"object_type"`
	ObjectID             string `json:"object_id"`
	Public               bool   `json:"public"`
	AccessCheckObject    string `json:"access_check_object"`
	AccessCheckRelation  string `json:"access_check_relation"`
	HistoryCheckObject   string `json:"history_check_object"`
	HistoryCheckRelation string `json:"history_check_relation"`
	AccessCheckQuery     string `json:"access_check_query"`
	HistoryCheckQuery    string `json:"history_check_query"`
}
