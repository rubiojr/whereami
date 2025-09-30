# Building whereami

This document describes how to build the whereami application from source.

## Prerequisites

- Go 1.21 or higher
- Qt 6.5 or higher
- miqt bindings for Qt6
- miqt-rcc tool for resource compilation

## Installing miqt-rcc

The `miqt-rcc` tool is required to compile QML resources into the binary:

```bash
go install github.com/mappu/miqt/cmd/miqt-rcc@latest
```

## Building

### 1. Generate Resources

The QML files are compiled into Qt resources using miqt-rcc. This step is automated with go:generate:

```bash
go generate
```

This will create:
- `resources_gen.go` - Go code that registers the resources
- `resources_gen.rcc` - Compiled Qt resource data

These generated files are gitignored and must be regenerated when QML files change.

### 2. Build the Application

```bash
go build -ldflags '-s -w'
```

This creates the `whereami` executable with stripped debug symbols for a smaller binary.

## Development Workflow

### Adding New QML Files

1. Add the QML file to the appropriate directory:
   - `ui/` for main views
   - `ui/components/` for reusable components  
   - `ui/services/` for service singletons

2. Update `ui/resources.qrc` to include the new file:
   ```xml
   <file>components/NewComponent.qml</file>
   ```

3. Regenerate resources:
   ```bash
   go generate
   ```

4. Rebuild:
   ```bash
   go build -ldflags '-s -w'
   ```

### QML Development Tips

- Run `qmllint-qt6` to validate QML files:
  ```bash
  qmllint-qt6 ui/**/*.qml
  ```

- QML files are loaded from `qrc:/` URLs at runtime
- Relative imports in QML (e.g., `import "components"`) work within the resource system
- The main entry point is `qrc:/MapView.qml`

## Troubleshooting

### Resource Not Found

If you get errors about missing QML files at runtime:
1. Check that the file is listed in `ui/resources.qrc`
2. Ensure you've run `go generate` after modifying resources.qrc
3. Verify the path in resources.qrc is relative to the ui/ directory

### Build Errors

If the build fails:
1. Ensure miqt-rcc is installed and in your PATH
2. Check that Qt6 development files are installed
3. Verify that the miqt Qt6 bindings are properly installed

## Alternative Build with go tip

For testing with the latest Go development version:

```bash
~/sdk/gotip/bin/go generate
~/sdk/gotip/bin/go build -ldflags '-s -w'
```
