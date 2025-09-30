import QtQuick 2.15
import QtQuick.Controls 2.15
import "../themes"

ToolTip {
    id: customToolTip

    // Public properties
    property string tooltipText: ""
    property string position: "left" // "left", "right", "top", "bottom"

    ThemeLoader {
        id: theme
    }

    text: tooltipText
    delay: 800
    timeout: 1500

    // Position the tooltip relative to parent
    function updatePosition() {
        if (!parent)
            return;

        switch (position) {
        case "left":
            x = -width - 5;
            y = (parent.height - height) / 2;
            break;
        case "right":
            x = parent.width + 5;
            y = (parent.height - height) / 2;
            break;
        case "top":
            x = (parent.width - width) / 2;
            y = -height - 5;
            break;
        case "bottom":
            x = (parent.width - width) / 2;
            y = parent.height + 5;
            break;
        }
    }

    onVisibleChanged: {
        if (visible) {
            updatePosition();
        }
    }

    Component.onCompleted: {
        updatePosition();
    }

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
        font.pixelSize: theme.scale ? theme.scale(1) : 12
        horizontalAlignment: Text.AlignHCenter
        verticalAlignment: Text.AlignVCenter
    }
}
