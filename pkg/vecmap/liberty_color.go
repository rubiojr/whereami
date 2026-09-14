package vecmap

import "github.com/rubiojr/whereami/pkg/vecmap/style"

type mapColor = style.Color

func parseLibertyColor(value any) (mapColor, bool) { return style.ParseColor(value) }
