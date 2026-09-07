# vecmap example

Minimal Qt Quick application using `pkg/vecmap` directly.

```bash
make -C examples/map build
./examples/map/map
```

Drag to pan and use the mouse wheel to zoom around the pointer. The example
needs Qt 6 QML/Quick development libraries, `pkg-config`, a C++ compiler, and
`miqt-rcc` v0.14.0.

For a non-interactive smoke run:

```bash
go run ./examples/map --duration=8s
```
