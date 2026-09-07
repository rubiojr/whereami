package quick

import (
	"testing"

	qt "github.com/mappu/miqt/qt6"
	"github.com/stretchr/testify/assert"
)

func TestTextFont(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		family string
		weight qt.QFont__Weight
		italic bool
	}{
		{name: "plain", input: "Noto Sans", family: "Noto Sans", weight: qt.QFont__Normal},
		{name: "bold", input: "Noto Sans Bold", family: "Noto Sans", weight: qt.QFont__Bold},
		{name: "medium", input: "Noto Sans Medium", family: "Noto Sans", weight: qt.QFont__Medium},
		{name: "italic", input: "Noto Sans Italic", family: "Noto Sans", weight: qt.QFont__Normal, italic: true},
		{name: "regular", input: "Noto Sans Regular", family: "Noto Sans", weight: qt.QFont__Normal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			family, weight, italic := textFont(test.input)
			assert.Equal(t, test.family, family)
			assert.Equal(t, test.weight, weight)
			assert.Equal(t, test.italic, italic)
		})
	}
}

func TestTextFontFamiliesKeepsSystemFallback(t *testing.T) {
	assert.Equal(t, []string{"KlokanTech Noto Sans", "Noto Sans"}, textFontFamilies("KlokanTech Noto Sans"))
	assert.Equal(t, []string{"Open Sans"}, textFontFamilies("Open Sans"))
}
