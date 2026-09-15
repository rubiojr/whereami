package sprite

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/rubiojr/whereami/pkg/vecmap/style"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func encodePNG(t testing.TB, img image.Image) []byte {
	t.Helper()
	var data bytes.Buffer
	require.NoError(t, png.Encode(&data, img))
	return data.Bytes()
}

func TestDecodeOwnershipAndConversion(t *testing.T) {
	for _, img := range []image.Image{
		&image.NRGBA{Pix: []byte{10, 20, 30, 128}, Stride: 4, Rect: image.Rect(0, 0, 1, 1)},
		&image.RGBA{Pix: []byte{10, 20, 30, 128}, Stride: 4, Rect: image.Rect(0, 0, 1, 1)},
		&image.Gray{Pix: []byte{42}, Stride: 1, Rect: image.Rect(0, 0, 1, 1)},
		&image.Paletted{Pix: []byte{0}, Stride: 1, Rect: image.Rect(0, 0, 1, 1), Palette: color.Palette{color.NRGBA{R: 10, G: 20, B: 30, A: 128}}},
	} {
		data := encodePNG(t, img)
		index := []byte(`{"a":{"x":0,"y":0,"width":1,"height":1,"pixelRatio":2,"sdf":true,"unknown":42}}`)
		atlas, err := Decode(index, data)
		require.NoError(t, err)
		decoded, err := png.Decode(bytes.NewReader(data))
		require.NoError(t, err)
		want := image.NewNRGBA(image.Rect(0, 0, 1, 1))
		draw.Draw(want, want.Rect, decoded, image.Point{}, draw.Src)
		clear(data)
		clear(index)
		assert.Equal(t, want, atlas.Pixels)
		assert.Equal(t, Entry{Width: 1, Height: 1, PixelRatio: 2, SDF: true}, atlas.Entries["a"])
	}
}

func pngDimensions(data []byte, width, height uint32) []byte {
	data = bytes.Clone(data)
	binary.BigEndian.PutUint32(data[16:20], width)
	binary.BigEndian.PutUint32(data[20:24], height)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	return data
}

func TestDecodeRejections(t *testing.T) {
	valid := encodePNG(t, image.NewNRGBA(image.Rect(0, 0, 1, 1)))
	for _, index := range []string{"", "null", "[]", "{", `{"a"`, `{"a":`, `{"a":{}`, `{"a":{}}`, `{"a":null}`, `{"a":{"width":"1"}}`, `{} {}`, `{} !`, `{"a":1}`, `{"a":{},]`, `{"a":{}]`} {
		t.Run(index, func(t *testing.T) {
			atlas, err := Decode([]byte(index), valid)
			assert.Error(t, err)
			assert.Nil(t, atlas)
		})
	}
	for _, data := range [][]byte{nil, []byte("not png"), valid[:33], pngDimensions(valid, MaxDimension+1, 1), pngDimensions(valid, MaxDimension, MaxDimension)} {
		atlas, err := Decode([]byte(`{}`), data)
		assert.Error(t, err)
		assert.Nil(t, atlas)
	}
	for _, inputs := range [][2][]byte{{make([]byte, MaxIndexBytes+1), valid}, {[]byte(`{}`), make([]byte, MaxPNGBytes+1)}} {
		atlas, err := Decode(inputs[0], inputs[1])
		assert.ErrorIs(t, err, ErrLimit)
		assert.Nil(t, atlas)
	}
	// Entry occurrences, including duplicate names, count towards the work bound.
	entry := `"a":{"width":1,"height":1,"pixelRatio":1}`
	index := "{" + strings.Repeat(entry+",", MaxEntries-1) + entry + "}"
	atlas, err := Decode([]byte(index), valid)
	require.NoError(t, err)
	assert.Len(t, atlas.Entries, 1)
	atlas, err = Decode([]byte("{"+strings.Repeat(entry+",", MaxEntries)+entry+"}"), valid)
	assert.ErrorIs(t, err, ErrLimit)
	assert.Nil(t, atlas)
	// Exact byte ceilings are accepted; whitespace and trailing PNG data are bounded.
	_, err = Decode([]byte("{}"+strings.Repeat(" ", MaxIndexBytes-2)), append(valid, make([]byte, MaxPNGBytes-len(valid))...))
	require.NoError(t, err)
	assert.True(t, dimensionsValid(MaxDimension, MaxPixels/MaxDimension))
	assert.False(t, dimensionsValid(0, 1))
	assert.False(t, dimensionsValid(1, 0))
	assert.False(t, dimensionsValid(1, MaxDimension+1))
}

func TestPreparePixelsAndOwnership(t *testing.T) {
	// Extra row stride and a nonzero crop origin exercise storage addressing.
	atlas := &image.NRGBA{Pix: []byte{0, 0, 0, 0, 10, 20, 30, 255, 99, 99, 99, 99, 0, 0, 0, 0, 40, 50, 60, 127}, Stride: 12, Rect: image.Rect(0, 0, 2, 2)}
	before := bytes.Clone(atlas.Pix)
	entry := Entry{X: 1, Width: 1, Height: 2, PixelRatio: 2}
	tint := style.Color{Red: 1, Green: 2, Blue: 3, Alpha: 128}
	for _, test := range []struct {
		name    string
		sdf     bool
		opacity float64
		want    []byte
	}{
		{"rgb half", false, 0.5, []byte{10, 20, 30, 128, 40, 50, 60, 64}},
		{"sdf half", true, 0.5, []byte{1, 2, 3, 64, 1, 2, 3, 32}},
		{"negative", false, -1, []byte{10, 20, 30, 0, 40, 50, 60, 0}},
		{"infinity", false, math.Inf(1), []byte{10, 20, 30, 255, 40, 50, 60, 127}},
		{"negative infinity", true, math.Inf(-1), []byte{1, 2, 3, 0, 1, 2, 3, 0}},
	} {
		t.Run(test.name, func(t *testing.T) {
			entry.SDF = test.sdf
			got, ok := Prepare(atlas, entry, tint, test.opacity)
			require.True(t, ok)
			assert.Equal(t, Image{Pixels: test.want, Width: 1, Height: 2, PixelRatio: 2}, got)
			clear(got.Pixels)
			assert.Equal(t, before, atlas.Pix)
		})
	}
	// Retain legacy byte conversion for integer RGB values, rather than clamping.
	entry.SDF = true
	got, ok := Prepare(atlas, entry, style.Color{Red: -1, Green: 256, Blue: 257, Alpha: 255}, 1)
	require.True(t, ok)
	assert.Equal(t, []byte{255, 0, 1, 255, 255, 0, 1, 127}, got.Pixels)
}

func TestPrepareRejectsInvalidStorageAndMetadata(t *testing.T) {
	atlas := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	entry := Entry{Width: 2, Height: 2, PixelRatio: 1}
	for _, bad := range []*image.NRGBA{
		nil, {}, {Rect: image.Rect(-1, 0, 1, 2)}, {Rect: image.Rect(0, 0, MaxDimension+1, 1)},
		{Pix: atlas.Pix, Rect: atlas.Rect, Stride: -1}, {Pix: atlas.Pix, Rect: atlas.Rect, Stride: 7},
		{Pix: atlas.Pix[:7], Rect: atlas.Rect, Stride: 8}, {Pix: atlas.Pix[:15], Rect: atlas.Rect, Stride: 8},
		{Pix: atlas.Pix, Rect: atlas.Rect, Stride: math.MaxInt},
	} {
		got, ok := Prepare(bad, entry, style.Color{}, 1)
		assert.False(t, ok)
		assert.Equal(t, Image{}, got)
	}
	for _, change := range []func(*Entry){
		func(e *Entry) { e.X = -1 }, func(e *Entry) { e.Y = -1 },
		func(e *Entry) { e.X = math.MaxInt }, func(e *Entry) { e.Y = math.MaxInt },
		func(e *Entry) { e.Width = 0 }, func(e *Entry) { e.Height = -1 },
		func(e *Entry) { e.Width = math.MaxInt }, func(e *Entry) { e.Height = math.MaxInt },
		func(e *Entry) { e.X = 1 }, func(e *Entry) { e.Y = 1 },
		func(e *Entry) { e.PixelRatio = 0 }, func(e *Entry) { e.PixelRatio = math.NaN() },
		func(e *Entry) { e.PixelRatio = math.Inf(1) },
	} {
		bad := entry
		change(&bad)
		got, ok := Prepare(atlas, bad, style.Color{}, 1)
		assert.False(t, ok)
		assert.Equal(t, Image{}, got)
	}
	got, ok := Prepare(atlas, entry, style.Color{}, math.NaN())
	assert.False(t, ok)
	assert.Equal(t, Image{}, got)
}

func TestPinnedSpritesHeadless(t *testing.T) {
	index, err := os.ReadFile("../liberty/liberty_sprite.json")
	require.NoError(t, err)
	data, err := os.ReadFile("../liberty/liberty_sprite.png")
	require.NoError(t, err)
	atlas, err := Decode(index, data)
	require.NoError(t, err)
	require.Len(t, atlas.Entries, 264)
	var legacyEntries map[string]Entry
	require.NoError(t, json.Unmarshal(index, &legacyEntries))
	assert.Equal(t, legacyEntries, atlas.Entries)
	decoded, err := png.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	for name, entry := range atlas.Entries {
		t.Run(name, func(t *testing.T) {
			got, ok := Prepare(atlas.Pixels, entry, style.Color{Red: 255, Green: 255, Blue: 255, Alpha: 255}, 1)
			require.True(t, ok)
			want := image.NewNRGBA(image.Rect(0, 0, entry.Width, entry.Height))
			draw.Draw(want, want.Rect, decoded, image.Pt(entry.X, entry.Y), draw.Src)
			assert.Equal(t, want.Pix, got.Pixels)
		})
	}
}

func FuzzDecode(f *testing.F) {
	data := encodePNG(f, image.NewNRGBA(image.Rect(0, 0, 1, 1)))
	f.Add([]byte(`{}`), data)
	f.Add([]byte(`{"a":{"width":1,"height":1,"pixelRatio":1}}`), data)
	f.Fuzz(func(t *testing.T, index, data []byte) {
		atlas, err := Decode(index, data)
		if err != nil {
			require.Nil(t, atlas)
			return
		}
		require.True(t, storageValid(atlas.Pixels))
		for _, entry := range atlas.Entries {
			require.True(t, entryValid(entry, atlas.Pixels.Rect.Max.X, atlas.Pixels.Rect.Max.Y))
		}
	})
}

func FuzzPrepare(f *testing.F) {
	f.Add(0, 0, 1, 1, 1.0, 1.0, 4)
	f.Add(math.MaxInt, 0, math.MaxInt, 1, math.NaN(), 0.5, math.MaxInt)
	f.Fuzz(func(t *testing.T, x, y, width, height int, ratio, opacity float64, stride int) {
		atlas := &image.NRGBA{Pix: make([]byte, 16), Stride: stride, Rect: image.Rect(0, 0, 2, 2)}
		got, ok := Prepare(atlas, Entry{X: x, Y: y, Width: width, Height: height, PixelRatio: ratio}, style.Color{Alpha: 255}, opacity)
		if !ok {
			require.Equal(t, Image{}, got)
			return
		}
		require.Len(t, got.Pixels, width*height*4)
		require.LessOrEqual(t, len(got.Pixels), 16)
	})
}

func BenchmarkPinnedSpriteDecode(b *testing.B) {
	index, err := os.ReadFile("../liberty/liberty_sprite.json")
	require.NoError(b, err)
	data, err := os.ReadFile("../liberty/liberty_sprite.png")
	require.NoError(b, err)
	for _, mode := range []string{"legacy", "bounded"} {
		b.Run(mode, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if mode == "bounded" {
					_, err := Decode(index, data)
					require.NoError(b, err)
					continue
				}
				var entries map[string]Entry
				require.NoError(b, json.Unmarshal(index, &entries))
				decoded, err := png.Decode(bytes.NewReader(data))
				require.NoError(b, err)
				bounds := decoded.Bounds()
				pixels := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
				draw.Draw(pixels, pixels.Bounds(), decoded, bounds.Min, draw.Src)
			}
		})
	}
}
