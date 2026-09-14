package glyph

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTextEligibleRetainsScriptSubset(t *testing.T) {
	for _, text := range []string{"Madrid", "Αθήνα", "Москва", "Երևան", "თბილისი", "東京", "ひらがな", "カタカナ", "서울", "ㄅㄆ", "123 € ❤", " \r\n\t", "\ufffd", strings.Repeat("界", MaxTextRunes)} {
		assert.True(t, TextEligible(text), text)
	}
	for _, text := range []string{"", "مرحبا", "שלום", "ไทย", "नमस्ते", "e\u0301", "\U0001f600", "\xff", strings.Repeat("A", MaxTextRunes+1), strings.Repeat("A", MaxTextBytes+1)} {
		assert.False(t, TextEligible(text), text)
	}
}
