// Package sprite prepares bounded, toolkit-neutral sprite atlases and images.
package sprite

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"math"

	"github.com/rubiojr/whereami/pkg/vecmap/style"
)

const (
	MaxIndexBytes = 2 << 20
	MaxPNGBytes   = 16 << 20
	MaxEntries    = 4096
	MaxDimension  = 4096
	MaxPixels     = 4 << 20
)

var (
	ErrInput = errors.New("invalid sprite input")
	ErrLimit = errors.New("sprite resource limit exceeded")
)

// Entry describes a named rectangle in physical atlas pixels.
type Entry struct {
	X          int     `json:"x"`
	Y          int     `json:"y"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	PixelRatio float64 `json:"pixelRatio"`
	SDF        bool    `json:"sdf"`
}

// Atlas owns normalized straight-alpha pixels and name-keyed metadata.
// Treat the image and entries as immutable after publication.
type Atlas struct {
	Pixels  *image.NRGBA
	Entries map[string]Entry
}

// Image owns tightly packed, straight-alpha RGBA bytes prepared for upload.
type Image struct {
	Pixels     []byte
	Width      int
	Height     int
	PixelRatio float64
}

// Decode reuses the standard JSON/PNG decoders and NRGBA conversion. It bounds
// encoded input, entry count and decoded dimensions before allocating PNG pixels.
// Invalid metadata or PNG data fails atomically; inputs are not retained.
func Decode(index, data []byte) (*Atlas, error) {
	if len(index) > MaxIndexBytes || len(data) > MaxPNGBytes {
		return nil, ErrLimit
	}
	entries, err := decodeIndex(index)
	if err != nil {
		return nil, fmt.Errorf("decode sprite index: %w", err)
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode sprite atlas header: %w", err)
	}
	if !dimensionsValid(config.Width, config.Height) {
		return nil, ErrLimit
	}
	for name, entry := range entries {
		if !entryValid(entry, config.Width, config.Height) {
			return nil, fmt.Errorf("sprite %q: %w", name, ErrInput)
		}
	}
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode sprite atlas: %w", err)
	}
	bounds := decoded.Bounds()
	pixels := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(pixels, pixels.Bounds(), decoded, bounds.Min, draw.Src)
	return &Atlas{Pixels: pixels, Entries: entries}, nil
}

func decodeIndex(data []byte) (map[string]Entry, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return nil, ErrInput
	}
	entries := make(map[string]Entry)
	for count := 0; decoder.More(); count++ {
		if count == MaxEntries {
			return nil, ErrLimit
		}
		name, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		var entry Entry
		if err := decoder.Decode(&entry); err != nil {
			return nil, err
		}
		entries[name.(string)] = entry
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, ErrInput
	}
	return entries, nil
}

func dimensionsValid(width, height int) bool {
	return width > 0 && height > 0 && width <= MaxDimension && height <= MaxDimension && width <= MaxPixels/height
}

func entryValid(entry Entry, width, height int) bool {
	return entry.X >= 0 && entry.Y >= 0 && entry.X <= width && entry.Y <= height &&
		entry.Width > 0 && entry.Height > 0 && entry.Width <= width-entry.X && entry.Height <= height-entry.Y &&
		entry.PixelRatio > 0 && !math.IsInf(entry.PixelRatio, 0)
}

// Prepare crops a normalized (zero-origin) atlas using the original sprite tint
// and opacity arithmetic. Non-SDF RGB ignores color; SDF RGB uses its byte values.
// Opacity clamps to [0,1]; NaN rejects. Invalid metadata/storage returns no image.
// Inputs are borrowed only during the call, and output never aliases the atlas.
func Prepare(atlas *image.NRGBA, entry Entry, color style.Color, opacity float64) (Image, bool) {
	if !storageValid(atlas) || !entryValid(entry, atlas.Rect.Max.X, atlas.Rect.Max.Y) || math.IsNaN(opacity) {
		return Image{}, false
	}
	pixels := make([]byte, entry.Width*entry.Height*4)
	alphaScale := max(0, min(1, opacity))
	for y := range entry.Height {
		for x := range entry.Width {
			sourceOffset := (entry.Y+y)*atlas.Stride + (entry.X+x)*4
			targetOffset := (y*entry.Width + x) * 4
			if entry.SDF {
				pixels[targetOffset] = byte(color.Red)
				pixels[targetOffset+1] = byte(color.Green)
				pixels[targetOffset+2] = byte(color.Blue)
				pixels[targetOffset+3] = byte(math.Round(float64(atlas.Pix[sourceOffset+3]) * alphaScale * float64(color.Alpha) / 255))
				continue
			}
			copy(pixels[targetOffset:targetOffset+3], atlas.Pix[sourceOffset:sourceOffset+3])
			pixels[targetOffset+3] = byte(math.Round(float64(atlas.Pix[sourceOffset+3]) * alphaScale))
		}
	}
	return Image{Pixels: pixels, Width: entry.Width, Height: entry.Height, PixelRatio: entry.PixelRatio}, true
}

func storageValid(atlas *image.NRGBA) bool {
	if atlas == nil || atlas.Rect.Min != (image.Point{}) || !dimensionsValid(atlas.Rect.Max.X, atlas.Rect.Max.Y) {
		return false
	}
	rowBytes := atlas.Rect.Max.X * 4
	// Divide remaining storage before multiplication, including on 32-bit Go.
	return atlas.Stride >= rowBytes && len(atlas.Pix) >= rowBytes &&
		atlas.Rect.Max.Y-1 <= (len(atlas.Pix)-rowBytes)/atlas.Stride
}
