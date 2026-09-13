package style

import (
	"math"
	"strings"
)

// NumberValue evaluates a finite number, using fallback for missing, failed,
// nonnumeric or nonfinite values. Fallback itself is caller-owned paint policy.
func (l CompiledLayer) NumberValue(name string, context Context, fallback float64) float64 {
	value, exists := l.Value(name, context)
	if !exists {
		return fallback
	}
	number, ok := Number(value)
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
		return fallback
	}
	return number
}

// StringValue accepts strings without coercing other primitive values.
func (l CompiledLayer) StringValue(name string, context Context, fallback string) string {
	value, exists := l.Value(name, context)
	if !exists {
		return fallback
	}
	text, ok := value.(string)
	if !ok {
		return fallback
	}
	return text
}

// BoolValue accepts booleans, rather than using expression truthiness.
func (l CompiledLayer) BoolValue(name string, context Context, fallback bool) bool {
	value, exists := l.Value(name, context)
	if !exists {
		return fallback
	}
	boolean, ok := value.(bool)
	if !ok {
		return fallback
	}
	return boolean
}

// ColorValue uses fallback for missing/failed evaluation, but preserves parse
// failure for a present invalid color. This distinction is used by legacy paint.
func (l CompiledLayer) ColorValue(name string, context Context, fallback Color) (Color, bool) {
	value, exists := l.Value(name, context)
	if !exists {
		return fallback, true
	}
	return ParseColor(value)
}

// NumberArrayValue returns an owned numeric slice, or nil on missing/invalid
// values. It retains the original signed and nonfinite component behavior;
// unlike NumberValue it does not enforce finiteness or nonnegative values.
func (l CompiledLayer) NumberArrayValue(name string, context Context) []float64 {
	value, exists := l.Value(name, context)
	if !exists {
		return nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	numbers := make([]float64, 0, len(items))
	for _, item := range items {
		number, ok := Number(item)
		if !ok {
			return nil
		}
		numbers = append(numbers, number)
	}
	return numbers
}

// FontStack evaluates text-font, retaining order and duplicates, trimming names
// and dropping nonstrings/empty names. The first name is the legacy font family;
// the full stack is comma-joined. Empty/invalid values use Noto Sans Regular.
func (l CompiledLayer) FontStack(context Context) (string, string) {
	value, exists := l.Value("text-font", context)
	if !exists {
		return "Noto Sans Regular", "Noto Sans Regular"
	}
	fonts, ok := value.([]any)
	if !ok {
		return "Noto Sans Regular", "Noto Sans Regular"
	}
	fontStack := make([]string, 0, len(fonts))
	for _, value := range fonts {
		font, ok := value.(string)
		if !ok || strings.TrimSpace(font) == "" {
			continue
		}
		fontStack = append(fontStack, strings.TrimSpace(font))
	}
	if len(fontStack) == 0 {
		return "Noto Sans Regular", "Noto Sans Regular"
	}
	return fontStack[0], strings.Join(fontStack, ",")
}
