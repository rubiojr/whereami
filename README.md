# Where Am I 

A lightweight desktop waypoint & GPX viewer for exploring and managing your location data.

![](/docs/screenshots/whereami.png)

## Features

- **Interactive Map**: Browse maps with zoom and pan controls
- **Waypoint Management**: Add, edit, and organize waypoints with custom tags
- **GPX Import**: Import waypoints from GPX files and directories
- **Bookmarks**: Save and manage your favorite locations
- **Search**: Find locations using geocoding search
- **Themes**: Multiple UI themes including dark mode
- **Keyboard Shortcuts**: Efficient navigation with hotkeys

> [!WARNING]
> WhereAmI is Beta quality software. Tested in a Fedora Workstation environment only.

## Installation

### From Release

Download the latest release for your platform from the [releases page](https://github.com/rubiojr/whereami/releases).

### From Source

Requirements: Go 1.24+, Qt 6.5+

```bash
git clone https://github.com/rubiojr/whereami.git
make
bin/whereami
```

See [BUILD.md](BUILD.md) for detailed build instructions.

## Usage

- **Add Waypoints**: Right-click on the map or use Ctrl+N
- **Import GPX**: Drag and drop GPX files or use Ctrl+O
- **Search**: Use the search box or press Ctrl+F
- **Navigate**: Use arrow keys or mouse to explore the map
- **Themes**: Switch themes with F1-F6 keys

## Data Storage

- **Linux**: `~/.local/share/whereami/`
- **macOS**: `~/Library/Application Support/whereami/`
- **Windows**: `%APPDATA%/whereami/`

## License

MIT License - see [LICENSE](LICENSE) file for details.

## Contributing

Contributions welcome! Please read the [project rules](.rules) for development guidelines.
