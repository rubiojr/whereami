package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedBindingsReproduce(t *testing.T) {
	include := os.Getenv("QT_RHI_INCLUDE")
	if include == "" {
		t.Skip("set QT_RHI_INCLUDE to the matching QtGui private include directory")
	}
	out := t.TempDir()
	require.NoError(t, run(out, include))
	entries, err := os.ReadDir(out)
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	for _, entry := range entries {
		generated, err := os.ReadFile(filepath.Join(out, entry.Name()))
		require.NoError(t, err)
		checkedIn, err := os.ReadFile(filepath.Join("..", "..", "internal", "qtrhi", entry.Name()))
		require.NoError(t, err)
		assert.Equal(t, string(checkedIn), string(generated), entry.Name())
		if strings.HasSuffix(entry.Name(), ".cpp") {
			assert.Contains(t, string(generated), "Code generated")
		}
	}
}

func TestGeneratedCallbacksRetainOwnership(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "internal", "qtrhi", "gen_qsgrendernode.cpp"))
	require.NoError(t, err)
	text := string(source)
	assert.Contains(t, text, "qtrhi_native_destroyed(this)")
	assert.Contains(t, text, "qtrhi_callback_released(handle__render)")
	assert.Contains(t, text, "if (self_cast->handle__render != 0) return false;")
	goSource, err := os.ReadFile(filepath.Join("..", "..", "internal", "qtrhi", "gen_qrhi.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(goSource), "_goptr.GoGC()", "native value copies must be explicitly released on the owning thread")
}
