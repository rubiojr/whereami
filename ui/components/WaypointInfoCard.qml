pragma ComponentBehavior: Bound
import QtQuick 2.15
import QtQuick.Layouts 1.15
import QtQuick.Controls 2.15

import "../themes"

/*
    WaypointInfoCard
    -----------------
    A compact, modern "card" style visual component to display
    details about a waypoint. Inspired by the provided mockup:
      + Title strip (e.g. waypoint name)
      + Prominent primary line (coordinates or time)
      + Supplementary date / time (if available)
      + Tag chip indicating source (Bookmark / GPX)
      + Optional description

    Usage:
        WaypointInfoCard {
            waypoint: {
                "name": "Some Place",
                "lat": 51.2345,
                "lon": -0.1234,
                "time": "2025-09-23T07:59:00Z",
                "desc": "Optional description",
                "bookmark": true
            }
        }

    Displays nothing if waypoint is null.
*/

Rectangle {
    id: root
    ThemeLoader {
        id: theme
    }
    property var waypoint: null
    // Separate reactive list used by the Repeater to ensure model change signals fire
    property var waypointTags: []
    // Tag editing state
    property bool addingTag: false
    property string pendingNewTag: ""
    // Editing state
    property bool editingName: false
    property string tempName: ""
    // True when current waypoint is a transient geocoded (non-persisted) result
    property bool isTransientGeocode: !!(waypoint && waypoint.transient === true)
    signal nameEdited(var waypoint)    // Emitted after a successful (or reverted) rename to force bindings to refresh
    signal addGeocodeRequested(var waypoint)   // Emitted when user clicks + Add for a transient geocoded waypoint
    property var api: null             // Injected centralized API service (API.qml)
    width: 300
    implicitHeight: contentCol.implicitHeight + 2
    radius: 5
    // Semi-transparent background to match WaypointTable style
    color: theme.waypointInfoCard.background
    border.color: theme.waypointInfoCard.border
    border.width: 1
    clip: true

    onWaypointChanged: {
        // Reset editing state when waypoint changes
        root.editingName = false;
        root.tempName = root.waypoint && root.waypoint.name ? root.waypoint.name : "";
        root.waypointTags = (root.waypoint && root.waypoint.tags) ? root.waypoint.tags.slice() : [];
    }

    // ---- Time / date helpers ----
    function hasTime() {
        return !!(root.waypoint && root.waypoint.time && root.waypoint.time.length > 0);
    }

    function parsedDate() {
        if (!root.hasTime())
            return null;
        var d = new Date(root.waypoint.time);
        if (isNaN(d.getTime()))
            return null;
        return d;
    }

    function formatTime(d) {
        // 07:59 AM style
        var h = d.getUTCHours();
        var m = d.getUTCMinutes();
        var ap = h >= 12 ? "PM" : "AM";
        var hh = ((h + 11) % 12 + 1); // 12-hour
        return (hh < 10 ? hh : hh) + ":" + (m < 10 ? "0" + m : m) + " " + ap;
    }

    function formatDate(d) {
        var day = d.getUTCDate();
        var monthNames = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
        return day + ", " + monthNames[d.getUTCMonth()];
    }

    ColumnLayout {
        id: contentCol
        anchors.fill: parent
        anchors.margins: 0
        spacing: 0

        // Title bar
        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 48
            border.width: 0
            radius: 5
            color: theme.accent
            antialiasing: true

            // Editable name field (visible only when editing)
            TextField {
                id: nameField
                visible: root.editingName
                anchors.verticalCenter: parent.verticalCenter
                anchors.left: parent.left
                anchors.leftMargin: 14
                anchors.right: editBtn.left
                anchors.rightMargin: 8
                text: root.tempName
                font.pixelSize: theme.waypointInfoCardTitleSize
                font.bold: true
                color: theme.waypointInfoCard.primaryText
                selectionColor: theme.accent
                selectedTextColor: theme.white
                background: Rectangle {
                    // Frameless look
                    color: theme.waypointInfoCard.nameField.background
                    border.width: 0
                }
                padding: 0
                leftPadding: 0
                rightPadding: 0
                onAccepted: editBtn.clicked()
            }

            // Read-only title
            Text {
                id: titleText
                visible: !root.editingName
                text: root.waypoint && root.waypoint.name ? root.waypoint.name : (root.waypoint ? "Waypoint" : "")
                anchors.verticalCenter: parent.verticalCenter
                anchors.left: parent.left
                anchors.leftMargin: 14
                anchors.right: editBtn.left
                anchors.rightMargin: 8
                font.pixelSize: theme.waypointInfoCardTitleSize
                font.bold: true
                elide: Text.ElideRight
            }

            // Edit / Save button
            Button {
                id: editBtn
                visible: root.waypoint !== null
                anchors.verticalCenter: parent.verticalCenter
                anchors.right: parent.right
                anchors.rightMargin: 10
                text: root.editingName ? "Save" : "Edit"
                font.pixelSize: theme.scale ? theme.scale(1) : 12

                // Increase pointer affordance
                hoverEnabled: true
                focusPolicy: Qt.TabFocus

                contentItem: Text {
                    text: editBtn.text
                    font.bold: true
                    color: editBtn.pressed ? (theme.waypointInfoCard.editButton.textPressed || theme.waypointInfoCard.editButton.text) : (editBtn.hovered ? (theme.waypointInfoCard.editButton.textHover || theme.waypointInfoCard.editButton.text) : theme.waypointInfoCard.editButton.text)
                    horizontalAlignment: Text.AlignHCenter
                    verticalAlignment: Text.AlignVCenter
                    // Smooth text color transition
                    Behavior on color {
                        ColorAnimation {
                            duration: 120
                        }
                    }
                }

                background: Rectangle {
                    implicitWidth: 60
                    implicitHeight: 28
                    radius: 5
                    color: editBtn.pressed ? (theme.waypointInfoCard.editButton.backgroundPressed || theme.waypointInfoCard.editButton.background) : (editBtn.hovered ? (theme.waypointInfoCard.editButton.backgroundHover || theme.waypointInfoCard.editButton.background) : theme.waypointInfoCard.editButton.background)
                    border.width: 1
                    border.color: editBtn.pressed ? (theme.waypointInfoCard.editButton.borderPressed || theme.waypointInfoCard.editButton.border || "transparent") : (editBtn.hovered ? (theme.waypointInfoCard.editButton.borderHover || theme.waypointInfoCard.editButton.border || "transparent") : (theme.waypointInfoCard.editButton.border || "transparent"))
                    // Smooth background color transition
                    Behavior on color {
                        ColorAnimation {
                            duration: 120
                        }
                    }
                }

                // Simple transitions handled inside background/text Behaviors

                Keys.onReturnPressed: clicked()
                Keys.onEnterPressed: clicked()

                onClicked: {
                    if (!root.waypoint)
                        return;
                    if (root.editingName) {
                        var newName = nameField.text.trim();
                        var oldName = root.waypoint.name;
                        var changed = (newName.length > 0 && newName !== oldName);
                        if (changed) {
                            if (root.waypoint.bookmark && root.api) {
                                root.api.renameWaypoint(root.waypoint, newName);
                            } else {
                                root.waypoint.name = newName;
                                root.nameEdited(root.waypoint);
                            }
                        }
                        root.tempName = root.waypoint.name;
                        root.editingName = false;
                    } else {
                        root.tempName = root.waypoint && root.waypoint.name ? root.waypoint.name : "";
                        nameField.text = root.tempName;
                        root.editingName = true;
                        nameField.forceActiveFocus();
                        nameField.selectAll();
                    }
                }
            }

            // Cancel button appears only during editing
            Button {
                id: cancelBtn
                visible: root.editingName
                anchors.verticalCenter: parent.verticalCenter
                anchors.right: editBtn.left
                anchors.rightMargin: 6
                text: "✕"
                font.pixelSize: theme.scale ? theme.scale(1) : 12
                contentItem: Text {
                    text: cancelBtn.text
                    font.bold: true
                    color: theme.waypointInfoCard.cancelButton.text
                    horizontalAlignment: Text.AlignHCenter
                    verticalAlignment: Text.AlignVCenter
                }
                background: Rectangle {
                    implicitWidth: 36
                    implicitHeight: 32
                    color: cancelBtn.down ? theme.waypointInfoCard.cancelButton.backgroundPressed : theme.waypointInfoCard.cancelButton.background
                    border.color: theme.waypointInfoCard.cancelButton.border
                    radius: 5
                }
                onClicked: {
                    root.editingName = false;
                    nameField.text = root.tempName;
                }
            }
        }

        // Main content area
        ColumnLayout {
            Layout.fillWidth: true
            Layout.margins: 14
            spacing: 10
            visible: root.waypoint !== null

            // Primary row (big metric + date)
            RowLayout {
                Layout.fillWidth: true

                Text {
                    // Big primary info – prefer time (if present) else coordinates
                    text: {
                        if (!root.waypoint)
                            return "";
                        var d = root.parsedDate();
                        if (d) {
                            return root.formatTime(d);
                        }
                        // Fallback to coordinates (lat)
                        return (root.waypoint.lat !== undefined ? root.waypoint.lat.toFixed(5) : "");
                    }
                    font.pixelSize: theme.waypointInfoCardPrimaryInfoFontSize
                    font.bold: true
                    color: theme.waypointInfoCard.primaryText
                    Layout.alignment: Qt.AlignVCenter | Qt.AlignLeft
                }

                Item {
                    Layout.fillWidth: true
                }

                Text {
                    text: {
                        if (!root.waypoint)
                            return "";
                        var d = root.parsedDate();
                        if (d)
                            return root.formatDate(d);
                        // Fallback to lon
                        return (root.waypoint.lon !== undefined ? root.waypoint.lon.toFixed(5) : "");
                    }
                    font.pixelSize: theme.scale ? theme.scale(2) : 14
                    color: theme.waypointInfoCard.primaryText
                    Layout.alignment: Qt.AlignRight | Qt.AlignTop
                }
            }

            // Secondary chips + timezone / classification line
            RowLayout {
                Layout.fillWidth: true
                spacing: 8

                // Source chip (Bookmark / GPX / Geocode)
                Rectangle {
                    visible: root.waypoint !== null
                    // Reuse existing color themes; transient (geocode) uses GPX styling for now
                    color: root.waypoint ? (root.waypoint.bookmark ? theme.waypointInfoCard.bookmarkChip.background : theme.waypointInfoCard.gpxChip.background) : theme.waypointInfoCard.gpxChip.background
                    radius: 5
                    implicitHeight: 22
                    implicitWidth: chipText.implicitWidth + 12
                    border.color: root.waypoint ? (root.waypoint.bookmark ? theme.waypointInfoCard.bookmarkChip.border : theme.waypointInfoCard.gpxChip.border) : theme.waypointInfoCard.gpxChip.border
                    border.width: 1

                    Text {
                        id: chipText
                        text: root.waypoint ? (root.waypoint.bookmark ? "BOOKMARK" : (root.waypoint.transient ? "GEOCODE" : "GPX")) : ""
                        anchors.centerIn: parent
                        font.pixelSize: theme.scale ? theme.scale(1) : 11
                        font.bold: true
                        color: theme.waypointInfoCard.chipText
                    }
                }

                // UTC / timezone chip (only if we have time)
                Rectangle {
                    visible: !!root.hasTime()
                    color: theme.waypointInfoCard.timeChip.background
                    radius: 5
                    implicitHeight: 22
                    implicitWidth: tzText.implicitWidth + 12
                    border.color: theme.waypointInfoCard.timeChip.border
                    border.width: 1

                    Text {
                        id: tzText
                        text: "UTC"
                        anchors.centerIn: parent
                        font.pixelSize: theme.scale ? theme.scale(1) : 11
                        font.bold: true
                        color: theme.waypointInfoCard.timeChip.text
                    }
                }

                Item {
                    Layout.fillWidth: true
                }
            }

            // Tag viewer / editor (show for all; internal elements conditionally hidden for transient)
            ColumnLayout {
                visible: root.waypoint !== null
                spacing: 4
                Layout.fillWidth: true

                // Header + add button
                RowLayout {
                    Layout.fillWidth: true
                    visible: (root.waypoint && root.waypoint.tags && root.waypoint.tags.length > 0) || true
                    spacing: 8
                    Text {
                        text: root.waypointTags.length > 0 ? ("Tags (" + root.waypointTags.length + ")") : "Tags"
                        font.pixelSize: theme.scale ? theme.scale(1) : 12
                        font.bold: true
                        color: theme.waypointInfoCard.primaryText
                    }
                    Item {
                        Layout.fillWidth: true
                    }
                    Button {
                        id: addTagBtn
                        // When transient (geocode) show + Add; otherwise tag editing states
                        text: (root.waypoint && root.waypoint.transient) ? "+ Add Bookmark" : (root.addingTag ? "…" : "+ Tag")
                        visible: root.waypoint !== null
                        enabled: !!(root.waypoint && (root.waypoint.transient || root.waypoint.bookmark))
                        font.pixelSize: theme.scale ? theme.scale(1) : 11
                        onClicked: {
                            if (!root.waypoint)
                                return;
                            if (root.waypoint.transient) {
                                // Emit request to open AddWaypointDialog with preset data
                                if (root.addGeocodeRequested)
                                    root.addGeocodeRequested(root.waypoint);
                            } else {
                                if (!root.addingTag) {
                                    root.addingTag = true;
                                    tagInputFocus.restart();
                                }
                            }
                        }
                        hoverEnabled: true
                        contentItem: Text {
                            text: addTagBtn.text
                            font.bold: true
                            color: addTagBtn.pressed ? (theme.waypointInfoCard.addTagButton.textPressed || theme.waypointInfoCard.addTagButton.text) : (addTagBtn.hovered ? (theme.waypointInfoCard.addTagButton.textHover || theme.waypointInfoCard.addTagButton.text) : theme.waypointInfoCard.addTagButton.text)
                            horizontalAlignment: Text.AlignHCenter
                            verticalAlignment: Text.AlignVCenter
                            Behavior on color {
                                ColorAnimation {
                                    duration: 120
                                }
                            }
                        }
                        background: Rectangle {
                            implicitWidth: 60
                            implicitHeight: 28
                            radius: 5
                            color: addTagBtn.pressed ? (theme.waypointInfoCard.addTagButton.backgroundPressed || theme.waypointInfoCard.addTagButton.background) : (addTagBtn.hovered ? (theme.waypointInfoCard.addTagButton.backgroundHover || theme.waypointInfoCard.addTagButton.background) : theme.waypointInfoCard.addTagButton.background)
                            border.width: (theme.waypointInfoCard.addTagButton.border || theme.waypointInfoCard.addTagButton.borderHover || theme.waypointInfoCard.addTagButton.borderPressed) ? 1 : 0
                            border.color: addTagBtn.pressed ? (theme.waypointInfoCard.addTagButton.borderPressed || theme.waypointInfoCard.addTagButton.border || "transparent") : (addTagBtn.hovered ? (theme.waypointInfoCard.addTagButton.borderHover || theme.waypointInfoCard.addTagButton.border || "transparent") : (theme.waypointInfoCard.addTagButton.border || "transparent"))
                            Behavior on color {
                                ColorAnimation {
                                    duration: 120
                                }
                            }
                        }
                    }
                }

                Flow {
                    id: tagFlow
                    visible: root.waypoint && !root.waypoint.transient
                    Layout.fillWidth: true
                    Layout.preferredHeight: childrenRect.height
                    // Removed invalid implicitWidth assignment (implicitWidth is read-only). Width is managed by Layout.
                    spacing: 6
                    Repeater {
                        id: tagRepeater
                        model: root.waypointTags
                        delegate: Rectangle {
                            id: tagChip
                            required property var modelData
                            // Support enriched tag objects ({ raw, emoji?, display? }) or legacy plain strings.
                            property var tagObj: modelData
                            property string tagRaw: (tagObj && typeof tagObj === "object" && tagObj.raw !== undefined) ? ("" + tagObj.raw) : (typeof tagObj === "string" ? tagObj : "")
                            property string tagText: (tagObj && typeof tagObj === "object" && tagObj.display) ? ("" + tagObj.display) : ((tagObj && typeof tagObj === "object" && tagObj.emoji && tagObj.raw) ? (tagObj.emoji + " " + tagObj.raw) : tagRaw)
                            onModelDataChanged: {
                                tagChip.tagObj = modelData;
                                tagChip.tagRaw = (tagChip.tagObj && typeof tagChip.tagObj === "object" && tagChip.tagObj.raw !== undefined) ? ("" + tagChip.tagObj.raw) : (typeof tagChip.tagObj === "string" ? tagChip.tagObj : "");
                                tagChip.tagText = (tagChip.tagObj && typeof tagChip.tagObj === "object" && tagChip.tagObj.display) ? ("" + tagChip.tagObj.display) : ((tagChip.tagObj && typeof tagChip.tagObj === "object" && tagChip.tagObj.emoji && tagChip.tagObj.raw) ? (tagChip.tagObj.emoji + " " + tagChip.tagObj.raw) : tagChip.tagRaw);
                            }
                            radius: 6

                            // Use fixed height to ensure consistency between text and emoji chips
                            implicitHeight: 28
                            implicitWidth: chipRow.implicitWidth + 16
                            color: theme.waypointInfoCard.tagChip.background
                            border.color: theme.waypointInfoCard.tagChip.border
                            border.width: 1

                            Row {
                                id: chipRow
                                anchors.verticalCenter: parent.verticalCenter
                                anchors.left: parent.left
                                anchors.leftMargin: 8
                                spacing: 4

                                Text {
                                    text: tagChip.tagText
                                    font.pixelSize: theme.scale ? theme.scale(1) : 12
                                    color: theme.waypointInfoCard.tagChip.text
                                    font.bold: false
                                    anchors.verticalCenter: parent.verticalCenter
                                    elide: Text.ElideRight
                                    wrapMode: Text.NoWrap
                                    // Ensure consistent vertical alignment for text and emojis
                                    verticalAlignment: Text.AlignVCenter
                                }

                                // Larger click target for delete "×" with subtle hover feedback
                                MouseArea {
                                    id: deleteTagArea
                                    width: 18
                                    height: parent.height - 4   // enlarge vertical hit area but tie to chip height for centering
                                    anchors.verticalCenter: parent.verticalCenter
                                    cursorShape: Qt.PointingHandCursor
                                    onClicked: (root.api && tagChip.tagRaw.length > 0) ? root.api.deleteTag(root.waypoint, tagChip.tagRaw) : null
                                    hoverEnabled: true
                                    Rectangle {
                                        anchors.fill: parent
                                        radius: 4
                                        color: (deleteTagArea.containsMouse ? theme.waypointInfoCard.tagChip.deleteHover : "transparent")
                                        border.color: (deleteTagArea.containsMouse ? theme.waypointInfoCard.tagChip.deleteBorderHover : "transparent")
                                        border.width: (deleteTagArea.containsMouse ? 1 : 0)
                                    }
                                    Text {
                                        anchors.centerIn: parent
                                        text: "×"
                                        font.pixelSize: theme.scale ? theme.scale(1) : 12
                                        color: theme.waypointInfoCard.tagChip.text
                                        font.bold: true
                                        opacity: (deleteTagArea.containsMouse ? 0.95 : 0.7)
                                    }
                                }
                            }
                        }
                    }
                    // Placeholder when no tags
                    // Plain-text fallback list (ensures visibility even if chips styling fails)
                    // Removed plain text fallback (chips now have guaranteed visibility with explicit sizing)
                    Text {
                        visible: root.waypointTags.length === 0 && !root.addingTag
                        text: "No tags"
                        font.pixelSize: theme.scale ? theme.scale(1) : 11
                        color: theme.waypointInfoCard.noTagsText
                        padding: 2
                    }

                    // Inline new tag editor
                    Rectangle {
                        visible: root.addingTag
                        color: theme.transparent
                        border.width: 0
                        height: 28
                        implicitWidth: tagInput.implicitWidth
                        TextField {
                            id: tagInput
                            text: ""
                            width: Math.max(80, implicitWidth)
                            font.pixelSize: theme.scale ? theme.scale(1) : 12
                            color: theme.waypointInfoCard.tagInput.text
                            placeholderTextColor: theme.waypointInfoCard.tagInput.placeholder
                            selectionColor: theme.waypointInfoCard.tagInput.selection
                            selectedTextColor: theme.white
                            background: Rectangle {
                                color: theme.waypointInfoCard.tagInput.background
                                radius: 4
                                border.color: theme.waypointInfoCard.tagInput.border
                                border.width: 1
                            }
                            // Live substitution so user immediately sees canonical emoji forms
                            // Emoji translation removed: tags are stored/displayed verbatim
                            onTextChanged: {}
                            onAccepted: {
                                var v = text.trim();
                                if (v.length > 0) {
                                    // Send raw tag verbatim (backend performs any enrichment)
                                    if (root.api && root.waypoint && root.waypoint.bookmark)
                                        root.api.addTag(root.waypoint, v);
                                } else {
                                    root.addingTag = false;
                                }
                                text = "";
                            }
                            Keys.onEscapePressed: {
                                root.addingTag = false;
                                text = "";
                            }
                        }
                    }
                }

                // Focus helper for new tag entry
                Timer {
                    id: tagInputFocus
                    interval: 30
                    repeat: false
                    onTriggered: {
                        if (root.addingTag && tagInput)
                            tagInput.forceActiveFocus();
                    }
                }

                // Auto-refresh tags whenever waypoint changes (bookmark only)
                Component.onCompleted: {
                    if (root.waypoint && root.waypoint.bookmark && root.api) {
                        root.api.fetchTags(root.waypoint);
                    } else {
                        root.waypointTags = (root.waypoint && root.waypoint.tags) ? root.waypoint.tags.slice() : [];
                    }
                }
                Connections {
                    target: root
                    function onWaypointChanged() {
                        root.addingTag = false;
                        if (root.waypoint && root.waypoint.bookmark && root.api)
                            root.api.fetchTags(root.waypoint);
                    }
                }
                // Listen to centralized API signals to keep local view state in sync
                Connections {
                    target: root.api
                    function onTagsFetched(name, lat, lon, tags) {
                        if (!root.waypoint)
                            return;
                        if (root.waypoint.name === name && Math.abs(root.waypoint.lat - lat) < 1e-9 && Math.abs(root.waypoint.lon - lon) < 1e-9) {
                            root.waypoint.tags = tags;
                            root.waypointTags = tags.slice();
                        }
                    }
                    function onTagAdded(name, lat, lon, tags, tag) {
                        onTagsFetched(name, lat, lon, tags);
                    }
                    function onTagDeleted(name, lat, lon, tags, tag) {
                        onTagsFetched(name, lat, lon, tags);
                    }
                    function onWaypointRenamed(updated, original) {
                        if (!root.waypoint)
                            return;
                        if (Math.abs(root.waypoint.lat - original.lat) < 1e-9 && Math.abs(root.waypoint.lon - original.lon) < 1e-9 && root.waypoint.name === original.name) {
                            root.waypoint.name = updated.name;
                            root.tempName = updated.name;
                            root.nameEdited(root.waypoint);
                        }
                    }
                    function onWaypointRenameFailed(original, newName, error) {
                        // No optimistic mutation performed; could show feedback here if desired
                        console.warn("Rename failed:", error);
                    }
                }
            }

            // Coordinates row (if time used as primary, show both lat/lon here)
            ColumnLayout {
                visible: root.waypoint !== null
                spacing: 2

                Text {
                    visible: root.waypoint !== null
                    text: root.waypoint ? ("Lat: " + root.waypoint.lat.toFixed(6) + "   Lon: " + root.waypoint.lon.toFixed(6)) : ""
                    font.pixelSize: theme.scale ? theme.scale(1) : 12
                    font.family: "monospace"
                    color: theme.waypointInfoCard.primaryText
                    wrapMode: Text.NoWrap
                    Layout.fillWidth: true
                }

                // Description (if present)
                Text {
                    visible: !!(root.waypoint && root.waypoint.desc)
                    text: root.waypoint && root.waypoint.desc ? root.waypoint.desc : ""
                    font.pixelSize: theme.scale ? theme.scale(1) : 12
                    color: theme.waypointInfoCard.secondaryText
                    wrapMode: Text.WordWrap
                    Layout.fillWidth: true
                }

                // Original ISO time (small)
                Text {
                    visible: !!root.hasTime()
                    text: root.waypoint && root.waypoint.time ? root.waypoint.time : ""
                    font.pixelSize: theme.scale ? theme.scale(1) : 11
                    color: theme.waypointInfoCard.secondaryText
                    font.family: "monospace"
                    wrapMode: Text.Wrap
                    Layout.fillWidth: true
                }
            }
        }

        // Spacer to pad bottom
        Item {
            Layout.fillWidth: true
            Layout.preferredHeight: 4
        }
    }
}
