// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package service

import (
	"context"
	stderrors "errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/linuxfoundation/lfx-v2-query-service/internal/domain/model"
	"github.com/linuxfoundation/lfx-v2-query-service/pkg/errors"
)

// errProbeFailed marks a probe the searcher could not answer. The check is
// advisory: a failure is logged and the ordinary result stands, so a
// request that succeeds today cannot start failing because the check could
// not run. Only a successful probe that finds a dimension absent rejects.
var errProbeFailed = stderrors.New("indexed filter support probe failed")

// maxCarrierProbesPerRequest caps the distinct probes one request may send.
// A request names one type probe plus one probe per bounded date field,
// parent kind, distinct tag prefix, distinct filter field, group_by prefix
// and metric prefix; realistic requests name a handful, so sixteen covers
// them with room to spare while bounding what a request with many distinct
// prefixes or fields can cost the index. Once the cap is reached the check
// stops and the ordinary result stands, as after a failed probe.
const maxCarrierProbesPerRequest = 16

// probeOutcome is the remembered answer of one probe.
type probeOutcome int

const (
	probeAbsent probeOutcome = iota
	probeCarried
	probeFailed
)

// carrierCheck holds one request's probe state so every rule of a request
// shares one memo: a prefix named by both a filter and an aggregation is
// probed once, and a probe that failed is not sent again. Each probe is
// type-wide, without caller values or access restrictions.
type carrierCheck struct {
	searcher interface {
		TypeCarries(ctx context.Context, resourceType string, probe model.CarrierProbe) (bool, error)
	}
	resourceType string
	memo         map[model.CarrierProbe]probeOutcome
	// sent counts the probes sent to the searcher, for the cap.
	sent int
	// aborted is set once a probe failed or the cap was reached: from then
	// on no probe is sent and no rule of the request can reject, so the
	// ordinary result stands whole.
	aborted bool
}

// newCarrierCheck returns the request's check, or nil when the toggle is off
// or the request names no type, in which case nothing is ever probed.
func (s *ResourceSearch) newCarrierCheck(criteria model.SearchCriteria) *carrierCheck {
	if s.config.DisableUnsatisfiableFilterRejection || criteria.ResourceType == nil || *criteria.ResourceType == "" {
		return nil
	}
	return &carrierCheck{
		searcher:     s.resourceSearcher,
		resourceType: *criteria.ResourceType,
		memo:         make(map[model.CarrierProbe]probeOutcome),
	}
}

// advisory turns a failed probe into the ordinary result.
func advisory(err error) error {
	if stderrors.Is(err, errProbeFailed) {
		return nil
	}
	return err
}

// checkSatisfiable distinguishes filters naming absent indexed dimensions
// from ordinary empty results.
func (s *ResourceSearch) checkSatisfiable(ctx context.Context, criteria model.SearchCriteria) error {
	check := s.newCarrierCheck(criteria)
	if check == nil {
		return nil
	}
	return advisory(check.criteria(ctx, criteria))
}

// carries answers one probe, memoised for the request. A probe the searcher
// could not answer is logged once, remembered, and aborts the check: every
// later ask of the request reports errProbeFailed without sending anything.
// A probe that would exceed the per-request cap aborts the same way. A caller
// cancellation passes through.
func (c *carrierCheck) carries(ctx context.Context, probe model.CarrierProbe) (bool, error) {
	if c.aborted {
		return false, errProbeFailed
	}
	if outcome, ok := c.memo[probe]; ok {
		if outcome == probeFailed {
			return false, errProbeFailed
		}
		return outcome == probeCarried, nil
	}
	if c.sent >= maxCarrierProbesPerRequest {
		slog.InfoContext(ctx, "indexed filter support probe cap reached; returning the ordinary result",
			"object_type", c.resourceType,
			"probes_sent", c.sent,
			"probe_kind", string(probe.Kind),
		)
		c.aborted = true
		return false, errProbeFailed
	}
	c.sent++
	carried, err := c.searcher.TypeCarries(ctx, c.resourceType, probe)
	if err != nil {
		if cause := ctx.Err(); cause != nil {
			// The caller's cancellation is the error that passes through;
			// the transport error the searcher saw travels with it.
			return false, fmt.Errorf("indexed filter support check cancelled: %w (%w)", cause, err)
		}
		slog.ErrorContext(ctx, "indexed filter support probe failed; returning the ordinary result",
			"error", err,
			"object_type", c.resourceType,
			"probe_kind", string(probe.Kind),
			"probe_name", probe.Name,
		)
		c.memo[probe] = probeFailed
		c.aborted = true
		return false, errProbeFailed
	}
	if carried {
		c.memo[probe] = probeCarried
	} else {
		c.memo[probe] = probeAbsent
	}
	return carried, nil
}

// hasDocuments reports whether the type has any indexed document. An empty
// type is not a caller error, so nothing is rejected for it.
func (c *carrierCheck) hasDocuments(ctx context.Context) (bool, error) {
	return c.carries(ctx, model.CarrierProbe{Kind: model.AnyDocument})
}

// reject builds the 400 for an absent dimension. Only the dimension's name
// is echoed, never a caller-supplied value.
func (c *carrierCheck) reject(ctx context.Context, dimension, name string) error {
	slog.DebugContext(ctx, "filter dimension not carried by indexed type", "object_type", c.resourceType, "dimension", dimension, "name", name)
	return errors.NewValidation(fmt.Sprintf("%s %q is not carried by any indexed %s document", dimension, name, c.resourceType))
}

// all rejects at the first name the type does not carry.
func (c *carrierCheck) all(ctx context.Context, names []string, kind model.CarrierProbeKind, dimension, suffix string) error {
	for _, name := range names {
		carried, err := c.carries(ctx, model.CarrierProbe{Kind: kind, Name: name})
		if err != nil {
			return err
		}
		if !carried {
			return c.reject(ctx, dimension, name+suffix)
		}
	}
	return nil
}

// any rejects only when none of the names is carried.
func (c *carrierCheck) any(ctx context.Context, names []string, kind model.CarrierProbeKind, dimension, suffix string) error {
	for _, name := range names {
		carried, err := c.carries(ctx, model.CarrierProbe{Kind: kind, Name: name})
		if err != nil {
			return err
		}
		if carried {
			return nil
		}
	}
	if len(names) > 0 {
		return c.reject(ctx, dimension, names[0]+suffix)
	}
	return nil
}

// criteria checks the filters of an empty result, in order, stopping at the
// first absent dimension.
func (c *carrierCheck) criteria(ctx context.Context, criteria model.SearchCriteria) error {
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

	carried, err := c.hasDocuments(ctx)
	if err != nil || !carried {
		return err
	}
	if dateField != "" {
		if err := c.all(ctx, []string{dateField}, model.DataField, "date_field", ""); err != nil {
			return err
		}
	}
	if parentKind != "" {
		if err := c.all(ctx, []string{parentKind}, model.ParentKind, "parent kind", ":"); err != nil {
			return err
		}
	}
	if err := c.all(ctx, allPrefixes, model.TagPrefix, "tag prefix", ":"); err != nil {
		return err
	}
	if err := c.any(ctx, anyPrefixes, model.TagPrefix, "tag prefix", ":"); err != nil {
		return err
	}
	if err := c.all(ctx, allFields, model.DataField, "filter field", ""); err != nil {
		return err
	}
	return c.any(ctx, anyFields, model.DataField, "filter field", "")
}

// aggregation checks the prefixes a count aggregated on when the aggregation
// came back empty: the group_by prefix when no group was returned and the
// metric prefix when the distinct count is zero. A non-zero count already
// proves the type has documents.
func (c *carrierCheck) aggregation(ctx context.Context, aggregation model.CountAggregation, result *model.CountResult) error {
	groupsEmpty := aggregation.GroupByPrefix != "" && len(result.Groups) == 0
	metricZero := aggregation.CardinalityPrefix != "" && (result.MetricValue == nil || *result.MetricValue == 0)
	if !groupsEmpty && !metricZero {
		return nil
	}
	if result.Count > 0 {
		c.memo[model.CarrierProbe{Kind: model.AnyDocument}] = probeCarried
	}
	carried, err := c.hasDocuments(ctx)
	if err != nil || !carried {
		return err
	}
	if groupsEmpty {
		if err := c.all(ctx, []string{aggregation.GroupByPrefix}, model.TagPrefix, "group_by prefix", ":"); err != nil {
			return err
		}
	}
	if metricZero {
		return c.all(ctx, []string{aggregation.CardinalityPrefix}, model.TagPrefix, "metric prefix", ":")
	}
	return nil
}
