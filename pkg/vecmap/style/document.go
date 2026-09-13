package style

import (
	"encoding/json"
	"fmt"
	"math"
)

// Document is the supported subset of a version-8 style document. Unknown JSON
// fields are ignored; source loading and asset/cache policy belong to the caller.
type Document struct {
	Version int     `json:"version"`
	Layers  []Layer `json:"layers"`
}

// Layer retains raw paint/layout JSON until compilation. Filter is already a
// JSON-style expression and is borrowed by Compile.
type Layer struct {
	ID          string                     `json:"id"`
	Type        string                     `json:"type"`
	SourceLayer string                     `json:"source-layer"`
	MinZoom     *float64                   `json:"minzoom"`
	MaxZoom     *float64                   `json:"maxzoom"`
	Filter      any                        `json:"filter"`
	Paint       map[string]json.RawMessage `json:"paint"`
	Layout      map[string]json.RawMessage `json:"layout"`
}

// CompiledLayer holds decoded expressions, not pre-evaluated paint. Treat all
// reachable maps/slices as immutable after publication. Order is the original
// document index, including unsupported layer types and duplicate IDs.
type CompiledLayer struct {
	Order       int
	ID          string
	Kind        string
	SourceLayer string
	MinZoom     float64
	MaxZoom     float64
	Filter      any
	Paint       map[string]any
	Layout      map[string]any
}

// Parse decodes and compiles an application-owned style without retaining the
// input byte buffer. This is the existing permissive subset, not a full validator
// or an untrusted-input resource policy; callers bound input size and work.
// Errors return nil layers, never a partially compiled document.
func Parse(data []byte) ([]CompiledLayer, error) {
	var document Document
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("decode style: %w", err)
	}
	return Compile(document)
}

// Compile preserves layer order and decodes paint/layout values. It borrows
// Filter expressions but owns output maps and decoded paint/layout expressions.
// Zoom pointers and raw JSON buffers are not retained. Failure is atomic.
func Compile(document Document) ([]CompiledLayer, error) {
	if document.Version != 8 {
		return nil, fmt.Errorf("unsupported style version %d", document.Version)
	}
	layers := make([]CompiledLayer, 0, len(document.Layers))
	for order, layer := range document.Layers {
		compiled := CompiledLayer{
			Order: order, ID: layer.ID, Kind: layer.Type, SourceLayer: layer.SourceLayer,
			MinZoom: 0, MaxZoom: math.Inf(1), Filter: layer.Filter,
		}
		if layer.MinZoom != nil {
			compiled.MinZoom = *layer.MinZoom
		}
		if layer.MaxZoom != nil {
			compiled.MaxZoom = *layer.MaxZoom
		}
		var err error
		compiled.Paint, err = decodeLayerValues(layer.ID, "paint", layer.Paint)
		if err != nil {
			return nil, err
		}
		compiled.Layout, err = decodeLayerValues(layer.ID, "layout", layer.Layout)
		if err != nil {
			return nil, err
		}
		layers = append(layers, compiled)
	}
	return layers, nil
}

func decodeLayerValues(id, section string, raw map[string]json.RawMessage) (map[string]any, error) {
	values := make(map[string]any, len(raw))
	for name, data := range raw {
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			return nil, fmt.Errorf("decode layer %s %s %s: %w", id, section, name, err)
		}
		values[name] = value
	}
	return values, nil
}

// VisibleAt checks only the half-open zoom range [MinZoom, MaxZoom).
func (l CompiledLayer) VisibleAt(zoom float64) bool {
	return zoom >= l.MinZoom && zoom < l.MaxZoom
}

// Hidden recognizes only literal layout visibility "none", as in the existing
// renderer. It does not evaluate visibility expressions or consult paint.
func (l CompiledLayer) Hidden() bool {
	return l.Layout["visibility"] == "none"
}

// Matches evaluates the optional filter; missing filters match every feature.
func (l CompiledLayer) Matches(context Context) bool {
	if l.Filter == nil {
		return true
	}
	value, ok := Evaluate(l.Filter, context)
	return ok && Truthy(value)
}

// Value evaluates paint before layout. Present paint (even null or a failing
// expression) shadows layout. Returned composites may alias immutable inputs.
func (l CompiledLayer) Value(name string, context Context) (any, bool) {
	value, exists := l.Paint[name]
	if !exists {
		value, exists = l.Layout[name]
	}
	if !exists {
		return nil, false
	}
	return Evaluate(value, context)
}
