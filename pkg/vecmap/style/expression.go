package style

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
)

const maxLibertyExpressionDepth = MaxExpressionDepth

type libertyEvaluation struct {
	zoom            float64
	geometryID      uint32
	properties      Properties
	expressionDepth int
}

// property reads a feature property; no properties means none exist.
func (e libertyEvaluation) property(name string) (any, bool) {
	if e.properties == nil {
		return nil, false
	}
	return e.properties.Get(name)
}

func evaluateLibertyExpression(expression any, evaluation libertyEvaluation) (any, bool) {
	if evaluation.expressionDepth >= maxLibertyExpressionDepth {
		return nil, false
	}
	evaluation.expressionDepth++
	items, isExpression := expression.([]any)
	if !isExpression {
		return expression, true
	}
	if len(items) == 0 {
		return items, true
	}
	operator, ok := items[0].(string)
	if !ok {
		return items, true
	}
	switch operator {
	case "get":
		if len(items) != 2 {
			return nil, false
		}
		name, ok := items[1].(string)
		if !ok {
			return nil, false
		}
		value, exists := evaluation.property(name)
		if !exists {
			return nil, true
		}
		return value, true
	case "has":
		if len(items) != 2 {
			return false, true
		}
		name, ok := items[1].(string)
		if !ok {
			return false, true
		}
		_, exists := evaluation.property(name)
		return exists, true
	case "zoom":
		return evaluation.zoom, true
	case "geometry-type":
		switch evaluation.geometryID {
		case 1:
			return "Point", true
		case 2:
			return "LineString", true
		case 3:
			return "Polygon", true
		default:
			return "Unknown", true
		}
	case "!":
		value, ok := libertyOperand(items, 1, evaluation)
		return !libertyTruthy(value), ok
	case "all":
		for _, item := range items[1:] {
			value, ok := evaluateLibertyExpression(item, evaluation)
			if !ok || !libertyTruthy(value) {
				return false, ok
			}
		}
		return true, true
	case "==", "!=", "<", "<=", ">", ">=":
		left, leftOK := libertyOperand(items, 1, evaluation)
		right, rightOK := libertyOperand(items, 2, evaluation)
		if !leftOK || !rightOK {
			return false, true
		}
		return libertyCompare(operator, left, right), true
	case "match":
		return evaluateLibertyMatch(items, evaluation)
	case "case":
		for index := 1; index+1 < len(items)-1; index += 2 {
			condition, ok := evaluateLibertyExpression(items[index], evaluation)
			if ok && libertyTruthy(condition) {
				return evaluateLibertyExpression(items[index+1], evaluation)
			}
		}
		if len(items) >= 2 {
			return evaluateLibertyExpression(items[len(items)-1], evaluation)
		}
	case "coalesce":
		for _, item := range items[1:] {
			value, ok := evaluateLibertyExpression(item, evaluation)
			if ok && value != nil {
				return value, true
			}
		}
		return nil, false
	case "concat":
		var builder strings.Builder
		for _, item := range items[1:] {
			value, ok := evaluateLibertyExpression(item, evaluation)
			if !ok {
				continue
			}
			builder.WriteString(libertyString(value))
		}
		return builder.String(), true
	case "to-string":
		value, ok := libertyOperand(items, 1, evaluation)
		return libertyString(value), ok
	case "interpolate":
		return evaluateLibertyInterpolate(items, evaluation)
	case "step":
		return evaluateLibertyStep(items, evaluation)
	}
	for _, item := range items {
		if _, ok := item.(string); !ok {
			return nil, false
		}
	}
	// Style arrays such as text-font are literals even though their first
	// element is a string.
	return expression, true
}

func libertyOperand(items []any, index int, evaluation libertyEvaluation) (any, bool) {
	if index >= len(items) {
		return nil, false
	}
	return evaluateLibertyExpression(items[index], evaluation)
}

func evaluateLibertyMatch(items []any, evaluation libertyEvaluation) (any, bool) {
	input, ok := libertyOperand(items, 1, evaluation)
	if !ok || len(items) < 5 {
		return nil, false
	}
	for index := 2; index+1 < len(items)-1; index += 2 {
		if libertyMatchValue(input, items[index]) {
			return evaluateLibertyExpression(items[index+1], evaluation)
		}
	}
	return evaluateLibertyExpression(items[len(items)-1], evaluation)
}

func libertyMatchValue(input, label any) bool {
	if labels, ok := label.([]any); ok {
		for _, candidate := range labels {
			if libertyCompare("==", input, candidate) {
				return true
			}
		}
		return false
	}
	return libertyCompare("==", input, label)
}

func evaluateLibertyInterpolate(items []any, evaluation libertyEvaluation) (any, bool) {
	if len(items) < 7 {
		return nil, false
	}
	inputValue, ok := evaluateLibertyExpression(items[2], evaluation)
	input, okNumber := libertyNumber(inputValue)
	if !ok || !okNumber {
		return nil, false
	}
	base := 1.0
	if descriptor, ok := items[1].([]any); ok && len(descriptor) > 1 && descriptor[0] == "exponential" {
		base, _ = libertyNumber(descriptor[1])
	}
	previousStop, _ := libertyNumber(items[3])
	previousValue, _ := evaluateLibertyExpression(items[4], evaluation)
	if input <= previousStop {
		return previousValue, true
	}
	for index := 5; index+1 < len(items); index += 2 {
		stop, valid := libertyNumber(items[index])
		if !valid {
			return nil, false
		}
		nextValue, _ := evaluateLibertyExpression(items[index+1], evaluation)
		if input <= stop {
			factor := libertyInterpolationFactor(base, input, previousStop, stop)
			return libertyInterpolateValue(previousValue, nextValue, factor), true
		}
		previousStop = stop
		previousValue = nextValue
	}
	return previousValue, true
}

func evaluateLibertyStep(items []any, evaluation libertyEvaluation) (any, bool) {
	if len(items) < 3 {
		return nil, false
	}
	inputValue, ok := evaluateLibertyExpression(items[1], evaluation)
	input, valid := libertyNumber(inputValue)
	if !ok || !valid {
		return nil, false
	}
	output, _ := evaluateLibertyExpression(items[2], evaluation)
	for index := 3; index+1 < len(items); index += 2 {
		stop, valid := libertyNumber(items[index])
		if !valid || input < stop {
			break
		}
		output, _ = evaluateLibertyExpression(items[index+1], evaluation)
	}
	return output, true
}

func libertyInterpolationFactor(base, input, lower, upper float64) float64 {
	if upper == lower {
		return 0
	}
	if base == 1 {
		return (input - lower) / (upper - lower)
	}
	return (math.Pow(base, input-lower) - 1) / (math.Pow(base, upper-lower) - 1)
}

func libertyInterpolateValue(first, second any, factor float64) any {
	if firstNumber, firstOK := libertyNumber(first); firstOK {
		if secondNumber, secondOK := libertyNumber(second); secondOK {
			return firstNumber + (secondNumber-firstNumber)*factor
		}
	}
	firstColor, firstOK := parseLibertyColor(first)
	secondColor, secondOK := parseLibertyColor(second)
	if firstOK && secondOK {
		return mapColor{
			Red:   libertyMixChannel(firstColor.Red, secondColor.Red, factor),
			Green: libertyMixChannel(firstColor.Green, secondColor.Green, factor),
			Blue:  libertyMixChannel(firstColor.Blue, secondColor.Blue, factor),
			Alpha: libertyMixChannel(firstColor.Alpha, secondColor.Alpha, factor),
		}
	}
	if factor < 0.5 {
		return first
	}
	return second
}

func libertyCompare(operator string, left, right any) bool {
	if leftNumber, leftOK := libertyNumber(left); leftOK {
		if rightNumber, rightOK := libertyNumber(right); rightOK {
			switch operator {
			case "==":
				return leftNumber == rightNumber
			case "!=":
				return leftNumber != rightNumber
			case "<":
				return leftNumber < rightNumber
			case "<=":
				return leftNumber <= rightNumber
			case ">":
				return leftNumber > rightNumber
			case ">=":
				return leftNumber >= rightNumber
			}
		}
	}
	leftString, leftStringOK := left.(string)
	rightString, rightStringOK := right.(string)
	if leftStringOK && rightStringOK {
		switch operator {
		case "==":
			return leftString == rightString
		case "!=":
			return leftString != rightString
		case "<":
			return leftString < rightString
		case "<=":
			return leftString <= rightString
		case ">":
			return leftString > rightString
		case ">=":
			return leftString >= rightString
		}
	}
	// Malformed expressions can produce arrays/maps (or structs containing them).
	// Go interface equality would panic for those values. They have no supported
	// scalar equality in this subset, so treat them as unequal.
	if !scalarComparable(left) || !scalarComparable(right) {
		return operator == "!="
	}
	if operator == "!=" {
		return left != right
	}
	return left == right
}

func scalarComparable(value any) bool {
	switch value.(type) {
	case nil, bool, string, float64, float32, int, int64, uint64, json.Number, Color:
		// In particular, avoid reflect.Value.Comparable's allocating struct-field
		// iterator on Go 1.27 for the common interpolated Color value.
		return true
	default:
		return reflect.ValueOf(value).Comparable()
	}
}

func libertyTruthy(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case nil:
		return false
	case string:
		return typed != ""
	case float64:
		return typed != 0
	default:
		return true
	}
}

func libertyNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case json.Number:
		number, err := typed.Float64()
		return number, err == nil
	default:
		return 0, false
	}
}

func libertyString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}
