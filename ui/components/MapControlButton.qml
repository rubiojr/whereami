import QtQuick 2.15
import QtQuick.Controls 2.15
import "../themes"

Button {
    id: control
    property int buttonSize: 48
    width: buttonSize
    height: buttonSize

    // Public property for tooltip text
    property string tooltipText: ""

    ThemeLoader {
        id: theme
    }

    // Use theme scaling (fallback to constant if theme.scale unavailable)
    font.pixelSize: theme && theme.scale ? theme.scale(4) : 20
    font.bold: true

    contentItem: Text {
        text: control.text
        font: control.font
        color: mouseArea.containsMouse ? theme.mapControlButton.textHover : theme.mapControlButton.text
        horizontalAlignment: Text.AlignHCenter
        verticalAlignment: Text.AlignVCenter
    }

    background: Rectangle {
        radius: control.buttonSize / 2  // Perfectly circular
        color: mouseArea.containsMouse ? theme.mapControlButton.backgroundHover : theme.mapControlButton.background
        border.color: theme.mapControlButton.border
        border.width: 1
    }

    // Hover effect
    MouseArea {
        id: mouseArea
        anchors.fill: parent
        hoverEnabled: true
        cursorShape: Qt.PointingHandCursor
        onPressed: function (mouse) {
            mouse.accepted = false;
        }
    }

    scale: mouseArea.containsMouse ? 1.1 : 1.0

    Behavior on scale {
        NumberAnimation {
            duration: 150
            easing.type: Easing.OutQuad
        }
    }

    // Tooltip
    ToolTip {
        parent: control
        visible: mouseArea.containsMouse && tooltipText.length > 0
        text: tooltipText
        delay: 800
        timeout: 1500
        x: -width - 5
        y: (control.height - height) / 2

        background: Rectangle {
            color: theme.mapControlButton.tooltip.background
            border.color: theme.mapControlButton.tooltip.border
            border.width: 1
            radius: 6
            implicitWidth: contentText.implicitWidth + 16
            implicitHeight: contentText.implicitHeight + 12
        }

        contentItem: Text {
            id: contentText
            text: tooltipText
            color: theme.mapControlButton.tooltip.text
            font.pixelSize: theme && theme.scale ? theme.scale(1) : 12
            horizontalAlignment: Text.AlignHCenter
            verticalAlignment: Text.AlignVCenter
        }
    }
}
