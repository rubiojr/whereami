package tileio

import (
	"fmt"
	"path/filepath"

	"github.com/rubiojr/whereami/pkg/vecmap/view"
)

const (
	OpenFreeMapSnapshot = "20260823_080002_pt"
	OpenFreeMapBaseURL  = "https://tiles.openfreemap.org/planet/" + OpenFreeMapSnapshot
	OpenFreeMapTemplate = OpenFreeMapBaseURL + "/{z}/{x}/{y}.pbf"
	PinnedCacheName     = "openfreemap-20260823-z9-250-193.pbf"
	PinnedSHA256        = "5007c887f3c99a2c0737b9a3afdf3813ef3e1ce939a63aa09a8407b7c3770c79"
)

type Source struct{ URL, CacheName, SHA256 string }

func OpenFreeMap(tile view.TileID) Source {
	s := Source{URL: fmt.Sprintf("%s/%d/%d/%d.pbf", OpenFreeMapBaseURL, tile.Z, tile.X, tile.Y),
		CacheName: filepath.Join("openfreemap-"+OpenFreeMapSnapshot, fmt.Sprint(tile.Z), fmt.Sprint(tile.X), fmt.Sprintf("%d.pbf", tile.Y))}
	if tile == (view.TileID{Z: 9, X: 250, Y: 193}) {
		s.CacheName, s.SHA256 = PinnedCacheName, PinnedSHA256
	}
	return s
}
