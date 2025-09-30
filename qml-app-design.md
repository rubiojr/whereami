# QML Application Design Guide

This document explains the architecture and design patterns used in the `whereami` application, providing a blueprint for creating robust Go + QML applications using MIQT (Modern Qt bindings for Go).

## Table of Contents

1. [Overview](#overview)
2. [Project Structure](#project-structure)
3. [Resource Management](#resource-management)
4. [Go Backend Architecture](#go-backend-architecture)
5. [QML Frontend Architecture](#qml-frontend-architecture)
6. [Communication Patterns](#communication-patterns)
7. [Theme and Styling](#theme-and-styling)
8. [JavaScript Libraries](#javascript-libraries)
9. [Development Workflow](#development-workflow)
10. [Best Practices](#best-practices)
11. [Testing and Debugging](#testing-and-debugging)
12. [Font Handling & Modular Typography](#font-handling--modular-typography)

## Font Handling & Modular Typography

Consistent typography is implemented through a centralized singleton: `Fonts.qml` (compiled into resources and imported via `import "qrc:/themes" as Themes`, then accessed as `Themes.Fonts`). This replaces ad‑hoc font size constants scattered across themes and components.

### Core Concepts

- Base size & ratio:
  - `Fonts.minFontSize` (default base size, e.g. 12px)
  - `Fonts.fontScaleRatio` (geometric progression ratio, e.g. 1.15)
- Scale function:
  - `Fonts.scale(step, overrideMin, overrideRatio)`
  - Definition: `scale(1) = base`, `scale(n) = base * ratio^(n-1)` (rounded to integer pixels)
- Semantic helpers (optional): `Fonts.small()`, `Fonts.body()`, `Fonts.heading()`, etc.

### How Themes Use It

Each `*Theme.qml` declares (or can override):
```
property int  minFontSize: 12
property real fontScaleRatio: 1.15   // or a custom ratio (e.g. 1.20 for larger progression)
```
Theme files do NOT duplicate the scaling math. Instead, size bindings reference:
```
Themes.Fonts.scale(3, minFontSize, fontScaleRatio)
```
The active theme is loaded by `ThemeLoader.qml`, which forwards a unified API:
```
theme.scale(n)  // Internally: Themes.Fonts.scale(n, theme.minFontSize, theme.fontScaleRatio)
```
Components should prefer `theme.scale(n)` so that:
1. Theme overrides are respected.
2. Future user preferences (e.g. accessibility sizing) can be injected centrally.

### Adding / Modifying Theme Typography

1. Open or create a theme file (e.g. `ui/themes/MyTheme.qml`).
2. Set (or omit to inherit defaults):
   ```
   property int minFontSize: 13
   property real fontScaleRatio: 1.18
   ```
3. Use consistent bindings for all size choices:
   - Small metadata: `theme.scale(1)`
   - Body text / common labels: `theme.scale(2)`
   - Section titles / card headers: `theme.scale(3)`
   - Prominent headers: `theme.scale(4)`
4. Avoid hard-coded numeric pixel sizes unless:
   - A value must remain visually invariant (e.g. dense icon labels).
   - It is a container / layout metric, not text.

### When to Override Ratio vs. Base

| Goal | Change | Example |
|------|--------|---------|
| Slight global enlargement | Increase `minFontSize` | 12 → 13 |
| More dramatic step growth | Increase `fontScaleRatio` | 1.15 → 1.20 |
| Accessibility / large type | Both | 13 + 1.20 |
| Finer granularity | Decrease ratio | 1.15 → 1.10 |

(Keep ratio between ~1.10 and ~1.25 to avoid cramped or jumpy vertical rhythm.)

### Fallback Logic

If a theme omits `minFontSize` or `fontScaleRatio`, the forwarding layer passes `undefined` and `Fonts.scale()` falls back to its internal defaults. For reliability (and to silence lint warnings), all shipped themes explicitly declare both properties.

### Validation

A helper script ensures property surface consistency across themes:
```
./scripts/check_theme_knobs.sh --report-extras
```
While it currently focuses on color & structural knobs, it also verifies that typography knobs (`minFontSize`, `fontScaleRatio`, and dependent size properties like `statusBarTextSize`) are present.

### Guidelines & Pitfalls

- Always round: integer pixel sizes avoid blurry text on some platforms.
- Don’t mutate font sizes dynamically in many places (layout churn). Bind once to `theme.scale(n)`.
- Keep maximum scale steps reasonable; the singleton clamps runaway step values.
- Avoid mixing raw pixel sizes with scale-based ones in the same component unless intentional (document exceptions inline).

### Example

```
Text {
    text: model.label
    font.pixelSize: theme.scale(2)        // body
}

Label {
    text: waypoint.name
    font.pixelSize: theme.scale(3)        // header
}
```

### Future Extension Ideas

- User preference injection (persisted in settings, applied by `ThemeLoader`).
- Alternate scale curves (e.g. major/minor third blends) by swapping the implementation inside `Fonts.qml`.
- Dynamic accessibility mode that adjusts both base and ratio.

---

## Overview

The `whereami` application demonstrates a clean separation between Go backend services and QML frontend UI, connected via HTTP APIs. This architecture provides:

- **Clear separation of concerns**: Business logic in Go, UI logic in QML
- **Testable components**: HTTP APIs can be tested independently
- **Cross-platform compatibility**: Qt handles platform differences
- **Hot reloading during development**: QML changes don't require Go recompilation
- **Resource embedding**: All QML files are compiled into the binary

## Project Structure

```
your-app/
├── main.go                     # Application entry point
├── generate.go                 # Resource generation directive
├── resources_gen.go            # Generated resource file (gitignored)
├── resources_gen.rcc           # Generated resource file (gitignored)
├── api.go                      # HTTP API handlers
├── storage.go                  # Data persistence layer
├── go.mod                      # Go module definition
├── ui/                         # QML frontend
│   ├── resources.qrc           # Qt resource file
│   ├── MainView.qml            # Root QML component
│   ├── components/             # Reusable UI components
│   │   ├── Theme.qml           # Centralized styling
│   │   ├── SomeDialog.qml      # Modal dialogs
│   │   ├── SomeCard.qml        # Information cards
│   │   └── ...
│   ├── services/               # QML service objects
│   │   └── API.qml             # HTTP API wrapper
│   └── lib/                    # JavaScript libraries
│       └── SomeLibrary.js      # Utility functions
└── .rules                      # Development guidelines
```

## Resource Management

### 1. Resource Definition (`ui/resources.qrc`)

All QML files must be listed in the Qt resource file:

```xml
<RCC>
  <qresource prefix="/">
    <file>MainView.qml</file>
    <file>components/Theme.qml</file>
    <file>components/SomeDialog.qml</file>
    <file>services/API.qml</file>
    <file>lib/SomeLibrary.js</file>
  </qresource>
</RCC>
```

### 2. Resource Generation (`generate.go`)

Use `go:generate` to compile QML into Go resources:

```go
package main

//go:generate miqt-rcc -Qt6 -Input ui/resources.qrc -OutputGo resources_gen.go -OutputRcc resources_gen.rcc -Package main
```

### 3. Resource Loading (`main.go`)

Load the main QML file from embedded resources:

```go
func main() {
    qt.NewQApplication(os.Args)
    engine := qml.NewQQmlApplicationEngine()
    
    // Load from embedded resources (qrc:/)
    engine.Load(qt.NewQUrl3("qrc:/MainView.qml"))
    
    if len(engine.RootObjects()) == 0 {
        fmt.Fprintln(os.Stderr, "QML load failed")
        os.Exit(2)
    }
    
    qt.QApplication_Exec()
}
```

## Go Backend Architecture

### 1. Main Application Structure

```go
package main

import (
    "net/http"
    "os"
    qt "github.com/mappu/miqt/qt6"
    "github.com/mappu/miqt/qt6/qml"
)

func main() {
    // 1. Setup data directories (XDG compliant)
    dataDir := setupDataDirectory()
    
    // 2. Register HTTP API handlers
    RegisterAPI(http.DefaultServeMux, dataDir)
    
    // 3. Start HTTP server in goroutine
    go func() {
        addr := "127.0.0.1:8080"
        http.ListenAndServe(addr, nil)
    }()
    
    // 4. Initialize Qt application
    qt.NewQApplication(os.Args)
    engine := qml.NewQQmlApplicationEngine()
    engine.Load(qt.NewQUrl3("qrc:/MainView.qml"))
    
    qt.QApplication_Exec()
}
```

### 2. API Layer Pattern

Create a dedicated file for HTTP handlers:

```go
// api.go
package main

func RegisterAPI(mux *http.ServeMux, dataDir string) {
    // Use Go 1.22 method-aware routing patterns
    mux.HandleFunc("GET /api/items", handleGetItems)
    mux.HandleFunc("POST /api/items", handleCreateItem)
    mux.HandleFunc("GET /api/items/{id}", handleGetItem)
    mux.HandleFunc("PUT /api/items/{id}", handleUpdateItem)
    mux.HandleFunc("DELETE /api/items/{id}", handleDeleteItem)
    
    // CORS preflight
    mux.HandleFunc("OPTIONS /api/items", handleItemsOptions)
    mux.HandleFunc("OPTIONS /api/items/{id}", handleItemsOptions)
}

func handleGetItems(w http.ResponseWriter, r *http.Request) {
    // List items - method is already guaranteed to be GET
    items := getItems()
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(items)
}

func handleCreateItem(w http.ResponseWriter, r *http.Request) {
    // Create item - method is already guaranteed to be POST
    var item Item
    if err := json.NewDecoder(r.Body).Decode(&item); err != nil {
        http.Error(w, "Invalid JSON", http.StatusBadRequest)
        return
    }
    
    savedItem, err := createItem(item)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusCreated)
    json.NewEncoder(w).Encode(savedItem)
}

func handleGetItem(w http.ResponseWriter, r *http.Request) {
    // Get path parameter using Go 1.22 pattern
    id := r.PathValue("id")
    item, err := getItemByID(id)
    if err != nil {
        http.Error(w, "Item not found", http.StatusNotFound)
        return
    }
    
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(item)
}

func handleItemsOptions(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Access-Control-Allow-Origin", "*")
    w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
    w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
    w.WriteHeader(http.StatusNoContent)
}
```

### 3. Data Layer Pattern

Separate data persistence concerns:

```go
// storage.go
package main

type Item struct {
    ID   int    `json:"id"`
    Name string `json:"name"`
}

func LoadItems(dataDir string) ([]Item, error) {
    // Load from file/database
}

func SaveItem(dataDir string, item Item) error {
    // Save to file/database
}
```

### 4. Concurrency Safety

Use mutexes for shared data:

```go
var (
    items   []Item
    itemsMu sync.RWMutex
)

func GetItems() []Item {
    itemsMu.RLock()
    defer itemsMu.RUnlock()
    return items
}

func AddItem(item Item) {
    itemsMu.Lock()
    defer itemsMu.Unlock()
    items = append(items, item)
}
```

## QML Frontend Architecture

### 1. Root Component Pattern

Create a main ApplicationWindow as the root:

```qml
// MainView.qml
import QtQuick 2.15
import QtQuick.Controls 2.15
import "components"
import "services"

ApplicationWindow {
    id: window
    visible: true
    width: 1200
    height: 800
    
    // Global theme
    Theme {
        id: theme
    }
    
    // API service
    API {
        id: api
        apiPort: 8080
        
        // Handle API responses
        onItemsLoaded: function(items) {
            // Update UI state
        }
    }
    
    // Main content
    SomeMainComponent {
        anchors.fill: parent
        api: api
        theme: theme
    }
}
```

### 2. Component Organization

#### Reusable Components (`components/`)

```qml
// components/SomeCard.qml
import QtQuick 2.15
import QtQuick.Controls 2.15

Rectangle {
    id: root
    
    // Public API
    property var item: null
    property var api: null
    
    // Theme integration
    Theme {
        id: theme
    }
    
    color: theme.cardBackground
    border.color: theme.cardBorder
    
    // Internal implementation
    Column {
        // Card content
    }
}
```

#### Service Objects (`services/`)

The API service should be a comprehensive wrapper around **ALL** Go backend endpoints. No QML component should make direct XMLHttpRequest calls - everything must go through the API service.

```qml
// services/API.qml
import QtQuick 2.15

QtObject {
    id: api
    
    property int apiPort: 8080
    property int requestTimeoutMs: 8000
    
    // Generic signals for all operations
    signal requestSucceeded(string kind, var result, var context)
    signal requestFailed(string kind, string errorMessage, var context)
    
    // Domain-specific signals - replace with your application's entities
    signal dataLoaded(var data)
    signal dataLoadFailed(string error)
    signal entityCreated(var entity, var originalEntity)
    signal entityCreateFailed(var entity, string error)
    signal entityDeleted(var entity)
    signal entityDeleteFailed(var entity, string error)
    signal entityUpdated(var entity, var originalEntity)
    signal entityUpdateFailed(var entity, string error)
    
    // PUBLIC METHODS - One for each backend endpoint
    
    function getData() {
        _xhr("GET", "/api/your-endpoint", null, function(txt) {
            var data = JSON.parse(txt || "[]");
            dataLoaded(data);
            requestSucceeded("GET /api/your-endpoint", data, null);
        }, function(err) {
            dataLoadFailed(err);
            requestFailed("GET /api/your-endpoint", err, null);
        });
    }
    
    function createEntity(entity) {
        if (!entity) {
            console.error("API.createEntity: invalid entity");
            return;
        }
        
        _xhr("POST", "/api/your-endpoint", entity, function(txt) {
            var saved = JSON.parse(txt || "{}");
            entityCreated(saved, entity);
            requestSucceeded("POST /api/your-endpoint", saved, entity);
        }, function(err) {
            entityCreateFailed(entity, err);
            requestFailed("POST /api/your-endpoint", err, entity);
        });
    }
    
    function deleteEntity(entity) {
        var path = "/api/your-endpoint/" + entity.id;
        _xhr("DELETE", path, null, function() {
            entityDeleted(entity);
            requestSucceeded("DELETE " + path, entity, entity);
        }, function(err) {
            entityDeleteFailed(entity, err);
            requestFailed("DELETE " + path, err, entity);
        });
    }
    
    function updateEntity(entity, updateData) {
        var path = "/api/your-endpoint/" + entity.id;
        _xhr("PATCH", path, updateData, function(txt) {
            var updated = JSON.parse(txt || "{}");
            entityUpdated(updated, entity);
            requestSucceeded("PATCH " + path, updated, entity);
        }, function(err) {
            entityUpdateFailed(entity, err);
            requestFailed("PATCH " + path, err, entity);
        });
    }
    
    // Add more functions for each of your backend endpoints
    // Follow the same pattern: validate input, make _xhr call, emit signals
    
    // PRIVATE HELPER FUNCTIONS
    function _xhr(method, path, body, success, failure) {
        var xhr = new XMLHttpRequest();
        xhr.open(method, "http://127.0.0.1:" + apiPort + path);
        if (method === "POST" || method === "PUT" || method === "PATCH") {
            xhr.setRequestHeader("Content-Type", "application/json");
        }
        xhr.onreadystatechange = function() {
            if (xhr.readyState === XMLHttpRequest.DONE) {
                if (xhr.status >= 200 && xhr.status < 300) {
                    if (success) success(xhr.responseText);
                } else {
                    if (failure) failure("HTTP " + xhr.status);
                }
            }
        };
        xhr.send(body ? JSON.stringify(body) : null);
    }
}
```

### 3. State Management

Use properties and signals for state management:

```qml
ApplicationWindow {
    // Application state
    property var items: []
    property var selectedItem: null
    property bool loading: false
    
    // State changes trigger UI updates automatically
    onItemsChanged: {
        console.log("Items updated:", items.length);
    }
}
```

## Communication Patterns

### 1. API Service Wrapper Pattern

**Critical Architecture Rule**: The API.qml service should be a comprehensive, self-contained wrapper around ALL Go backend endpoints. Every QML component must use this service exclusively—never make direct XMLHttpRequest calls.

#### Complete API Service Structure

```qml
// services/API.qml
import QtQuick 2.15

QtObject {
    id: api
    
    property int apiPort: 8080
    property int requestTimeoutMs: 8000
    
    // ========== SIGNALS ==========
    // Generic signals
    signal requestSucceeded(string kind, var result, var context)
    signal requestFailed(string kind, string errorMessage, var context)
    
    // Entity-specific signals (replace with your domain entities)
    signal entitiesLoaded(var entities)
    signal entitiesLoadFailed(string error)
    signal entityAdded(var entity, var originalEntity)
    signal entityAddFailed(var originalEntity, string error)
    signal entityDeleted(var entity)
    signal entityDeleteFailed(var entity, string error)
    
    // ========== PUBLIC API METHODS ==========
    // Wrap EVERY backend endpoint
    
    function getEntities() {
        if (api.apiPort < 0) {
            entitiesLoaded([]);
            return;
        }
        _xhr("GET", "/api/entities", null, function(response) {
            var entities = JSON.parse(response);
            entitiesLoaded(entities);
            requestSucceeded("GET /api/entities", entities, null);
        }, function(error) {
            entitiesLoadFailed(error);
            requestFailed("GET /api/entities", error, null);
        });
    }
    
    function addEntity(entity) {
        if (!entity || !entity.name) {
            console.error("API.addEntity: invalid entity payload", entity);
            return;
        }
        _xhr("POST", "/api/entities", entity, function(response) {
            var savedEntity = JSON.parse(response);
            entityAdded(savedEntity, entity);
            requestSucceeded("POST /api/entities", savedEntity, entity);
        }, function(error) {
            entityAddFailed(entity, error);
            requestFailed("POST /api/entities", error, entity);
        });
    }
    
    function deleteEntity(entity) {
        var path = "/api/entities/" + entity.id;
        _xhr("DELETE", path, null, function() {
            entityDeleted(entity);
            requestSucceeded("DELETE " + path, entity, entity);
        }, function(error) {
            entityDeleteFailed(entity, error);
            requestFailed("DELETE " + path, error, entity);
        });
    }
    
    // Generic request method for future endpoints
    function request(method, path, body, successCallback, errorCallback, context) {
        _xhr(method, path, body, function(response) {
            if (successCallback) successCallback(response);
            requestSucceeded(method + " " + path, response, context);
        }, function(error) {
            if (errorCallback) errorCallback(error);
            requestFailed(method + " " + path, error, context);
        });
    }
    
    // ========== PRIVATE IMPLEMENTATION ==========
    function _xhr(method, path, body, success, failure, timeoutOverride, context) {
        var xhr = new XMLHttpRequest();
        var timedOut = false;
        var timeoutMs = timeoutOverride || api.requestTimeoutMs;
        
        var timer = _startTimeout(timeoutMs, function() {
            timedOut = true;
            try { xhr.abort(); } catch(e) {}
            if (failure) failure("timeout (" + timeoutMs + " ms)");
        });
        
        var url = _baseUrl() + path;
        xhr.open(method, url);
        
        if (method === "POST" || method === "PUT" || method === "PATCH") {
            xhr.setRequestHeader("Content-Type", "application/json");
        }
        
        xhr.onreadystatechange = function() {
            if (xhr.readyState === XMLHttpRequest.DONE) {
                _clearTimeout(timer);
                if (timedOut) return;
                
                if (xhr.status >= 200 && xhr.status < 300) {
                    if (success) success(xhr.responseText);
                } else {
                    var msg = "HTTP " + xhr.status;
                    if (xhr.responseText) {
                        msg += " " + xhr.responseText.substring(0, 160);
                    }
                    if (failure) failure(msg);
                }
            }
        };
        
        try {
            if (body) {
                xhr.send(JSON.stringify(body));
            } else {
                xhr.send();
            }
        } catch(e) {
            _clearTimeout(timer);
            if (failure) failure("send error: " + e);
        }
    }
    
    function _baseUrl() {
        return "http://127.0.0.1:" + api.apiPort;
    }
    
    // Timer management helpers
    function _startTimeout(ms, callback) {
        var timer = Qt.createQmlObject(
            'import QtQuick 2.15; Timer { interval: ' + ms + '; repeat: false }',
            api, "timeout_timer"
        );
        timer.timeout.connect(callback);
        timer.start();
        return timer;
    }
    
    function _clearTimeout(timer) {
        if (timer) {
            timer.stop();
            timer.destroy();
        }
    }
}
```

#### Component Usage Pattern

```qml
// components/EntityList.qml
Rectangle {
    property var api: null  // Injected from parent
    property var entities: []
    
    Component.onCompleted: {
        // Never make direct XMLHttpRequest - always use API service
        api.getEntities();
    }
    
    Connections {
        target: api
        function onEntitiesLoaded(loadedEntities) {
            entities = loadedEntities;
        }
        function onEntitiesLoadFailed(error) {
            console.error("Failed to load entities:", error);
        }
    }
}
```

### 2. Signal-Based Communication

Use Qt's signal system for loose coupling between components:

```qml
// Parent component
SomeDialog {
    id: dialog
    api: root.api  // Pass API service reference
    
    onEntityCreated: function(entity) {
        // Dialog handled the API call, just refresh UI
        console.log("Entity created:", entity.name);
    }
}

// Dialog component
Dialog {
    property var api: null
    signal entityCreated(var entity)
    
    onAccepted: {
        // Use API service, never direct XMLHttpRequest
        api.addEntity({
            name: nameField.text,
            description: descField.text
        });
    }
    
    Connections {
        target: api
        function onEntityAdded(savedEntity, originalEntity) {
            entityCreated(savedEntity);
            close();
        }
        function onEntityAddFailed(originalEntity, error) {
            errorLabel.text = "Failed to create entity: " + error;
        }
    }
}
```

### 3. API Service Benefits

This comprehensive API wrapper pattern provides:

- **Single source of truth**: All backend communication goes through one service
- **Consistent error handling**: Standardized error reporting across the application
- **Offline mode support**: Easy to implement offline fallbacks
- **Testability**: Mock the API service for testing UI components
- **Maintainability**: Backend API changes only require updates in one place
- **Signal consistency**: Uniform event-driven architecture throughout the app

## Theme and Styling

### 1. Theme System Architecture

The application uses a centralized theming system with multiple color variants that can be switched at compile time using the `ThemeLoader` component.

#### Available Themes

- **Orange Theme (Default)**: Modern energetic style with orange accents
- **Green Theme**: Nature-inspired with green palette
- **Purple Theme**: Elegant with purple tones

#### Theme Structure

All themes follow the same property structure to ensure consistency:

```qml
// components/Theme.qml (Orange - Default)
import QtQuick 2.15

QtObject {
    // Base colors
    property color background: "#222226"
    property color accent: "#F97700"
    property color primaryText: "#FF8226"
    property color secondaryText: "#BDBDBD"
    
    // Component-specific nested themes
    property QtObject waypointInfoCard: QtObject {
        property color background: Qt.rgba(0, 0, 0, 0.82)
        property color border: "#444"
        property color primaryText: "white"
        property color secondaryText: "white"
        
        property QtObject editButton: QtObject {
            property color text: Qt.darker("DeepOrange", 1.3)
            property color background: "White"
        }
    }
    
    property QtObject mapControlButton: QtObject {
        property color background: Qt.rgba(0, 0, 0, 0.6)
        property color backgroundHover: Qt.rgba(0, 0, 0, 0.75)
        property color border: "#444"
        property color text: "white"
        property color textHover: accent
        
        property QtObject tooltip: QtObject {
            property color background: Qt.rgba(0, 0, 0, 0.9)
            property color border: "#555"
            property color text: "white"
        }
    }
}
```

### 2. ThemeLoader Component

The `ThemeLoader` provides a centralized way to switch between themes:

```qml
// components/ThemeLoader.qml
import QtQuick 2.15

Loader {
    readonly property string THEME_VARIANT: "orange"  // Change this to switch themes
    
    source: {
        switch (THEME_VARIANT) {
        case "green": return "GreenTheme.qml"
        case "purple": return "PurpleTheme.qml"
        default: return "Theme.qml"  // Orange default
        }
    }
}
```

### 3. Theme Usage Pattern

Components should use `ThemeLoader` instead of importing themes directly:

```qml
// In components
Rectangle {
    ThemeLoader {
        id: theme
    }
    
    color: theme.waypointInfoCard.background
    border.color: theme.waypointInfoCard.border
    
    Text {
        color: theme.waypointInfoCard.primaryText
    }
    
    Button {
        contentItem: Text {
            color: theme.waypointInfoCard.editButton.text
        }
        background: Rectangle {
            color: theme.waypointInfoCard.editButton.background
        }
    }
}
```

### 4. Creating New Themes

To add a new theme variant:

1. **Copy an existing theme**:
   ```bash
   cp ui/components/Theme.qml ui/components/BlueTheme.qml
   ```

2. **Update color values** in the new theme file:
   ```qml
   // components/BlueTheme.qml
   import QtQuick 2.15
   
   QtObject {
       property color background: "#1A1A2E"
       property color accent: "#2196F3"
       property color primaryText: "#42A5F5"
       // ... update all color properties
   }
   ```

3. **Add to resources** in `ui/resources.qrc`:
   ```xml
   <file>components/BlueTheme.qml</file>
   ```

4. **Update ThemeLoader** to include the new option:
   ```qml
   source: {
       switch (THEME_VARIANT) {
       case "green": return "GreenTheme.qml"
       case "purple": return "PurpleTheme.qml"
       case "blue": return "BlueTheme.qml"
       default: return "Theme.qml"
       }
   }
   ```

5. **Regenerate resources**:
   ```bash
   go generate
   ```

### 5. Theme Switching

To switch themes, edit `ThemeLoader.qml`:

```qml
readonly property string THEME_VARIANT: "green"  // Changes entire app theme
```

Then regenerate resources and rebuild:
```bash
go generate
go build -ldflags '-s -w' -o bin/whereami .
```

### 6. Theme Property Groups

Each theme provides these main property groups:

- `waypointInfoCard`: Waypoint information display styling
- `searchBox`: Search interface colors and styling  
- `addWaypointDialog`: Dialog box appearance
- `mapControlButton`: Map control button styling including tooltips
- `waypointTable`: Table component styling
- `mapStatusBar`: Status bar colors
- `snackBar`: Notification styling

This nested structure keeps related colors organized and makes theme maintenance easier.

## JavaScript Libraries

### 1. Library Structure

```javascript
// lib/SomeLibrary.js
.pragma library

/**
 * SomeLibrary.js - Utility functions for the application
 */

// Private data/functions
var PRIVATE_CONSTANT = "value";

function privateHelper(input) {
    return input.toUpperCase();
}

// Public API
function publicFunction(input) {
    return privateHelper(input);
}

function anotherFunction(data) {
    // Implementation
}
```

### 2. Library Usage

```qml
// Import the library
import "../lib/SomeLibrary.js" as SomeLibrary

Rectangle {
    Component.onCompleted: {
        var result = SomeLibrary.publicFunction("hello");
        console.log(result); // "HELLO"
    }
}
```

### 3. Library Best Practices

- Prefer pure functions that take all needed data as parameters (easier to test).
- Use `.pragma library` only when you actually need shared cached state or want a single evaluated instance. For purely functional helpers it can be omitted for clarity.
- Export only necessary functions; keep internal helpers unexported (file‑local).
- Keep each library focused on a single responsibility (e.g. string formatting, geometry utils, search logic).
- Document the public API (purpose, params, return shape, side effects) above each exported function.
- Avoid storing references to QML objects in global (library) scope; pass them in instead to reduce lifetime surprises.
- If you introduce shared mutable state (cache, throttling flags) add a clearly marked section: `// SHARED STATE:` explaining invariants.
- Keep normalization / parsing logic outside visual components so components stay declarative.

### 4. Testing JavaScript Libraries

Add lightweight QML tests using Qt Quick Test:

1. Directory layout:
   - Put tests under `ui/tests/`
   - Name files `tst_<Something>.qml` so `qmltestrunner` auto-discovers them.
2. Import the library with a relative path:
   - `import "../lib/SearchBoxLogic.js" as SearchBoxLogic`
3. Provide minimal mock objects for any expected properties a function mutates.
4. Use `TestCase {}` from `QtTest 1.2`:
   - Functions starting with `test_` are executed.
   - Use `verify()`, `compare()`, `fail()` for assertions.
5. Keep each test independent:
   - Recreate mutable mock state in `init()` (runs before every test).
6. Add a Make target (already present: `make qml-test`) that runs:
   - `qmltestrunner-qt6 -import ui -input ui/tests` (falls back to `qmltestrunner` if needed).

Example minimal test file:

```/dev/null/example.qml#L1-40
import QtQuick 2.15
import QtTest 1.2
import "../lib/SomeLibrary.js" as SomeLibrary

TestCase {
    name: "SomeLibrary"

    function test_uppercase() {
        compare(SomeLibrary.publicFunction("abc"), "ABC")
    }
}
```

Run tests:

```/dev/null/commands.sh#L1-5
make qml-test
# or directly:
qmltestrunner-qt6 -import ui -input ui/tests
```

Guidelines for adding tests to a new library:

- Start with edge cases first (empty input, symbol-only tags, extreme numeric bounds).
- Assert both the return value and any side-effect on passed-in objects.
- For asynchronous patterns (callbacks), wrap assertions in `wait()` loops or redesign logic to be pure/ synchronous when feasible.

CI Integration (suggested):

- Add a job step after build/lint:
  - `make qml-test`
- Fail the pipeline on any QML test failure.

> Keep UI logic (animations, visual geometry) out of JS libraries unless strictly necessary; the more deterministic the library, the simpler the tests.

## Development Workflow

### 1. Initial Setup

```bash
# Initialize Go module
go mod init your-app

# Add dependencies
go get github.com/mappu/miqt

# Create directory structure
mkdir -p ui/{components,services,lib}
```

### 2. Development Cycle

```bash
# 1. Edit QML files
vim ui/components/SomeComponent.qml

# 2. Regenerate resources (if new files added)
go generate

# 3. Build and run
go build && ./your-app

# 4. For QML-only changes, just refresh the app
# (Resources are embedded, so changes need regeneration)
```

### 3. Resource Management Commands

```bash
# Regenerate resources after QML changes
go generate

# Check generated files (should be in .gitignore)
ls resources_gen.*

# Clean generated files
rm resources_gen.*
```

## Best Practices

### 1. Project Organization

- **Separate concerns**: Keep Go backend and QML frontend clearly separated
- **Use meaningful names**: Component names should reflect their purpose
- **Group related files**: Use directories to organize components, services, and libraries
- **Version control**: Ignore generated resource files

### 2. QML Component Design

- **Single responsibility**: Each component should have one clear purpose
- **Configurable**: Use properties for customization
- **Reusable**: Avoid hardcoded values and dependencies
- **Use ThemeLoader**: Always use `ThemeLoader` for consistent theming across theme variants
- **Documented**: Include usage examples in comments

```qml
/*
    SomeCard.qml
    
    A reusable card component for displaying items.
    
    Usage:
        SomeCard {
            item: myItem
            onItemClicked: function(item) {
                // Handle click
            }
        }
*/
```

**Theme Integration Example:**
```qml
Rectangle {
    ThemeLoader {
        id: theme
    }
    
    // Good: Uses theme colors
    color: theme.waypointInfoCard.background
    border.color: theme.waypointInfoCard.border
    
    // Bad: Hardcoded colors
    // color: "#000000"
    // border.color: "#444444"
}
```

### 3. API Design

- **RESTful patterns**: Use standard HTTP methods and status codes
- **JSON responses**: Consistent data format
- **Error handling**: Meaningful error messages
- **Validation**: Validate input data

### 4. State Management

- **Centralized state**: Keep application state in the root component
- **Reactive updates**: Use property bindings for automatic updates
- **Signal flow**: Use signals for component communication
- **Avoid global state**: Pass data through component properties

### 5. Performance

- **Lazy loading**: Load components only when needed
- **Image optimization**: Use appropriate image formats and sizes
- **Memory management**: Avoid memory leaks in long-running applications
- **Efficient updates**: Minimize unnecessary re-renders

## Testing and Debugging

### 1. Go Backend Testing

```go
// api_test.go
func TestItemsAPI(t *testing.T) {
    req := httptest.NewRequest("GET", "/api/items", nil)
    w := httptest.NewRecorder()
    
    handleItems(w, req)
    
    if w.Code != 200 {
        t.Errorf("Expected 200, got %d", w.Code)
    }
}
```

### 2. QML Debugging

- Use `console.log()` for debugging output
- Enable QML debugging in Qt Creator
- Use `qmllint` for static analysis:

```bash
qmllint-qt6 ui/**/*.qml
```

### 3. Development Tools

- **Qt Creator**: Full IDE with QML debugging
- **qmllint**: Static analysis tool for QML
- **Browser DevTools**: For HTTP API testing

## Error Handling

### 1. Go Error Patterns

```go
func handleAPI(w http.ResponseWriter, r *http.Request) {
    item, err := processItem(r)
    if err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    
    json.NewEncoder(w).Encode(item)
}
```

### 2. QML Error Handling

```qml
function performAction() {
    api.doSomething(function(result) {
        // Success
        handleSuccess(result);
    }, function(error) {
        // Failure
        showErrorMessage(error);
    });
}
```

## Deployment

### 1. Build Process

```bash
# Generate resources
go generate

# Build for current platform
go build -ldflags '-s -w' -o your-app

# Cross-compilation (if needed)
GOOS=windows GOARCH=amd64 go build -o your-app.exe
```

### 2. Distribution

- **Single binary**: All resources are embedded
- **No external dependencies**: Qt libraries may need to be bundled
- **Cross-platform**: Same codebase works on multiple platforms

This architecture provides a solid foundation for building maintainable, scalable QML applications with Go backends. The clear separation of concerns, consistent patterns, and robust tooling support make it suitable for both small utilities and large applications.