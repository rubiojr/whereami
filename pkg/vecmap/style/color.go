package style

import (
	"math"
	"strconv"
	"strings"
)

// Color preserves the compiler's integer RGBA channels and rounding behavior.
// Parsing retains the existing permissive range behavior; it does not clamp RGB.
type Color struct {
	Red   int
	Green int
	Blue  int
	Alpha int
}

type mapColor = Color

func libertyMixChannel(first, second int, factor float64) int {
	return int(math.Round(float64(first) + float64(second-first)*factor))
}

func parseLibertyColor(value any) (mapColor, bool) {
	if color, ok := value.(mapColor); ok {
		return color, true
	}
	text, ok := value.(string)
	if !ok {
		return mapColor{}, false
	}
	text = strings.TrimSpace(strings.ToLower(text))
	switch text {
	case "transparent":
		return mapColor{}, true
	case "black":
		return mapColor{Alpha: 255}, true
	case "white":
		return mapColor{Red: 255, Green: 255, Blue: 255, Alpha: 255}, true
	}
	if strings.HasPrefix(text, "#") {
		return parseLibertyHexColor(text)
	}
	if strings.HasPrefix(text, "rgb(") || strings.HasPrefix(text, "rgba(") {
		return parseLibertyRGBColor(text)
	}
	if strings.HasPrefix(text, "hsl(") || strings.HasPrefix(text, "hsla(") {
		return parseLibertyHSLColor(text)
	}
	return mapColor{}, false
}

func parseLibertyHexColor(text string) (mapColor, bool) {
	hex := strings.TrimPrefix(text, "#")
	if len(hex) == 3 || len(hex) == 4 {
		expanded := make([]byte, 0, len(hex)*2)
		for index := range hex {
			expanded = append(expanded, hex[index], hex[index])
		}
		hex = string(expanded)
	}
	if len(hex) != 6 && len(hex) != 8 {
		return mapColor{}, false
	}
	parsed, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return mapColor{}, false
	}
	if len(hex) == 6 {
		return mapColor{Red: int(parsed >> 16), Green: int(parsed >> 8 & 0xff), Blue: int(parsed & 0xff), Alpha: 255}, true
	}
	return mapColor{Red: int(parsed >> 24), Green: int(parsed >> 16 & 0xff), Blue: int(parsed >> 8 & 0xff), Alpha: int(parsed & 0xff)}, true
}

func parseLibertyRGBColor(text string) (mapColor, bool) {
	values, ok := libertyColorArguments(text)
	if !ok || len(values) < 3 {
		return mapColor{}, false
	}
	alpha := 255
	if len(values) == 4 {
		alpha = int(math.Round(values[3] * 255))
	}
	return mapColor{Red: int(values[0]), Green: int(values[1]), Blue: int(values[2]), Alpha: alpha}, true
}

func parseLibertyHSLColor(text string) (mapColor, bool) {
	values, ok := libertyColorArguments(strings.ReplaceAll(text, "%", ""))
	if !ok || len(values) < 3 {
		return mapColor{}, false
	}
	hue := math.Mod(values[0], 360) / 360
	saturation := values[1] / 100
	lightness := values[2] / 100
	red, green, blue := libertyHSLToRGB(hue, saturation, lightness)
	alpha := 255
	if len(values) == 4 {
		alpha = int(math.Round(values[3] * 255))
	}
	return mapColor{Red: red, Green: green, Blue: blue, Alpha: alpha}, true
}

func libertyColorArguments(text string) ([]float64, bool) {
	start := strings.IndexByte(text, '(')
	end := strings.LastIndexByte(text, ')')
	if start < 0 || end <= start {
		return nil, false
	}
	parts := strings.Split(text[start+1:end], ",")
	values := make([]float64, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil {
			return nil, false
		}
		values = append(values, value)
	}
	return values, true
}

func libertyHSLToRGB(hue, saturation, lightness float64) (int, int, int) {
	if saturation == 0 {
		channel := int(math.Round(lightness * 255))
		return channel, channel, channel
	}
	q := lightness * (1 + saturation)
	if lightness >= 0.5 {
		q = lightness + saturation - lightness*saturation
	}
	p := 2*lightness - q
	convert := func(value float64) int {
		if value < 0 {
			value++
		}
		if value > 1 {
			value--
		}
		var channel float64
		switch {
		case value < 1.0/6:
			channel = p + (q-p)*6*value
		case value < 0.5:
			channel = q
		case value < 2.0/3:
			channel = p + (q-p)*(2.0/3-value)*6
		default:
			channel = p
		}
		return int(math.Round(channel * 255))
	}
	return convert(hue + 1.0/3), convert(hue), convert(hue - 1.0/3)
}

func libertyColorWithOpacity(color mapColor, opacity float64) mapColor {
	color.Alpha = int(math.Round(float64(color.Alpha) * max(0, min(1, opacity))))
	return color
}
