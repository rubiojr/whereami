import QtQuick 2.15
import QtPositioning 6.5

QtObject {
    property int searchZoomLevel: 17
    property int initialZoomLevel: 2
    // Centralized initial map position (moved from MapView center binding)
    property var initialPosition: QtPositioning.coordinate(40.7128, 0)
}
