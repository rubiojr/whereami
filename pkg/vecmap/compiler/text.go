package compiler

import (
	"fmt"
	"iter"
	"slices"

	"github.com/rubiojr/whereami/pkg/vecmap/geometry"
	"github.com/rubiojr/whereami/pkg/vecmap/glyph"
)

// TextRequest is the metric-layout portion of an evaluated symbol. Text and font
// identity borrow immutable strings; no feature properties or native state remain.
type TextRequest struct {
	Text, FontStack string
	Options         glyph.LayoutOptions
}

// Layout reuses metric layout without choosing script/readiness policy. Live
// callers can resolve fonts and choose fallback before calling it. Failed layout
// returns nil; successful positioned glyphs are owned with borrowed bitmap bytes.
func (r TextRequest) Layout(available map[uint32]glyph.Glyph) *glyph.PreparedLayout {
	layout, ok := glyph.LayoutText(r.Text, r.FontStack, available, r.Options)
	if !ok {
		return nil
	}
	return &glyph.PreparedLayout{TextLayout: layout}
}

// FontStacks returns sorted, unique exact font identities for nonempty requests,
// including unsupported text and empty font names. It does not load or validate
// fonts. Iteration and string sizes are caller-bounded; nil input is empty.
func FontStacks[K comparable](requests iter.Seq2[K, TextRequest]) []string {
	stacks := make(map[string]struct{})
	if requests != nil {
		for _, request := range requests {
			if request.Text != "" {
				stacks[request.FontStack] = struct{}{}
			}
		}
	}
	return sortedKeys(stacks)
}

// DecodeFontRanges decodes one common range start per exact font-stack identity.
// It owns all output maps/bitmaps and fails atomically with font context. Per-range
// byte/metric limits come from glyph.DecodeRange; total font count/bytes and map
// traversal order are caller-owned. Empty input yields an owned empty map.
func DecodeFontRanges(ranges map[string][]byte, start uint32) (map[string]map[uint32]glyph.Glyph, error) {
	fonts := make(map[string]map[uint32]glyph.Glyph)
	for font, data := range ranges {
		decoded, err := glyph.DecodeRange(data, font, start)
		if err != nil {
			return nil, fmt.Errorf("font %q: %w", font, err)
		}
		fonts[font] = decoded.Glyphs
	}
	return fonts, nil
}

// PrepareTextLayouts applies the fixture's SDF-only availability policy to keyed
// text requests. Missing glyphs, unsupported scripts and invalid text record the
// font in the sorted missing list. Invalid metric layout after complete coverage
// simply omits that label. Empty text is skipped. Text is checked before layout
// whitespace processing; only CR/LF need no glyph. Font maps are immutable borrows.
//
// The result owns its map/layouts, preserving keys without a conversion slice.
// Duplicate keys retain the last successful layout; a later skipped request does
// not erase it. At most glyph.MaxPreparedLayouts requests may be yielded (including
// empty/duplicate requests). Overflow stops iteration and returns nil outputs and
// geometry.ErrGeometryLimit. Iterator code/work between yields is caller-owned.
// Nil input produces empty owned results. Inputs and iterator are not retained.
func PrepareTextLayouts[K comparable](requests iter.Seq2[K, TextRequest], fonts map[string]map[uint32]glyph.Glyph) (map[K]*glyph.PreparedLayout, []string, error) {
	layouts := make(map[K]*glyph.PreparedLayout)
	missing := make(map[string]struct{})
	if requests != nil {
		count := 0
		for key, request := range requests {
			count++
			if count > glyph.MaxPreparedLayouts {
				return nil, nil, geometry.ErrGeometryLimit
			}
			if request.Text == "" {
				continue
			}
			available := fonts[request.FontStack]
			if !TextComplete(request.Text, available) {
				missing[request.FontStack] = struct{}{}
				continue
			}
			if layout := request.Layout(available); layout != nil {
				layouts[key] = layout
			}
		}
	}
	return layouts, sortedKeys(missing), nil
}

// TextComplete checks the existing SDF eligibility subset and original-text glyph
// key coverage, not glyph metric validity, shaping or atlas rectangle coverage.
func TextComplete(text string, available map[uint32]glyph.Glyph) bool {
	if available == nil || !glyph.TextEligible(text) {
		return false
	}
	for _, code := range text {
		if code == '\n' || code == '\r' {
			continue
		}
		if _, exists := available[uint32(code)]; !exists {
			return false
		}
	}
	return true
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
