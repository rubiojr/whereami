package vecmap

import (
	"log"
	"os"
)

var outputLogger = log.New(os.Stdout, "", 0)

var (
	reportVectorError = func(format string, args ...any) {
		outputLogger.Printf("[ERROR] "+format, args...)
	}
	reportVectorWarning = func(format string, args ...any) {
		outputLogger.Printf("[WARN] "+format, args...)
	}
)
