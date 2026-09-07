package vecmap

import (
	"math"
	"strings"
	"unicode/utf8"
)

const (
	maxTileSymbols         = 10_000
	maximumSymbolTextBytes = 4_096
	maximumSymbolTextRunes = 256
)

type libertySymbolCandidate struct {
	order               int
	layerID             string
	anchor              roadPoint
	lineAngle           float64
	iconLineAngle       float64
	viewportAligned     bool
	iconViewportAligned bool
	sortKey             float64
	textAllowsOverlap   bool
	iconAllowsOverlap   bool
	textOptional        bool
	iconOptional        bool
	textPadding         float64
	iconPadding         float64

	text          string
	fontFamily    string
	fontStack     string
	textSize      float64
	textColor     mapColor
	haloColor     mapColor
	haloWidth     float64
	haloBlur      float64
	letterSpacing float64
	lineHeight    float64
	maximumWidth  float64
	textAnchor    string
	textJustify   string
	textOffset    roadPoint
	textRotate    float64

	iconName    string
	iconSize    float64
	iconColor   mapColor
	iconOpacity float64
	iconAnchor  string
	iconOffset  roadPoint
	iconRotate  float64
}

func compileLibertySymbolLayer(bucket *tileBucket, layer compiledLibertyLayer, zoom float64) error {
	features := bucket.sourceLayers[layer.sourceLayer]
	for _, feature := range features {
		evaluation := libertyEvaluation{zoom: zoom, geometryID: feature.geometryID, properties: feature.properties}
		if !layer.matches(evaluation) {
			continue
		}
		text := libertyEvaluatedString(layer, "text-field", evaluation, "")
		text = expandLibertyTokens(text, feature.properties)
		switch libertyEvaluatedString(layer, "text-transform", evaluation, "none") {
		case "uppercase":
			text = strings.ToUpper(text)
		case "lowercase":
			text = strings.ToLower(text)
		}
		text = normalizeLibertySymbolText(text)
		text = boundedLibertySymbolText(text)
		iconName := libertyEvaluatedString(layer, "icon-image", evaluation, "")
		iconName = expandLibertyTokens(iconName, feature.properties)
		if text == "" && iconName == "" {
			continue
		}
		placement := libertyEvaluatedString(layer, "symbol-placement", evaluation, "point")
		spacing := libertySymbolSpacing(
			libertyEvaluatedNumber(layer, "symbol-spacing", evaluation, 250),
			bucket.tile.Z,
			zoom,
		)
		fontFamily, fontStack := libertyEvaluatedFonts(layer, evaluation)
		anchors := libertyFeatureAnchors(feature, placement, spacing)
		for _, anchor := range anchors {
			if len(bucket.symbols) >= maxTileSymbols {
				return errFeatureResourceLimit
			}
			candidate := libertySymbolCandidate{
				order:               layer.order,
				layerID:             layer.id,
				anchor:              anchor.point,
				lineAngle:           anchor.angle,
				iconLineAngle:       anchor.rawAngle,
				viewportAligned:     placement == "point",
				iconViewportAligned: placement == "point",
				sortKey:             libertyEvaluatedNumber(layer, "symbol-sort-key", evaluation, 0),
				textAllowsOverlap:   libertyEvaluatedBool(layer, "text-allow-overlap", evaluation, false),
				iconAllowsOverlap:   libertyEvaluatedBool(layer, "icon-allow-overlap", evaluation, false),
				textOptional:        libertyEvaluatedBool(layer, "text-optional", evaluation, false),
				iconOptional:        libertyEvaluatedBool(layer, "icon-optional", evaluation, false),
				textPadding:         libertyEvaluatedNumber(layer, "text-padding", evaluation, 2),
				iconPadding:         libertyEvaluatedNumber(layer, "icon-padding", evaluation, 2),
				text:                text,
				fontFamily:          fontFamily,
				fontStack:           fontStack,
				textSize:            libertyEvaluatedNumber(layer, "text-size", evaluation, 16),
				letterSpacing:       libertyEvaluatedNumber(layer, "text-letter-spacing", evaluation, 0),
				lineHeight:          libertyEvaluatedNumber(layer, "text-line-height", evaluation, 1.2),
				maximumWidth:        libertyEvaluatedNumber(layer, "text-max-width", evaluation, 10),
				textAnchor:          libertyEvaluatedString(layer, "text-anchor", evaluation, "center"),
				textJustify:         libertyEvaluatedString(layer, "text-justify", evaluation, "auto"),
				textOffset:          libertyEvaluatedPoint(layer, "text-offset", evaluation),
				textRotate:          libertyEvaluatedNumber(layer, "text-rotate", evaluation, 0) * math.Pi / 180,
				iconName:            iconName,
				iconSize:            libertyEvaluatedNumber(layer, "icon-size", evaluation, 1),
				iconOpacity:         libertyEvaluatedNumber(layer, "icon-opacity", evaluation, 1),
				iconAnchor:          libertyEvaluatedString(layer, "icon-anchor", evaluation, "center"),
				iconOffset:          libertyEvaluatedPoint(layer, "icon-offset", evaluation),
				iconRotate:          libertyEvaluatedNumber(layer, "icon-rotate", evaluation, 0) * math.Pi / 180,
			}
			if !libertyEvaluatedBool(layer, "text-keep-upright", evaluation, true) {
				candidate.lineAngle = anchor.rawAngle
			}
			if libertyEvaluatedBool(layer, "icon-keep-upright", evaluation, false) {
				candidate.iconLineAngle = anchor.angle
			}
			candidate.textColor, _ = libertyEvaluatedColor(layer, "text-color", evaluation, mapColor{alpha: 255})
			candidate.textColor = libertyColorWithOpacity(
				candidate.textColor,
				libertyEvaluatedNumber(layer, "text-opacity", evaluation, 1),
			)
			candidate.haloColor, _ = libertyEvaluatedColor(layer, "text-halo-color", evaluation, mapColor{})
			candidate.haloWidth = libertyEvaluatedNumber(layer, "text-halo-width", evaluation, 0)
			candidate.haloBlur = libertyEvaluatedNumber(layer, "text-halo-blur", evaluation, 0)
			candidate.iconColor, _ = libertyEvaluatedColor(layer, "icon-color", evaluation, mapColor{alpha: 255})
			if alignment := libertyEvaluatedString(layer, "text-rotation-alignment", evaluation, "auto"); alignment == "viewport" {
				candidate.viewportAligned = true
			} else if alignment == "map" {
				candidate.viewportAligned = false
			}
			if alignment := libertyEvaluatedString(layer, "icon-rotation-alignment", evaluation, "auto"); alignment == "viewport" {
				candidate.iconViewportAligned = true
			} else if alignment == "map" {
				candidate.iconViewportAligned = false
			}
			bucket.symbols = append(bucket.symbols, candidate)
		}
	}
	return nil
}

func libertySymbolSpacing(screenPixels float64, tileZoom uint32, styleZoom float64) float64 {
	return screenPixels * math.Exp2(float64(tileZoom)-styleZoom)
}

type libertySymbolAnchor struct {
	point    roadPoint
	angle    float64
	rawAngle float64
}

func libertyFeatureAnchors(feature vectorFeature, placement string, spacing float64) []libertySymbolAnchor {
	if placement == "line" || placement == "line-center" {
		anchors := make([]libertySymbolAnchor, 0, len(feature.lines))
		for _, line := range feature.lines {
			if placement == "line-center" {
				if anchor, ok := libertyLineAnchor(line, 0.5); ok {
					anchors = append(anchors, anchor)
				}
				continue
			}
			anchors = append(anchors, libertyRepeatedLineAnchors(line, spacing)...)
		}
		return anchors
	}
	anchors := make([]libertySymbolAnchor, 0, len(feature.points)+len(feature.polygons))
	for _, point := range feature.points {
		anchors = append(anchors, libertySymbolAnchor{point: point})
	}
	for _, polygon := range feature.polygons {
		anchors = append(anchors, libertySymbolAnchor{point: libertyPolygonCentroid(polygon.exterior)})
	}
	if len(anchors) == 0 {
		for _, line := range feature.lines {
			if anchor, ok := libertyLineAnchor(line, 0.5); ok {
				anchors = append(anchors, anchor)
			}
		}
	}
	return anchors
}

func libertyRepeatedLineAnchors(line []roadPoint, spacing float64) []libertySymbolAnchor {
	length := libertyLineLength(line)
	if length <= polygonEpsilon {
		return nil
	}
	if spacing <= 0 || length < spacing {
		anchor, ok := libertyLineAnchor(line, 0.5)
		if !ok {
			return nil
		}
		return []libertySymbolAnchor{anchor}
	}
	count := min(16, max(1, int(math.Floor(length/spacing))))
	anchors := make([]libertySymbolAnchor, 0, count)
	for index := range count {
		fraction := (float64(index) + 0.5) / float64(count)
		if anchor, ok := libertyLineAnchor(line, fraction); ok {
			anchors = append(anchors, anchor)
		}
	}
	return anchors
}

func libertyLineAnchor(line []roadPoint, fraction float64) (libertySymbolAnchor, bool) {
	total := libertyLineLength(line)
	if total <= polygonEpsilon {
		return libertySymbolAnchor{}, false
	}
	target := max(0, min(1, fraction)) * total
	traversed := 0.0
	for index := 1; index < len(line); index++ {
		first := line[index-1]
		second := line[index]
		length := math.Hypot(second.X-first.X, second.Y-first.Y)
		if length <= polygonEpsilon {
			continue
		}
		if traversed+length >= target {
			factor := (target - traversed) / length
			rawAngle := math.Atan2(second.Y-first.Y, second.X-first.X)
			angle := rawAngle
			if angle > math.Pi/2 || angle < -math.Pi/2 {
				angle += math.Pi
			}
			return libertySymbolAnchor{
				point:    roadPoint{X: first.X + (second.X-first.X)*factor, Y: first.Y + (second.Y-first.Y)*factor},
				angle:    angle,
				rawAngle: rawAngle,
			}, true
		}
		traversed += length
	}
	return libertySymbolAnchor{}, false
}

func libertyLineLength(line []roadPoint) float64 {
	length := 0.0
	for index := 1; index < len(line); index++ {
		length += math.Hypot(line[index].X-line[index-1].X, line[index].Y-line[index-1].Y)
	}
	return length
}

func libertyPolygonCentroid(ring []roadPoint) roadPoint {
	if len(ring) == 0 {
		return roadPoint{}
	}
	area := 0.0
	centroid := roadPoint{}
	for index, first := range ring {
		second := ring[(index+1)%len(ring)]
		cross := first.X*second.Y - second.X*first.Y
		area += cross
		centroid.X += (first.X + second.X) * cross
		centroid.Y += (first.Y + second.Y) * cross
	}
	if math.Abs(area) <= polygonEpsilon {
		for _, point := range ring {
			centroid.X += point.X
			centroid.Y += point.Y
		}
		centroid.X /= float64(len(ring))
		centroid.Y /= float64(len(ring))
		return centroid
	}
	centroid.X /= 3 * area
	centroid.Y /= 3 * area
	return centroid
}

func expandLibertyTokens(text string, properties featureProperties) string {
	const maximumExpansions = 256
	if len(text) > maximumSymbolTextBytes || !utf8.ValidString(text) {
		return ""
	}
	offset := 0
	for range maximumExpansions {
		startOffset := strings.IndexByte(text[offset:], '{')
		if startOffset < 0 {
			return boundedLibertySymbolText(text)
		}
		start := offset + startOffset
		endOffset := strings.IndexByte(text[start+1:], '}')
		if endOffset < 0 {
			return boundedLibertySymbolText(text)
		}
		end := start + endOffset + 1
		name := text[start+1 : end]
		value, _ := properties.get(name)
		replacement := libertyString(value)
		retainedBytes := len(text) - (end + 1 - start)
		if len(replacement) > maximumSymbolTextBytes-retainedBytes {
			return ""
		}
		text = text[:start] + replacement + text[end+1:]
		offset = start + len(replacement)
	}
	return boundedLibertySymbolText(text)
}

func boundedLibertySymbolText(text string) string {
	if len(text) > maximumSymbolTextBytes || !utf8.ValidString(text) || utf8.RuneCountInString(text) > maximumSymbolTextRunes {
		return ""
	}
	return text
}

func libertyEvaluatedBool(layer compiledLibertyLayer, name string, evaluation libertyEvaluation, fallback bool) bool {
	value, exists := layer.value(name, evaluation)
	if !exists {
		return fallback
	}
	boolean, ok := value.(bool)
	if !ok {
		return fallback
	}
	return boolean
}

func libertyEvaluatedPoint(layer compiledLibertyLayer, name string, evaluation libertyEvaluation) roadPoint {
	values := libertyEvaluatedNumberArray(layer, name, evaluation)
	if len(values) != 2 {
		return roadPoint{}
	}
	return roadPoint{X: values[0], Y: values[1]}
}

func libertyEvaluatedNumberArray(layer compiledLibertyLayer, name string, evaluation libertyEvaluation) []float64 {
	value, exists := layer.value(name, evaluation)
	if !exists {
		return nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	numbers := make([]float64, 0, len(items))
	for _, item := range items {
		number, ok := libertyNumber(item)
		if !ok {
			return nil
		}
		numbers = append(numbers, number)
	}
	return numbers
}

func libertyEvaluatedFonts(layer compiledLibertyLayer, evaluation libertyEvaluation) (string, string) {
	value, exists := layer.value("text-font", evaluation)
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
