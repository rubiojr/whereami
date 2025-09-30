// qmllint disable unqualified
import QtQuick 2.15
import QtQuick.Controls 2.15
import "../themes"

Dialog {
    id: root
    ThemeLoader {
        id: theme
    }

    /*
  AddWaypointDialog.qml

  Simplified reusable Dialog component for adding a waypoint (now with optional tags).

  Behaviour changes:
    - The component no longer mutates a `waypoints` array or performs any XHR.
    - On acceptance it emits `addRequested(var waypoint)` with { name, lat, lon, tags? }.
    - The parent (e.g. MapView.qml) is responsible for updating its waypoint array
      and persisting the waypoint if desired.
    - New tags field lets user enter comma-separated tags (stored as an array).
*/

    width: 320  // Make dialog 20px wider
    height: 300 // Slightly higher to accommodate tags field
    property double lat: 0
    property double lon: 0
    property string titleText: qsTr("Add Waypoint")
    property string presetName: "" // optional prefilled name (e.g. from search result)

    // Expose the inner TextField so parent callers can clear/focus it if needed.
    property alias nameFieldRef: nameField
    property alias tagsFieldRef: tagsField

    // Emitted when the user accepts a non-empty name. Payload: { name, lat, lon, tags? }
    signal addRequested(var waypoint)

    modal: false
    // Use Popup closePolicy to be portable across platforms.
    closePolicy: Popup.CloseOnEscape | Popup.CloseOnPressOutside
    title: titleText
    standardButtons: Dialog.Ok | Dialog.Cancel

    // Custom styling for dialog - semi-transparent background to match WaypointTable
    background: Rectangle {
        color: theme.addWaypointDialog.background
        border.color: theme.addWaypointDialog.border
        border.width: 1
        radius: 5
    }

    // Custom header with DarkOrange background and white text
    header: Rectangle {
        height: 40
        color: theme.addWaypointDialog.headerBackground
        border.color: theme.addWaypointDialog.headerBorder
        border.width: 2
        radius: 5
        Text {
            anchors.centerIn: parent
            text: root.titleText
            color: theme.addWaypointDialog.headerText
            font.bold: true
            font.pixelSize: theme.scale ? theme.scale(6) : 24
        }
    }

    // Custom footer with styled buttons
    footer: DialogButtonBox {
        background: Rectangle {
            color: theme.addWaypointDialog.footerBackground
            border.color: theme.addWaypointDialog.footerBorder
            border.width: 1
            radius: 5
        }

        delegate: Button {
            id: control

            contentItem: Text {
                text: control.text
                font.bold: true
                color: theme.addWaypointDialog.button.text
                horizontalAlignment: Text.AlignHCenter
                verticalAlignment: Text.AlignVCenter
            }

            background: Rectangle {
                implicitWidth: 80
                implicitHeight: 36
                color: control.down ? theme.addWaypointDialog.button.backgroundPressed : theme.addWaypointDialog.button.background
                border.color: theme.addWaypointDialog.button.border
                radius: 5
            }
        }
    }

    // Focus the name field when shown and reset its text + clear tags
    onAboutToShow: {
        if (typeof nameField !== 'undefined') {
            nameField.text = (root.presetName && root.presetName.length > 0) ? root.presetName : "";
            nameField.forceActiveFocus();
            nameField.selectAll();
        }
        if (typeof tagsField !== 'undefined') {
            tagsField.text = "";
        }
    }

    onAccepted: {
        var name = "";
        if (typeof nameField !== 'undefined')
            name = nameField.text.trim();
        if (name === "")
            return;

        // Parse tags (comma separated)
        var tagsArr = [];
        if (typeof tagsField !== 'undefined' && tagsField.text.trim().length > 0) {
            var raw = tagsField.text.split(",");
            for (var i = 0; i < raw.length; i++) {
                var t = raw[i].trim();
                if (t.length === 0)
                    continue;
                // Keep tag verbatim (backend does any enrichment/emoji mapping)
                if (tagsArr.indexOf(t) === -1) {
                    tagsArr.push(t);
                }
            }
        }

        var wp = {
            name: name,
            lat: lat,
            lon: lon
        };
        if (tagsArr.length > 0)
            wp.tags = tagsArr;

        addRequested(wp);
        root.close();
    }

    contentItem: Column {
        spacing: 14
        padding: 10
        // Show coordinates using 6 decimal places like the original inline dialog.
        Row {
            spacing: 5
            Rectangle {
                width: 90
                height: 25
                color: theme.addWaypointDialog.coordinateLabel.background
                radius: 5
                Text {
                    padding: 14
                    anchors.centerIn: parent
                    text: "Latitude"
                    color: theme.addWaypointDialog.coordinateLabel.text
                    font.bold: true
                }
            }
            Rectangle {
                width: 120
                height: 25
                color: theme.addWaypointDialog.coordinateValue.background
                border.color: theme.addWaypointDialog.coordinateValue.border
                border.width: 1
                radius: 3
                Text {
                    anchors.centerIn: parent
                    text: root.lat.toFixed(6)
                    color: theme.addWaypointDialog.coordinateValue.text
                }
            }
        }
        Row {
            spacing: 5
            Rectangle {
                width: 90
                height: 25
                color: theme.addWaypointDialog.coordinateLabel.background
                radius: 5
                Text {
                    padding: 14
                    anchors.centerIn: parent
                    text: "Longitude"
                    color: theme.addWaypointDialog.coordinateLabel.text
                    font.bold: true
                }
            }
            Rectangle {
                width: 120
                height: 25
                color: theme.addWaypointDialog.coordinateValue.background
                border.color: theme.addWaypointDialog.coordinateValue.border
                border.width: 1
                radius: 3
                Text {
                    anchors.centerIn: parent
                    text: root.lon.toFixed(6)
                    color: theme.addWaypointDialog.coordinateValue.text
                }
            }
        }
        TextField {
            id: nameField
            width: parent.width - 20  // Make text field use full available width
            height: 40
            placeholderText: qsTr("Waypoint name...")
            focus: true
            onAccepted: root.accept()
            Component.onCompleted: text = ""

            // Style the text field to match the dialog theme
            color: theme.addWaypointDialog.textField.text
            placeholderTextColor: theme.addWaypointDialog.textField.placeholderText
            selectionColor: theme.addWaypointDialog.textField.selection
            selectedTextColor: theme.addWaypointDialog.textField.selectedText
            background: Rectangle {
                color: theme.addWaypointDialog.textField.background
                border.color: theme.addWaypointDialog.textField.border
                border.width: 1
                radius: 5
            }
        }

        // New tags input
        TextField {
            id: tagsField
            width: parent.width - 20
            height: 34
            placeholderText: qsTr("Tags (comma separated)")
            color: theme.addWaypointDialog.textField.text
            placeholderTextColor: theme.addWaypointDialog.textField.placeholderText
            selectionColor: theme.addWaypointDialog.textField.selection
            selectedTextColor: theme.addWaypointDialog.textField.selectedText
            onAccepted: root.accept()
            onTextChanged:
            // No client-side emoji translation; tags are stored verbatim
            {}
            background: Rectangle {
                color: theme.addWaypointDialog.textField.background
                border.color: theme.addWaypointDialog.textField.border
                border.width: 1
                radius: 5
            }
        }
    }

    // Helper method to open the dialog at a clamped position. Parent can call:
    // addWaypointDialog.openAt(mouseX + 10, mouseY + 10)
    function openAt(px, py) {
        var targetX = px;
        var targetY = py;
        if (typeof window !== 'undefined' && root.width > 0 && root.height > 0) {
            targetX = Math.min(window.width - root.width - 20, px);
            targetY = Math.min(window.height - root.height - 20, py);
        }
        x = targetX;
        y = targetY;
        if (typeof nameField !== 'undefined') {
            nameField.text = (root.presetName && root.presetName.length > 0) ? root.presetName : "";
            nameField.selectAll();
        }
        if (typeof tagsField !== 'undefined') {
            tagsField.text = "";
        }
        root.open();
    }
}
