package quick

// Qt stores geometry buffer sizes and image strides in signed 32-bit integers.
const maxGeometryVertices = (1<<31 - 1) / 16

func validImageSize(size, width, height, channels int) bool {
	const maxInt = int(^uint(0) >> 1)
	return width > 0 && height > 0 && height <= 1<<31-1 &&
		width <= (1<<31-1)/channels && width <= maxInt/channels/height &&
		size == width*height*channels
}

func writePatternVertices(vertices, points []float32, width, height, phaseX, phaseY float32) {
	for source := 0; source < len(points); source += 2 {
		target := source * 2
		x, y := points[source], points[source+1]
		vertices[target] = x
		vertices[target+1] = y
		vertices[target+2] = (x + phaseX) / width
		vertices[target+3] = (y + phaseY) / height
	}
}

// sdfMaterialData matches the contiguous shader uniforms at byte offsets 80–123:
// fill RGBA, halo RGBA, font scale, halo width, halo blur. The native shader can
// copy this block unchanged, without color conversion or per-field packing.
type sdfMaterialData [11]float32

func sdfTextPasses(color, halo [4]int, scale, width, blur float32) ([2]sdfMaterialData, int) {
	var passes [2]sdfMaterialData
	fill := sdfMaterialData{8: scale}
	for index, value := range color {
		fill[index] = float32(min(255, max(0, value))) / 255
	}
	if width <= 0 || halo[3] <= 0 {
		passes[0] = fill
		return passes, 1
	}
	// All halos precede all fills to avoid covering adjacent glyphs' interiors.
	passes[0] = sdfMaterialData{8: scale, 9: width, 10: max(0, blur)}
	for index, value := range halo {
		passes[0][4+index] = float32(min(255, max(0, value))) / 255
	}
	passes[1] = fill
	return passes, 2
}
