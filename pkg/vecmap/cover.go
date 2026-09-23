package vecmap

import "github.com/rubiojr/whereami/pkg/vecmap/view"

// Alias the shared ID so cover publication does not allocate conversion slices.
type vectorTileID = view.TileID

func visibleTileCover(camera Camera) []vectorTileID { return view.VisibleTileCover(camera) }
