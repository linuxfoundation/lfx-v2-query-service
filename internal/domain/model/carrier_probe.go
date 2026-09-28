// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package model

// CarrierProbeKind identifies a type-level indexed capability, not a value match.
type CarrierProbeKind string

const (
	// AnyDocument asks whether the type has any indexed document.
	AnyDocument CarrierProbeKind = "any_document"
	// DataField asks whether any document carries a field within data.
	DataField CarrierProbeKind = "data_field"
	// ParentKind asks whether any document carries a parent of this kind.
	ParentKind CarrierProbeKind = "parent_kind"
	// TagPrefix asks whether any document carries a tag with this prefix.
	TagPrefix CarrierProbeKind = "tag_prefix"
)

// CarrierProbe describes a capability to check without access or value filters.
// Name is a bare data field, parent kind, or tag prefix; AnyDocument ignores it.
type CarrierProbe struct {
	Kind CarrierProbeKind
	Name string
}
