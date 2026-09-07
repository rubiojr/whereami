package vecmap

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"sync"
)

//go:generate go run ./cmd/libertystylegen

//go:embed liberty_style.json
var libertyStyleJSON []byte

const libertyStyleSHA256 = "6010998863b4876911ac9a2d62c9a28d97c8877f6d20cd158b74808572257b60"

type libertyStyleDocument struct {
	Version int                 `json:"version"`
	Layers  []libertyStyleLayer `json:"layers"`
}

type libertyStyleLayer struct {
	ID          string                     `json:"id"`
	Type        string                     `json:"type"`
	SourceLayer string                     `json:"source-layer"`
	MinZoom     *float64                   `json:"minzoom"`
	MaxZoom     *float64                   `json:"maxzoom"`
	Filter      any                        `json:"filter"`
	Paint       map[string]json.RawMessage `json:"paint"`
	Layout      map[string]json.RawMessage `json:"layout"`
}

type compiledLibertyLayer struct {
	order       int
	id          string
	kind        string
	sourceLayer string
	minZoom     float64
	maxZoom     float64
	filter      any
	paint       map[string]any
	layout      map[string]any
}

var (
	libertyOnce       sync.Once
	libertyLayers     []compiledLibertyLayer
	libertyStyleError error
)

func compiledLibertyLayers() ([]compiledLibertyLayer, error) {
	libertyOnce.Do(func() {
		var document libertyStyleDocument
		if err := json.Unmarshal(libertyStyleJSON, &document); err != nil {
			libertyStyleError = fmt.Errorf("decode embedded Liberty style: %w", err)
			return
		}
		if document.Version != 8 {
			libertyStyleError = fmt.Errorf("unsupported Liberty style version %d", document.Version)
			return
		}
		libertyLayers = make([]compiledLibertyLayer, 0, len(document.Layers))
		for order, layer := range document.Layers {
			compiled := compiledLibertyLayer{
				order:       order,
				id:          layer.ID,
				kind:        layer.Type,
				sourceLayer: layer.SourceLayer,
				minZoom:     0,
				maxZoom:     math.Inf(1),
				filter:      layer.Filter,
				paint:       make(map[string]any, len(layer.Paint)),
				layout:      make(map[string]any, len(layer.Layout)),
			}
			if layer.MinZoom != nil {
				compiled.minZoom = *layer.MinZoom
			}
			if layer.MaxZoom != nil {
				compiled.maxZoom = *layer.MaxZoom
			}
			for name, raw := range layer.Paint {
				var value any
				if err := json.Unmarshal(raw, &value); err != nil {
					libertyStyleError = fmt.Errorf("decode Liberty layer %s paint %s: %w", layer.ID, name, err)
					return
				}
				compiled.paint[name] = value
			}
			for name, raw := range layer.Layout {
				var value any
				if err := json.Unmarshal(raw, &value); err != nil {
					libertyStyleError = fmt.Errorf("decode Liberty layer %s layout %s: %w", layer.ID, name, err)
					return
				}
				compiled.layout[name] = value
			}
			libertyLayers = append(libertyLayers, compiled)
		}
	})
	return libertyLayers, libertyStyleError
}

func (l compiledLibertyLayer) visibleAt(zoom float64) bool {
	return zoom >= l.minZoom && zoom < l.maxZoom
}

func (l compiledLibertyLayer) matches(evaluation libertyEvaluation) bool {
	if l.filter == nil {
		return true
	}
	value, ok := evaluateLibertyExpression(l.filter, evaluation)
	return ok && libertyTruthy(value)
}

func (l compiledLibertyLayer) value(name string, evaluation libertyEvaluation) (any, bool) {
	value, exists := l.paint[name]
	if !exists {
		value, exists = l.layout[name]
	}
	if !exists {
		return nil, false
	}
	return evaluateLibertyExpression(value, evaluation)
}
