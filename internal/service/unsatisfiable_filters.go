// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"
)

// checkSatisfiable distinguishes absent indexed dimensions from ordinary empty
// results. Each probe is type-wide, without caller values or access restrictions.
func (s *ResourceSearch) checkSatisfiable(ctx context.Context, criteria model.SearchCriteria) error {
	if s.config.DisableUnsatisfiableFilterRejection || criteria.ResourceType == nil || *criteria.ResourceType == "" {
		return nil
	}
	resourceType := *criteria.ResourceType
	memo := make(map[model.CarrierProbe]bool)
	carries := func(probe model.CarrierProbe) (bool, error) {
		if carried, ok := memo[probe]; ok {
			return carried, nil
		}
		carried, err := s.resourceSearcher.TypeCarries(ctx, resourceType, probe)
		if err != nil {
			return false, errors.NewServiceUnavailable("failed to check indexed filter support", err)
		}
		memo[probe] = carried
		return carried, nil
	}
	carried, err := carries(model.CarrierProbe{Kind: model.AnyDocument})
	if err != nil || !carried {
		return err
	}

	reject := func(dimension, name string) error {
		slog.InfoContext(ctx, "filter dimension not carried by indexed type", "object_type", resourceType, "dimension", dimension, "name", name)
		return errors.NewValidation(fmt.Sprintf("%s %q is not carried by any indexed %s document", dimension, name, resourceType))
	}
	check := func(probe model.CarrierProbe, dimension, name string) error {
		carried, err := carries(probe)
		if err != nil {
			return err
		}
		if !carried {
			return reject(dimension, name)
		}
		return nil
	}
	if criteria.DateField != nil {
		field := strings.TrimPrefix(*criteria.DateField, "data.")
		if err := check(model.CarrierProbe{Kind: model.DataField, Name: field}, "date_field", field); err != nil {
			return err
		}
	}
	if criteria.Parent != nil && *criteria.Parent != "" {
		kind, _, _ := strings.Cut(*criteria.Parent, ":")
		if err := check(model.CarrierProbe{Kind: model.ParentKind, Name: kind}, "parent kind", kind+":"); err != nil {
			return err
		}
	}
	for _, tag := range criteria.TagsAll {
		prefix, _, prefixed := strings.Cut(tag, ":")
		if prefixed {
			if err := check(model.CarrierProbe{Kind: model.TagPrefix, Name: prefix}, "tag prefix", prefix+":"); err != nil {
				return err
			}
		}
	}

	// A bare OR tag is an unprobed alternative, so absent prefixes cannot
	// establish that the entire OR clause is unsatisfiable.
	var tagPrefixes []string
	bareTag := false
	for _, tag := range criteria.Tags {
		prefix, _, prefixed := strings.Cut(tag, ":")
		if !prefixed {
			bareTag = true
			break
		}
		tagPrefixes = append(tagPrefixes, prefix)
	}
	checkAny := func(names []string, kind model.CarrierProbeKind, dimension, suffix string) error {
		for _, name := range names {
			carried, err := carries(model.CarrierProbe{Kind: kind, Name: name})
			if err != nil {
				return err
			}
			if carried {
				return nil
			}
		}
		if len(names) > 0 {
			return reject(dimension, names[0]+suffix)
		}
		return nil
	}
	if !bareTag {
		if err := checkAny(tagPrefixes, model.TagPrefix, "tag prefix", ":"); err != nil {
			return err
		}
	}
	for _, filters := range [][]model.FieldFilter{criteria.Filters, criteria.FiltersAll} {
		for _, filter := range filters {
			field := strings.TrimPrefix(filter.Field, "data.")
			if err := check(model.CarrierProbe{Kind: model.DataField, Name: field}, "filter field", field); err != nil {
				return err
			}
		}
	}
	fields := make([]string, 0, len(criteria.FiltersOr))
	for _, filter := range criteria.FiltersOr {
		fields = append(fields, strings.TrimPrefix(filter.Field, "data."))
	}
	return checkAny(fields, model.DataField, "filter field", "")
}
