# geometry

Toolkit-neutral polygon preparation extracted from vecmap. It reuses the existing
Go Earcut implementation and the compiler's ring cleanup, triangle ordering,
point limits, and conservative operation budget. It builds without Qt or cgo.

```go
budget := geometry.Budget{Remaining: 50_000_000}
mesh, err := geometry.TriangulatePolygon(exterior, holes, &budget)
// mesh.Vertices contains float64 tile-local points.
// mesh.Indices contains uint32 triangle indices, in Earcut order.
```

Input coordinates must be finite tile-local values, as validated by the MVT
decoder. Input rings are neither modified nor retained. Results own their slices;
treat them as immutable after publication. Share a budget across polygons in one
preparation job, including unsuccessful calls: spent work remains charged.

`TriangulatePolygonExpanded` serves existing triangle-list consumers. It expands
Earcut's native indices directly, avoiding an intermediate uint32 buffer. The
parent vecmap package uses this compatibility path and aliases `roadPoint` to
`geometry.Point`, so no point-slice conversion is required.

This is the first part of direct indexed compilation, tracked by kata **tgde**.
The decoder's feature storage, material batching, line construction, and glyph
construction still need index propagation. The fixture compiler still produces
expanded meshes; `-indexed` still invokes the optional `scene.IndexMesh` pass.

```sh
CGO_ENABLED=0 go test -cover ./pkg/vecmap/geometry
CGO_ENABLED=0 GOAMD64=v4 go test ./pkg/vecmap/geometry \
  -run '^$' -bench BenchmarkPolygonPreparation -benchmem -count 3
```

The benchmark includes polygon triangulation and float32 scene packing in all
three modes, plus `IndexMesh` in the post-indexed mode. It does not measure the
full tile compiler or rendering. See `docs/vecmap-rhi.md` for measured results.
