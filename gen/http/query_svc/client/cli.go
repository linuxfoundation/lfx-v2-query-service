// Code generated by goa v3.21.1, DO NOT EDIT.
//
// query-svc HTTP client CLI support package
//
// Command:
// $ goa gen github.com/linuxfoundation/lfx-v2-query-service/design

package client

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"

	querysvc "github.com/linuxfoundation/lfx-v2-query-service/gen/query_svc"
	goa "goa.design/goa/v3/pkg"
)

// BuildQueryResourcesPayload builds the payload for the query-svc
// query-resources endpoint from CLI flags.
func BuildQueryResourcesPayload(querySvcQueryResourcesVersion string, querySvcQueryResourcesName string, querySvcQueryResourcesParent string, querySvcQueryResourcesType string, querySvcQueryResourcesTags string, querySvcQueryResourcesSort string, querySvcQueryResourcesPageToken string, querySvcQueryResourcesBearerToken string) (*querysvc.QueryResourcesPayload, error) {
	var err error
	var version string
	{
		version = querySvcQueryResourcesVersion
		if !(version == "1") {
			err = goa.MergeErrors(err, goa.InvalidEnumValueError("version", version, []any{"1"}))
		}
		if err != nil {
			return nil, err
		}
	}
	var name *string
	{
		if querySvcQueryResourcesName != "" {
			name = &querySvcQueryResourcesName
			if utf8.RuneCountInString(*name) < 1 {
				err = goa.MergeErrors(err, goa.InvalidLengthError("name", *name, utf8.RuneCountInString(*name), 1, true))
			}
			if err != nil {
				return nil, err
			}
		}
	}
	var parent *string
	{
		if querySvcQueryResourcesParent != "" {
			parent = &querySvcQueryResourcesParent
			err = goa.MergeErrors(err, goa.ValidatePattern("parent", *parent, "^[a-zA-Z]+:[a-zA-Z0-9_-]+$"))
			if err != nil {
				return nil, err
			}
		}
	}
	var type_ *string
	{
		if querySvcQueryResourcesType != "" {
			type_ = &querySvcQueryResourcesType
		}
	}
	var tags []string
	{
		if querySvcQueryResourcesTags != "" {
			err = json.Unmarshal([]byte(querySvcQueryResourcesTags), &tags)
			if err != nil {
				return nil, fmt.Errorf("invalid JSON for tags, \nerror: %s, \nexample of valid JSON:\n%s", err, "'[\n      \"active\"\n   ]'")
			}
		}
	}
	var sort string
	{
		if querySvcQueryResourcesSort != "" {
			sort = querySvcQueryResourcesSort
			if !(sort == "name_asc" || sort == "name_desc" || sort == "updated_asc" || sort == "updated_desc") {
				err = goa.MergeErrors(err, goa.InvalidEnumValueError("sort", sort, []any{"name_asc", "name_desc", "updated_asc", "updated_desc"}))
			}
			if err != nil {
				return nil, err
			}
		}
	}
	var pageToken *string
	{
		if querySvcQueryResourcesPageToken != "" {
			pageToken = &querySvcQueryResourcesPageToken
		}
	}
	var bearerToken string
	{
		bearerToken = querySvcQueryResourcesBearerToken
	}
	v := &querysvc.QueryResourcesPayload{}
	v.Version = version
	v.Name = name
	v.Parent = parent
	v.Type = type_
	v.Tags = tags
	v.Sort = sort
	v.PageToken = pageToken
	v.BearerToken = bearerToken

	return v, nil
}
