# SDF text shaders

The committed `.qsb` files target Qt 6.5's shader-pack format and include
Vulkan/SPIR-V, OpenGL/OpenGL ES, Direct3D/HLSL, and Metal/MSL variants.

Regenerate them with Qt Shader Tools 6.5 or newer:

```sh
qsb --qsbversion 65 --qt6 -b -o sdftext.vert.qsb sdftext.vert
qsb --qsbversion 65 --qt6 -o sdftext.frag.qsb sdftext.frag
```

The vertex shader is baked with Qt Quick's batchable variant. The fragment
shader implements the MapLibre SDF fill and halo thresholds, including numeric
`text-halo-width` and `text-halo-blur` values in logical pixels.
See `LICENSE` for the MapLibre and Mapbox BSD 3-Clause notices retained for the
derived threshold and halo equations.
