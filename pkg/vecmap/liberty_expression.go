package vecmap

import "github.com/rubiojr/whereami/pkg/vecmap/style"

const maxLibertyExpressionDepth = style.MaxExpressionDepth

type libertyEvaluation struct {
	zoom       float64
	geometryID uint32
	properties featureProperties
}

func evaluateLibertyExpression(expression any, evaluation libertyEvaluation) (any, bool) {
	return style.Evaluate(expression, evaluation.context())
}

func (e libertyEvaluation) context() style.Context {
	return style.Context{Zoom: e.zoom, GeometryType: e.geometryID, Properties: e.properties}
}

func libertyNumber(value any) (float64, bool) { return style.Number(value) }
func libertyString(value any) string          { return style.String(value) }
