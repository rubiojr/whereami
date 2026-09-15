package glyph

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLegacyFallbackEligible(t *testing.T) {
	for _, text := range []string{"", "\xff", "ދިވެހި", strings.Repeat("a", MaxTextRunes+1)} {
		assert.False(t, LegacyFallbackEligible(text), "%q", text)
	}
	for _, text := range []string{"Madrid", "مرحبا", "a\u0301", strings.Repeat("界", MaxTextRunes)} {
		assert.True(t, LegacyFallbackEligible(text), "%q", text)
	}
}
