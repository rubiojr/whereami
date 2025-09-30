pragma ComponentBehavior: Bound
import QtQuick 2.15
import QtQuick.Controls 2.15
import QtQuick.Layouts 1.15
import "../themes"
import QtPositioning 6.5

/*
  WaypointTable.qml

  A bottom overlay table listing waypoints currently visible in the map viewport.

  Intended usage inside `MapView.qml`:

    WaypointTable {
        id: waypointTable
        map: map
        waypoints: window.waypoints
        bookmarkOnlyWaypoints: window.bookmarkOnlyWaypoints
        clusteringEnabled: window.clusteringEnabled
        clusterModel: window.clusterModel
        showNonBookmarkWaypoints: window.showNonBookmarkWaypoints
        selectedWaypoint: window.selectedWaypoint
        selectedWaypointIndex: window.selectedWaypointIndex
        open: waypointTableVisible   // bool managed by parent (toggle with Ctrl+T)
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.bottom: parent.bottom
        onWaypointActivated: function (wp) {
            // Parent should:
            //  - set window.selectedWaypoint = wp
            //  - resolve selectedWaypointIndex
            //  - center & zoom similar to search suggestion double-click
        }
    }

  Exposed API:
    property bool open
    property var  map
    property var  waypoints
    property var  bookmarkOnlyWaypoints
    property bool showNonBookmarkWaypoints
    property bool clusteringEnabled
    property var  clusterModel
    property var  selectedWaypoint
    property int  selectedWaypointIndex

    readonly property var visibleWaypoints   // array (viewport + filter processed)
    signal waypointActivated(var waypoint)   // emitted on double-click / Enter

  Behavior:
    - Hidden by default (open=false).
    - Occupies full width and 1/3 of its parent height (caller should anchor).
    - Recomputes `visibleWaypoints` on:
        * map center / zoom / size changes
        * waypoints / clusterModel / filter toggles
        * open becoming true
    - For clustering: only entries with type === "waypoint" are listed.
    - Selection highlighting:
        * Row showing `selectedWaypoint` (coordinate + name match) is tinted.
    - Double-click row:
        * Emits waypointActivated(modelData).
    - Keyboard:
        * Up/Down to move current row highlight.
        * Enter / Return activates current row.

  Notes:
    - Reassignment pattern used for visibleWaypoints so changes are observable.
    - Debounced recomputation to prevent rapid churn while panning.
*/

Rectangle {
    id: root
    ThemeLoader {
        id: theme
    }
    color: theme.waypointTable.background
    border.color: theme.waypointTable.border
    border.width: 1
    radius: 5
    visible: open
    anchors.bottomMargin: 20
    anchors.leftMargin: 10
    anchors.rightMargin: 10
    opacity: open ? 1 : 0
    Behavior on opacity {
        NumberAnimation {
            duration: 140
            easing.type: Easing.OutQuad
        }
    }
    // Slide in/out transform (translate from bottom when closed)
    transform: Translate {
        id: slideTrans
        y: root.open ? 0 : root.height
        Behavior on y {
            NumberAnimation {
                duration: 180
                easing.type: Easing.OutQuad
            }
        }
    }

    // Public API
    property bool open: false
    property var map: null
    property var waypoints: []
    property var bookmarkOnlyWaypoints: []
    property bool showNonBookmarkWaypoints: true
    property bool clusteringEnabled: false
    property var clusterModel: []
    // Tag filtering (injected from parent MapView)
    property bool tagFilterActive: false
    property var tagFilteredWaypoints: []
    // External selection (read-only here). We never assign to these to keep parent binding intact.
    property var externalSelectedWaypoint: null
    property int externalSelectedWaypointIndex: -1

    // Derived list (viewport filtered). Reassigned wholesale for change notification.
    property var visibleWaypoints: []
    // Resolved row index of externalSelectedWaypoint within visibleWaypoints (-1 if not present)
    property int resolvedSelectedRow: -1

    // Style knobs
    property int rowHeight: 30
    property color headerTextColor: theme.waypointTable.headerText
    property color headerSeparatorColor: theme.waypointTable.headerSeparator
    property color rowAltColorA: theme.waypointTable.rowAltA
    property color rowAltColorB: theme.waypointTable.rowAltB
    property color rowSelectedColor: theme.waypointTable.rowSelected
    property color rowHoverColor: theme.waypointTable.rowHover
    property color badgeBookmarkColor: theme.waypointTable.badgeBookmark
    property int hoverIndex: -1
    // Fixed column widths for consistent alignment
    property int colLatWidth: 80
    property int colLonWidth: 90
    property int colStarWidth: 24
    property int colTagsWidth: 140   // NEW: Tags column width

    // (hoverIndex removed – only selected row is highlighted now)

    // Formatting helper for tags column
    // Supports either plain string tags or enriched objects:
    //   { raw, emoji?, display? }
    function formatTags(wp) {
        if (!wp || !wp.tags || wp.tags.length === 0)
            return "";
        var parts = [];
        for (var i = 0; i < wp.tags.length; i++) {
            var t = wp.tags[i];
            if (t === null || t === undefined)
                continue;
            if (typeof t === "object") {
                if (t.display) {
                    // Backend already provided preferred display (may be emoji-only for symbolic tags)
                    parts.push("" + t.display);
                } else if (t.emoji && t.raw) {
                    var rawStr = "" + t.raw;
                    // If raw is a run of only mapped symbolic characters, prefer pure repeated emojis
                    // (covers legacy objects without a display field)
                    if (/^[\\*\\$!\\?\\+\\-x@#%&]+$/.test(rawStr)) {
                        var rep = "";
                        for (var r = 0; r < rawStr.length; r++)
                            rep += t.emoji;
                        parts.push(rep);
                    } else {
                        parts.push(t.emoji + " " + rawStr);
                    }
                } else if (t.raw) {
                    parts.push("" + t.raw);
                } else {
                    parts.push(String(t)); // fallback stringify
                }
            } else {
                parts.push(String(t));
            }
        }
        if (parts.length === 0)
            return "";
        var joined = parts.join(", ");
        if (joined.length > 40)
            joined = joined.slice(0, 37) + "…";
        return joined;
    }

    signal waypointActivated(var waypoint)
    signal closeRequested
    // Emitted when the user selects a row (table no longer mutates selectedWaypoint directly)
    signal selectionRequested(var waypoint)

    // Sizing (caller usually provides anchors; maintain the 1/3 height rule)
    anchors.left: parent ? parent.left : undefined
    anchors.right: parent ? parent.right : undefined
    anchors.bottom: parent ? parent.bottom : undefined
    height: parent ? parent.height / 3 : 260

    // Internal state
    property int currentIndex: -1    // navigational highlight (ListView.currentIndex)

    // Debounce recomputation to avoid excess work while user pans/zooms
    Timer {
        id: recomputeTimer
        interval: 120
        repeat: false
        onTriggered: root.recomputeVisible()
    }

    function scheduleRecompute() {
        if (!open)
            return;
        recomputeTimer.restart();
    }

    // Source list factoring in clustering/filter but not viewport
    function effectiveSource() {
        // Table should reflect active filters (tag filter has highest priority).
        if (tagFilterActive)
            return tagFilteredWaypoints || [];
        if (!showNonBookmarkWaypoints)
            return bookmarkOnlyWaypoints || [];
        return waypoints || [];
    }

    // Compute viewport bounding box and filter
    function recomputeVisible() {
        if (!map) {
            visibleWaypoints = [];
            return;
        }
        var src = effectiveSource();
        var tl = map.toCoordinate(Qt.point(0, 0));
        var br = map.toCoordinate(Qt.point(map.width, map.height));
        if (!tl || !br || !src) {
            visibleWaypoints = [];
            return;
        }
        var minLat = Math.min(tl.latitude, br.latitude);
        var maxLat = Math.max(tl.latitude, br.latitude);
        var minLon = Math.min(tl.longitude, br.longitude);
        var maxLon = Math.max(tl.longitude, br.longitude);

        var arr = [];
        for (var i = 0; i < src.length; i++) {
            var w = src[i];
            if (!w || w.lat === undefined || w.lon === undefined)
                continue;
            if (w.lat >= minLat && w.lat <= maxLat && w.lon >= minLon && w.lon <= maxLon) {
                arr.push(w);
            }
        }

        // Stable sort: name asc, then lat, lon
        arr.sort(function (a, b) {
            var an = a.name || "";
            var bn = b.name || "";
            if (an < bn)
                return -1;
            if (an > bn)
                return 1;
            if (a.lat < b.lat)
                return -1;
            if (a.lat > b.lat)
                return 1;
            if (a.lon < b.lon)
                return -1;
            if (a.lon > b.lon)
                return 1;
            return 0;
        });

        visibleWaypoints = arr;
        // Preserve selection highlight if still present
        syncCurrentIndexToSelection();
    }

    function syncCurrentIndexToSelection() {
        // Clear when no external selection
        if (!externalSelectedWaypoint) {
            resolvedSelectedRow = -1;
            if (list.currentIndex !== -1)
                list.currentIndex = -1;
            return;
        }

        // Resolve index
        var idx = -1;
        for (var i = 0; i < visibleWaypoints.length; i++) {
            var w = visibleWaypoints[i];
            if (w && w.name === externalSelectedWaypoint.name && Math.abs(w.lat - externalSelectedWaypoint.lat) < 1e-9 && Math.abs(w.lon - externalSelectedWaypoint.lon) < 1e-9) {
                idx = i;
                break;
            }
        }

        resolvedSelectedRow = idx;

        // Only adjust currentIndex if needed; force refresh if same
        if (idx !== list.currentIndex) {
            list.currentIndex = idx;
        } else if (idx !== -1) {
            list.currentIndex = -1;
            list.currentIndex = idx;
        }

        if (idx >= 0)
            list.positionViewAtIndex(idx, ListView.Contain);
    }

    // React to external selection changes (MapView owns the selection)
    onExternalSelectedWaypointChanged: {
        hoverIndex = -1;
        syncCurrentIndexToSelection();
        // Retry once if not yet visible after map pan/zoom
        if (externalSelectedWaypoint && list.currentIndex === -1)
            Qt.callLater(syncCurrentIndexToSelection);
    }
    onExternalSelectedWaypointIndexChanged: {
        hoverIndex = -1;
        syncCurrentIndexToSelection();
    }

    // Triggers that affect visibility
    onWaypointsChanged: scheduleRecompute()
    onBookmarkOnlyWaypointsChanged: scheduleRecompute()
    onShowNonBookmarkWaypointsChanged: scheduleRecompute()
    onClusterModelChanged: scheduleRecompute()
    onClusteringEnabledChanged: scheduleRecompute()
    onTagFilterActiveChanged: scheduleRecompute()
    onTagFilteredWaypointsChanged: scheduleRecompute()
    onOpenChanged: {
        if (open) {
            recomputeVisible();
            root.forceActiveFocus();
        }
    }

    // Re-sync when viewport-derived list changes
    onVisibleWaypointsChanged: {
        if (externalSelectedWaypoint)
            syncCurrentIndexToSelection();
    }
    // Monitor map changes (center, zoom, size)
    Connections {
        target: root.map
        enabled: root.map !== null
        function onCenterChanged() {
            root.scheduleRecompute();
        }
        function onZoomLevelChanged() {
            root.scheduleRecompute();
        }
        function onWidthChanged() {
            root.scheduleRecompute();
        }
        function onHeightChanged() {
            root.scheduleRecompute();
        }
    }

    ColumnLayout {
        anchors.fill: parent
        anchors.margins: 10
        spacing: 6

        // Header
        RowLayout {
            Layout.fillWidth: true
            implicitHeight: 26

            Text {
                id: titleLabel
                text: "Visible Waypoints (" + root.visibleWaypoints.length + ")"
                color: root.headerTextColor
                font.bold: true
                font.pixelSize: theme && theme.scale ? theme.scale(2) : 14
            }

            Item {
                Layout.fillWidth: true
            }

            Button {
                id: closeBtn
                text: "Close"
                Layout.rightMargin: 6
                // Emit a signal instead of assigning to `open` so the binding
                // in the parent (open: waypointTableVisible) remains intact.
                focusPolicy: Qt.NoFocus
                onClicked: root.closeRequested()
                background: Rectangle {
                    implicitWidth: 72
                    implicitHeight: 28
                    radius: 4
                    color: closeBtn.hovered ? theme.waypointTable.closeButtonBackgroundHover : theme.waypointTable.closeButtonBackground
                    border.color: closeBtn.hovered ? theme.waypointTable.closeButtonBorderHover : theme.waypointTable.closeButtonBorder
                    border.width: 1
                }
                contentItem: Text {
                    text: closeBtn.text
                    color: theme.waypointTable.closeButtonText
                    font.bold: true
                    horizontalAlignment: Text.AlignHCenter
                    verticalAlignment: Text.AlignVCenter
                    anchors.centerIn: parent
                }
            }
        }

        Rectangle {
            Layout.fillWidth: true
            implicitHeight: 1
            color: root.headerSeparatorColor
        }

        // Column headers
        RowLayout {
            Layout.fillWidth: true
            implicitHeight: 20
            spacing: 12
            Text {
                text: "Name"
                color: theme.waypointTable.columnHeaderText
                font.pixelSize: theme && theme.scale ? theme.scale(1) : 11
                Layout.fillWidth: true
            }
            Text {
                text: "Tags"
                color: theme.waypointTable.columnHeaderText
                font.pixelSize: theme && theme.scale ? theme.scale(1) : 11
                Layout.preferredWidth: root.colTagsWidth
                elide: Text.ElideRight
            }
            Text {
                text: "Lat"
                color: theme.waypointTable.columnHeaderText
                font.pixelSize: theme && theme.scale ? theme.scale(1) : 11
                Layout.preferredWidth: root.colLatWidth
            }
            Text {
                text: "Lon"
                color: theme.waypointTable.columnHeaderText
                font.pixelSize: theme && theme.scale ? theme.scale(1) : 11
                Layout.preferredWidth: root.colLonWidth
            }
            Text {
                text: "★"
                color: theme.waypointTable.columnHeaderText
                font.pixelSize: theme && theme.scale ? theme.scale(1) : 11
                Layout.preferredWidth: root.colStarWidth
                horizontalAlignment: Text.AlignHCenter
            }
        }

        Rectangle {
            Layout.fillWidth: true
            implicitHeight: 1
            color: theme.waypointTable.separator
        }

        // List of visible waypoints
        ListView {
            id: list
            Layout.fillWidth: true
            Layout.fillHeight: true
            clip: true
            model: root.visibleWaypoints
            currentIndex: -1
            keyNavigationWraps: false

            delegate: Rectangle {
                id: rowRect
                required property int index
                // Explicit waypoint reference (avoid relying on implicit modelData which was undefined under some bindings)
                property var wp: (root.visibleWaypoints && rowRect.index >= 0 && rowRect.index < root.visibleWaypoints.length) ? root.visibleWaypoints[rowRect.index] : null

                width: list.width
                implicitHeight: root.rowHeight
                height: root.rowHeight
                color: rowColor()
                border.color: theme.waypointTable.rowBorder
                border.width: 0

                function rowColor() {
                    var selected = (root.resolvedSelectedRow === rowRect.index);
                    if (selected)
                        return root.rowSelectedColor;
                    if (root.hoverIndex === rowRect.index || list.currentIndex === rowRect.index)
                        return root.rowHoverColor;
                    return (rowRect.index % 2 === 0) ? root.rowAltColorA : root.rowAltColorB;
                }

                RowLayout {
                    anchors.fill: parent
                    anchors.margins: 6
                    spacing: 12

                    Text {
                        // Explicit width so other columns keep their space (avoids RowLayout collapse/overlap)
                        text: (rowRect.wp && rowRect.wp.name && rowRect.wp.name !== "") ? rowRect.wp.name : "(unnamed)"
                        color: theme.waypointTable.nameText
                        elide: Text.ElideRight
                        Layout.preferredWidth: Math.max(60, rowRect.width - (root.colTagsWidth + root.colLatWidth + root.colLonWidth + root.colStarWidth) - (12 * 4))
                        font.pixelSize: theme && theme.scale ? theme.scale(2) : 13
                        font.bold: root.resolvedSelectedRow === rowRect.index
                    }
                    Text {
                        // Tags column
                        text: root.formatTags(rowRect.wp)
                        color: theme.waypointTable.tagsText
                        elide: Text.ElideRight
                        font.pixelSize: theme && theme.scale ? theme.scale(1) : 11
                        Layout.preferredWidth: root.colTagsWidth
                        font.bold: root.resolvedSelectedRow === rowRect.index
                        Accessible.name: "Waypoint tags"
                    }
                    Text {
                        text: (rowRect.wp && rowRect.wp.lat !== undefined) ? rowRect.wp.lat.toFixed(5) : ""
                        color: theme.waypointTable.coordText
                        font.pixelSize: theme && theme.scale ? theme.scale(1) : 12
                        Layout.preferredWidth: root.colLatWidth
                        horizontalAlignment: Text.AlignRight
                        font.bold: root.resolvedSelectedRow === rowRect.index
                    }
                    Text {
                        text: (rowRect.wp && rowRect.wp.lon !== undefined) ? rowRect.wp.lon.toFixed(5) : ""
                        color: theme.waypointTable.coordText
                        font.pixelSize: theme && theme.scale ? theme.scale(1) : 12
                        Layout.preferredWidth: root.colLonWidth
                        horizontalAlignment: Text.AlignRight
                        font.bold: root.resolvedSelectedRow === rowRect.index
                    }
                    Text {
                        text: (rowRect.wp && rowRect.wp.bookmark) ? "★" : ""
                        color: (rowRect.wp && rowRect.wp.bookmark) ? root.badgeBookmarkColor : "#666666"
                        font.bold: (rowRect.wp && rowRect.wp.bookmark) || (root.resolvedSelectedRow === rowRect.index)
                        font.pixelSize: theme && theme.scale ? theme.scale(2) : 14
                        horizontalAlignment: Text.AlignHCenter
                        verticalAlignment: Text.AlignVCenter
                        Layout.preferredWidth: root.colStarWidth
                    }
                }

                MouseArea {
                    anchors.fill: parent
                    hoverEnabled: true
                    onEntered: {
                        list.currentIndex = rowRect.index;
                        root.hoverIndex = rowRect.index;
                    }
                    onExited: {
                        if (root.hoverIndex === rowRect.index) {
                            root.hoverIndex = -1;
                        }
                    }
                    onClicked: {
                        list.currentIndex = rowRect.index;
                        root.hoverIndex = rowRect.index;
                        if (rowRect.wp) {
                            // Request external selection and immediately activate (center/zoom)
                            root.selectionRequested(rowRect.wp);
                            root.waypointActivated(rowRect.wp);
                        }
                    }
                }
            }

            // Keyboard navigation & activation (now also handles Escape directly)
            Keys.onPressed: function (event) {
                if (event.key === Qt.Key_Escape) {
                    root.closeRequested();
                    event.accepted = true;
                    return;
                }
                if (event.key === Qt.Key_Up) {
                    if (list.count === 0)
                        return;
                    var ni = list.currentIndex <= 0 ? 0 : list.currentIndex - 1;
                    list.currentIndex = ni;
                    event.accepted = true;
                } else if (event.key === Qt.Key_Down) {
                    if (list.count === 0)
                        return;
                    var di = list.currentIndex < 0 ? 0 : Math.min(list.count - 1, list.currentIndex + 1);
                    list.currentIndex = di;
                    event.accepted = true;
                } else if (event.key === Qt.Key_Return || event.key === Qt.Key_Enter) {
                    if (list.currentIndex >= 0 && list.currentIndex < root.visibleWaypoints.length) {
                        root.waypointActivated(root.visibleWaypoints[list.currentIndex]);
                        event.accepted = true;
                    }
                }
            }
            // Fallback: if ListView loses focus, still allow Escape via this handler
            Keys.onEscapePressed: root.closeRequested()
        }
    }

    // Ensure ListView focus so keyboard works when opened
    onVisibleChanged: {
        if (visible) {
            list.forceActiveFocus();
        }
    }

    // Close on Escape without breaking parent binding (use ApplicationShortcut so focus is irrelevant)
    // Invoke the close button's clicked handler first so any future logic tied to it remains centralized.
    // (Escape-to-close removed)
}
