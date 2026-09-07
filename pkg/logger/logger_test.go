package logger

import (
	"bytes"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestErrorsWriteToStdout(t *testing.T) {
	assert.Equal(t, os.Stdout, errorLogger.Writer())
}

func TestErrorFormatsCopyableLine(t *testing.T) {
	var output bytes.Buffer
	errorLogger.SetOutput(&output)
	t.Cleanup(func() { errorLogger.SetOutput(os.Stdout) })

	Error("tile %d failed", 7)

	assert.Equal(t, "[ERROR] tile 7 failed\n", output.String())
}

func TestWarningFormatsCopyableStdoutLine(t *testing.T) {
	var output bytes.Buffer
	warnLogger.SetOutput(&output)
	t.Cleanup(func() { warnLogger.SetOutput(os.Stdout) })

	Warn("tile %d degraded", 7)

	assert.Equal(t, "[WARN] tile 7 degraded\n", output.String())
}
