# Focused miqt Qt Quick bindings

This package is a generated, deliberately narrow Qt 6 Quick/QSG binding used by
the vector renderer prototype. It was generated from miqt v0.14.0 against the
public Qt 6.11 headers for:

- `QQuickItem`
- `QSGNode`, `QSGBasicGeometryNode`, `QSGGeometryNode`, and `QSGTransformNode`
- `QSGGeometry` and its `AttributeSet` and `Point2D` types
- `QSGMaterial` and `QSGFlatColorMaterial`

The focused generator allowlist retains only construction, retained geometry,
flat-color material, dirty-state, and `updatePaintNode` APIs. Clang 22 requires
nested Qt types such as `QQuickItem::UpdatePaintNodeData` and
`QSGNode::DirtyState` to be explicitly qualified before emission.

The repository ignores `/vendor`, so the generated package lives here rather
than under the miqt dependency. `cflags.go`, `callback_handle.go`, and
`point2d.*` and `transformnode.*` are small lifecycle, array-access, and public
scene-graph transform additions around the generated surface. They contain no
application rendering policy.
