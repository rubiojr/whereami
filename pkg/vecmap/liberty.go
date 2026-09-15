package vecmap

import (
	"github.com/rubiojr/whereami/pkg/vecmap/liberty"
	"github.com/rubiojr/whereami/pkg/vecmap/style"
)

type compiledLibertyLayer = style.CompiledLayer

func compiledLibertyLayers() ([]compiledLibertyLayer, error) {
	return liberty.Layers()
}
