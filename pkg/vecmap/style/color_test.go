package style

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseColors(t *testing.T) {
	for _, tt := range []struct {
		text string
		want Color
	}{
		{"#f8f4f0", Color{Red: 248, Green: 244, Blue: 240, Alpha: 255}},
		{"rgba(95, 208, 100, 0.5)", Color{Red: 95, Green: 208, Blue: 100, Alpha: 128}},
		{"hsla(0, 0%, 100%, 0.25)", Color{Red: 255, Green: 255, Blue: 255, Alpha: 64}},
		{"#abc", Color{Red: 170, Green: 187, Blue: 204, Alpha: 255}},
		{"#abcd", Color{Red: 170, Green: 187, Blue: 204, Alpha: 221}},
		{"#01020304", Color{Red: 1, Green: 2, Blue: 3, Alpha: 4}},
		{" transparent ", Color{}},
		{"BLACK", Color{Alpha: 255}},
		{"white", Color{Red: 255, Green: 255, Blue: 255, Alpha: 255}},
		{"rgb(1,2,3)", Color{Red: 1, Green: 2, Blue: 3, Alpha: 255}},
		{"hsl(0,100%,50%)", Color{Red: 255, Alpha: 255}},
		{"hsl(120,100%,25%)", Color{Green: 128, Alpha: 255}},
		{"hsl(240,100%,50%)", Color{Blue: 255, Alpha: 255}},
		{"hsl(300,100%,50%)", Color{Red: 255, Blue: 255, Alpha: 255}},
	} {
		got, ok := ParseColor(tt.text)
		require.True(t, ok, tt.text)
		assert.Equal(t, tt.want, got, tt.text)
		got, ok = ParseColor(got)
		require.True(t, ok)
		assert.Equal(t, tt.want, got)
	}
	for _, value := range []any{nil, true, "red", "#12", "#gggggg", "rgb(1,2)", "rgb(a,2,3)", "hsl(1,2)", "hsl(a,2,3)", "rgb(1,2,3"} {
		_, ok := ParseColor(value)
		assert.False(t, ok, "%v", value)
	}
	_, ok := libertyColorArguments("invalid")
	assert.False(t, ok)
}

func TestColorOpacity(t *testing.T) {
	color := Color{Red: 10, Green: 20, Blue: 30, Alpha: 255}
	for _, tt := range []struct {
		opacity float64
		alpha   int
	}{{-1, 0}, {0, 0}, {.5, 128}, {1, 255}, {2, 255}} {
		got := ColorWithOpacity(color, tt.opacity)
		assert.Equal(t, Color{Red: 10, Green: 20, Blue: 30, Alpha: tt.alpha}, got)
	}
	assert.Equal(t, 255, color.Alpha)
}
