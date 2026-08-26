# Where Am I

A lightweight desktop waypoint and GPX viewer for exploring and managing location data.

![](/docs/screenshots/whereami.png)

## Features

- **Interactive map**: Pan by dragging, zoom with the mouse wheel, and use touch pinch/rotation gestures
- **Bookmarks**: Add, rename, tag, and delete saved locations
- **GPX import**: Recursively import GPX files from a selected directory
- **Search**: Search local waypoints and remote Nominatim geocoding results
- **Filters**: Show bookmarks only, filter by tag, or select an inclusive UTC date range
- **Themes**: Six runtime-selectable color themes
- **Keyboard shortcuts**: Open the in-app shortcut list with `Ctrl+?` or `Ctrl+/`

> [!WARNING]
> WhereAmI is beta-quality software. It has been tested in a Fedora Workstation environment only.

## Installation

### From a Release

Download a Linux release artifact from the [releases page](https://github.com/rubiojr/whereami/releases).

### From Source

Requirements: Go 1.24 or newer, Qt 6.5 or newer, GCC, and `miqt-rcc`.

```bash
git clone https://github.com/rubiojr/whereami.git
cd whereami
make build
bin/whereami
```

See [BUILD.md](BUILD.md) for detailed build instructions.

## Use Case

WhereAmI can display location history exported as GPX. One example is combining it with [hass2geo](https://github.com/rubiojr/hass2geo) and the [Home Assistant companion app](https://companion.home-assistant.io/) to keep location history in a private Home Assistant instance and import the resulting GPX directory.

## Usage

- **Add a bookmark**: Right-click the map. To reuse a selected or searched location, press `Ctrl+Enter` or `Ctrl+Return`.
- **Import GPX**: Select a directory with the folder button in the toolbar. Import scans that directory recursively.
- **Search**: Press `Ctrl+F` and use the search box.
- **Filter by date**: Use the calendar button in the toolbar to choose a day, range, or UTC date preset. Click the highlighted button again to clear the filter.
- **Navigate**: Drag to pan, use the mouse wheel to zoom, or use touch pinch gestures.
- **Themes**: Start the application with `--theme=<variant>`, where the variant is `orange`, `green`, `purple`, `adwaita-dark`, `nord-polar`, or `nord-frost`. The default is `nord-polar`.
- **All shortcuts**: Press `Ctrl+?` or `Ctrl+/`.

## Command-Line Options

```text
-cache-dir string
      custom cache directory (overrides XDG_CACHE_HOME)
-config-dir string
      custom config directory (overrides XDG_CONFIG_HOME)
-data-dir string
      custom data directory (overrides XDG_DATA_HOME)
-debug
      enable debug logging (verbose tile proxy requests)
-theme string
      theme variant (orange|green|purple|adwaita-dark|nord-polar|nord-frost)
```

## Storage

On Linux, the defaults are:

- Data: `${XDG_DATA_HOME:-$HOME/.local/share}/whereami/`
- Cache: `${XDG_CACHE_HOME:-$HOME/.cache}/whereami/`
- Configuration: `${XDG_CONFIG_HOME:-$HOME/.config}/whereami/`

The data directory contains bookmarks, imported GPX files, tags, and search history. The cache directory contains map tiles and the geocoding cache. The three command-line directory options override these locations.

## License

MIT License. See [LICENSE](LICENSE).

## Contributing

See the project guidance in [.rules](.rules).
