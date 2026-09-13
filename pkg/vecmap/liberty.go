package vecmap

import (
	_ "embed"
	"fmt"
	"sync"

	"github.com/rubiojr/whereami/pkg/vecmap/style"
)

//go:generate go run ./cmd/libertystylegen

//go:embed liberty_style.json
var libertyStyleJSON []byte

const libertyStyleSHA256 = "6010998863b4876911ac9a2d62c9a28d97c8877f6d20cd158b74808572257b60"

type libertyStyleDocument = style.Document
type compiledLibertyLayer = style.CompiledLayer

var (
	libertyOnce       sync.Once
	libertyLayers     []compiledLibertyLayer
	libertyStyleError error
)

func compiledLibertyLayers() ([]compiledLibertyLayer, error) {
	libertyOnce.Do(func() {
		libertyLayers, libertyStyleError = style.Parse(libertyStyleJSON)
		if libertyStyleError != nil {
			libertyStyleError = fmt.Errorf("prepare embedded Liberty style: %w", libertyStyleError)
		}
	})
	return libertyLayers, libertyStyleError
}
