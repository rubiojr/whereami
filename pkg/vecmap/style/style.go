// Package style evaluates vecmap's existing Liberty expression/color subset.
// It depends only on the standard library and contains no toolkit or I/O state.
package style

const MaxExpressionDepth = 64

// Context borrows immutable feature properties for synchronous evaluation.
// GeometryType uses MVT's 1=Point, 2=LineString, 3=Polygon codes.
type Context struct {
	Zoom         float64
	GeometryType uint32
	Properties   Properties // nil has no properties
}

// Properties are a feature's properties, read by name.
type Properties interface {
	Get(name string) (any, bool)
}

// MapProperties are Properties held in a map.
type MapProperties map[string]any

func (m MapProperties) Get(name string) (any, bool) {
	value, ok := m[name]
	return value, ok
}

// Evaluate reuses the existing depth-limited interpreter. Expressions are trusted
// application-owned JSON-style values; the depth limit is not a total work/size
// budget. The supported subset and permissive fallback semantics are documented
// in README.md. Inputs are not mutated or retained, but returned literal/property
// arrays and maps may alias them. Callers must keep inputs/results immutable.
func Evaluate(expression any, context Context) (any, bool) {
	return evaluateLibertyExpression(expression, libertyEvaluation{
		zoom: context.Zoom, geometryID: context.GeometryType, properties: context.Properties,
	})
}

func Number(value any) (float64, bool)   { return libertyNumber(value) }
func String(value any) string            { return libertyString(value) }
func Truthy(value any) bool              { return libertyTruthy(value) }
func ParseColor(value any) (Color, bool) { return parseLibertyColor(value) }
func ColorWithOpacity(color Color, opacity float64) Color {
	return libertyColorWithOpacity(color, opacity)
}
