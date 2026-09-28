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

	// Only dimensions the query actually applies are checked. A date field
	// without a bound renders no range clause, so it is not a filter here.
	var dateField string
	if criteria.DateField != nil && (criteria.DateFrom != nil || criteria.DateTo != nil) {
		dateField = strings.TrimPrefix(*criteria.DateField, "data.")
	}
	var parentKind string
	if criteria.Parent != nil && *criteria.Parent != "" {
		parentKind, _, _ = strings.Cut(*criteria.Parent, ":")
	}
	var allPrefixes []string
	for _, tag := range criteria.TagsAll {
		if prefix, _, prefixed := strings.Cut(tag, ":"); prefixed {
			allPrefixes = append(allPrefixes, prefix)
		}
	}
	// A bare OR tag is an unprobed alternative, so absent prefixes cannot
	// establish that the entire OR clause is unsatisfiable.
	var anyPrefixes []string
	for _, tag := range criteria.Tags {
		prefix, _, prefixed := strings.Cut(tag, ":")
		if !prefixed {
			anyPrefixes = nil
			break
		}
		anyPrefixes = append(anyPrefixes, prefix)
	}
	var allFields []string
	for _, filters := range [][]model.FieldFilter{criteria.Filters, criteria.FiltersAll} {
		for _, filter := range filters {
			allFields = append(allFields, strings.TrimPrefix(filter.Field, "data."))
		}
	}
	anyFields := make([]string, 0, len(criteria.FiltersOr))
	for _, filter := range criteria.FiltersOr {
		anyFields = append(anyFields, strings.TrimPrefix(filter.Field, "data."))
	}
	if dateField == "" && parentKind == "" && len(allPrefixes)+len(anyPrefixes)+len(allFields)+len(anyFields) == 0 {
		return nil
	}

	memo := make(map[model.CarrierProbe]bool)
	carries := func(probe model.CarrierProbe) (bool, error) {
		if carried, ok := memo[probe]; ok {
			return carried, nil
		}
		carried, err := s.resourceSearcher.TypeCarries(ctx, resourceType, probe)
		if err != nil {
			if ctx.Err() != nil {
				return false, fmt.Errorf("indexed filter support check cancelled: %w", err)
			}
			slog.ErrorContext(ctx, "indexed filter support check failed", "error", err, "object_type", resourceType)
			return false, errors.NewServiceUnavailable("failed to check indexed filter support")
		}
		memo[probe] = carried
		return carried, nil
	}
	carried, err := carries(model.CarrierProbe{Kind: model.AnyDocument})
	if err != nil || !carried {
		return err
	}

	reject := func(dimension, name string) error {
		slog.DebugContext(ctx, "filter dimension not carried by indexed type", "object_type", resourceType, "dimension", dimension, "name", name)
		return errors.NewValidation(fmt.Sprintf("%s %q is not carried by any indexed %s document", dimension, name, resourceType))
	}
	checkAll := func(names []string, kind model.CarrierProbeKind, dimension, suffix string) error {
		for _, name := range names {
			carried, err := carries(model.CarrierProbe{Kind: kind, Name: name})
			if err != nil {
				return err
			}
			if !carried {
				return reject(dimension, name+suffix)
			}
		}
		return nil
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

	if dateField != "" {
		if err := checkAll([]string{dateField}, model.DataField, "date_field", ""); err != nil {
			return err
		}
	}
	if parentKind != "" {
		if err := checkAll([]string{parentKind}, model.ParentKind, "parent kind", ":"); err != nil {
			return err
		}
	}
	if err := checkAll(allPrefixes, model.TagPrefix, "tag prefix", ":"); err != nil {
		return err
	}
	if err := checkAny(anyPrefixes, model.TagPrefix, "tag prefix", ":"); err != nil {
		return err
	}
	if err := checkAll(allFields, model.DataField, "filter field", ""); err != nil {
		return err
	}
	return checkAny(anyFields, model.DataField, "filter field", "")
}
