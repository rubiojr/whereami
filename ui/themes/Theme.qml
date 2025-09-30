import QtQuick 2.15
import "qrc:/themes" as Themes

QtObject {
    id: themeRoot
    // Centralized theme values for the app. Use these instead of hard-coded colors
    // so components adapt to the selected Controls style / configuration.
    property color background: "#222226"
    property color accent: "#F97700"
    property color primaryText: "#FF8226"
    property color secondaryText: "#BDBDBD"
    property color darkColor: "#222226"
    property color textColor: primaryText

    // Search box styling (prefixed with searchBox to avoid collisions)
    // Designed to look like a search engine input: rounded, white background.
    property color searchBoxBackground: "#FFFFFF"
    property color searchBoxText: "#000000"
    property color searchBoxPlaceholder: "#777777"
    property color searchBoxBorder: "#E6E6E6"
    property int searchBoxRadius: 10
    property int searchBoxHeight: 40
    // Button / accent used inside the search control (e.g. Search button, icons)
    property color searchBoxButtonBackground: accent
    property color searchBoxButtonText: "White"

    property int toolbarButtonSize: 24
    property int statusBarTextSize: 14

    // Toolbar palette (consumed by MapToolBar / future toolbar components)
    // Centralizes colors instead of hardcoding in QML files.
    property QtObject toolbar: QtObject {
        // Opaque accent-derived toolbar surfaces (moved from semi-transparent black)
        // Darker factor first (resting), slightly less dark on hover for subtle lift.
        property color background: Qt.darker(themeRoot.accent, 2.2)
        property color backgroundHover: Qt.darker(themeRoot.accent, 1.8)
        property color border: "#444"
        property color separator: "#555"
        property color icon: "white"
        property color iconHover: themeRoot.accent
        property color iconDisabled: "#666666"
        property color text: themeRoot.primaryText
        property color textSecondary: themeRoot.secondaryText
    }

    property color waypointInfoCardTitleColor: themeRoot.accent
    property color waypointInfoCardTitleTextColor: themeRoot.background
    property int waypointInfoCardTitleSize: Themes.Fonts.scale(3, minFontSize, fontScaleRatio)
    property int waypointInfoCardPrimaryInfoFontSize: Themes.Fonts.scale(3, minFontSize, fontScaleRatio)

    property color bookmarkColor: "#9C27B0"

    // WaypointInfoCard nested colors
    property QtObject waypointInfoCard: QtObject {
        // Main card colors
        property color background: Qt.rgba(0, 0, 0, 0.82)
        property color border: "#444"
        property color primaryText: "white"
        property color secondaryText: "white"
        property color noTagsText: "#CCCCCC"

        // Edit buttons
        property QtObject editButton: QtObject {
            // Base (normal) state
            property color text: Qt.darker("DeepOrange", 1.3)
            property color background: "White"
            // Added interactive states for improved contrast & affordance
            property color textHover: Qt.darker("DeepOrange", 1.15)
            property color textPressed: Qt.darker("DeepOrange", 1.3)
            property color backgroundHover: Qt.darker("White", 1.05)
            property color backgroundPressed: Qt.darker("White", 1.15)
            property color border: "#F97700"
            property color borderHover: Qt.darker("#F97700", 1.1)
            property color borderPressed: Qt.darker("#F97700", 1.25)
        }

        property QtObject cancelButton: QtObject {
            property color text: "white"
            property color background: "DarkOrange"
            property color backgroundPressed: Qt.darker("DarkOrange", 1.2)
            property color border: Qt.darker("DarkOrange", 1.3)
        }

        // Source chips (BOOKMARK/GPX)
        property QtObject bookmarkChip: QtObject {
            property color background: "#C8E6C9"
            property color border: "#81C784"
        }

        property QtObject gpxChip: QtObject {
            property color background: "#E0E7FF"
            property color border: "#94A3FF"
        }

        property color chipText: "#2b2f33"

        // UTC/Time chip
        property QtObject timeChip: QtObject {
            property color background: "#C8E6C9"
            property color border: "#A5D6A7"
            property color text: "Black"
        }

        // Add tag button
        property QtObject addTagButton: QtObject {
            // Base state
            property color text: "Black"
            property color background: "DarkOrange"
            // Hover state
            property color textHover: "Black"
            property color backgroundHover: Qt.darker("DarkOrange", 0.9)
            // Pressed state
            property color textPressed: "Black"
            property color backgroundPressed: Qt.darker("DarkOrange", 1.2)
            // Border states (optional – components can start using these)
            property color border: Qt.darker("DarkOrange", 1.1)
            property color borderHover: Qt.darker("DarkOrange", 1.25)
            property color borderPressed: Qt.darker("DarkOrange", 1.4)
        }

        // Tag chips
        property QtObject tagChip: QtObject {
            property color background: "#C25AD2"
            property color border: "#8E24AA"
            property color text: "white"
            property color deleteHover: "#B67CCF"
            property color deleteBorderHover: "#8E24AA"
        }

        // Tag input field
        property QtObject tagInput: QtObject {
            property color background: "white"
            property color text: "black"
            property color placeholder: "#555"
            property color border: "#555"
            property color selection: themeRoot.accent
            property color selectedText: "white"
        }

        // Name field
        property QtObject nameField: QtObject {
            property color background: "transparent"
        }
    }

    // SearchBox nested colors
    property QtObject searchBox: QtObject {
        // Input pill
        property color pillBackground: Qt.rgba(0, 0, 0, 0.82)
        property color pillBorder: "#444"
        property color iconColor: "white"
        property color inputText: "white"
        property color inputBackground: "transparent"

        // Suggestions panel
        property color suggestionsBackground: Qt.rgba(0, 0, 0, 0.82)
        property color suggestionsBorder: "#444"
        property color suggestionRowBorder: "#555"

        // Suggestion items
        property color suggestionText: "white"
        property color suggestionTextHover: "#FFFFFF"
        property color coordText: "#CCCCCC"
        property color coordTextHover: "#FFE5CC"
        property color highlightBackground: "#DF5F00"
    }

    // AddWaypointDialog nested colors
    property QtObject addWaypointDialog: QtObject {
        // Main dialog
        property color background: Qt.rgba(0, 0, 0, 0.82)
        property color border: "#444"

        // Header
        property color headerBackground: "DarkOrange"
        property color headerBorder: "#444"
        property color headerText: "white"

        // Footer
        property color footerBackground: Qt.rgba(0, 0, 0, 0.82)
        property color footerBorder: "#444"

        // Buttons
        property QtObject button: QtObject {
            property color text: "white"
            property color background: "DarkOrange"
            property color backgroundPressed: Qt.darker("DarkOrange", 1.2)
            property color border: Qt.darker("DarkOrange", 1.3)
        }

        // Coordinate labels
        property QtObject coordinateLabel: QtObject {
            property color background: "DarkOrange"
            property color text: "white"
        }

        // Coordinate values
        property QtObject coordinateValue: QtObject {
            property color background: Qt.rgba(255, 255, 255, 0.1)
            property color border: "#666"
            property color text: "white"
        }

        // Text fields
        property QtObject textField: QtObject {
            property color text: "white"
            property color placeholderText: "#CCCCCC"
            property color background: Qt.rgba(255, 255, 255, 0.1)
            property color border: "#F97700"
            property color selection: themeRoot.accent
            property color selectedText: "white"
        }
    }

    // MapControlButton nested colors
    property QtObject mapControlButton: QtObject {
        property color background: Qt.rgba(0, 0, 0, 0.75)
        property color backgroundHover: Qt.rgba(0, 0, 0, 0.85)
        property color border: "#444"
        property color text: "white"
        property color textHover: themeRoot.accent

        // Tooltip colors
        property QtObject tooltip: QtObject {
            property color background: Qt.rgba(0, 0, 0, 0.9)
            property color border: "#555"
            property color text: "white"
        }
    }

    // WaypointTable nested colors
    property QtObject waypointTable: QtObject {
        property color background: Qt.rgba(0, 0, 0, 0.82)
        property color border: "#444"
        property color headerText: "white"
        property color headerSeparator: "#555"
        property color rowAltA: "#1F1F1F"
        property color rowAltB: "#252525"
        property color rowSelected: "#CA6100"
        property color rowHover: "#333333"
        property color rowBorder: "#383838"
        property color nameText: "white"
        property color tagsText: "#BBBBBB"
        property color coordText: "#CCCCCC"
        property color badgeBookmark: "gold"
        property color separator: "#484848"
        property color closeButtonText: "white"
        property color columnHeaderText: "#CCCCCC"
        // Button styling (added for close button + future table buttons)
        property color closeButtonBackground: "#555555"
        property color closeButtonBackgroundHover: "#CA6100"
        property color closeButtonBorder: "#666666"
        property color closeButtonBorderHover: "#CA6100"
    }

    // MapStatusBar colors
    property QtObject mapStatusBar: QtObject {
        property color sectionBackground: "transparent"
    }

    // SnackBar colors
    property QtObject snackBar: QtObject {
        property color background: "#323232"
        property color text: "white"
        property color actionBackground: "#F57C00"
        property color actionText: "white"
    }

    // General UI colors
    property color transparent: "transparent"
    property color white: "white"

    property int waypointMarkerRadius: 8
    property int clusterMarkerRadius: 16
    property color clusterMarkerColor: "#DF5F00"

    // Map marker colors (consumed by MapView; eliminates hard-coded literals there)
    // Normal (non-bookmark) waypoint dot
    property color waypointDefaultColor: "#2196F3"
    // Selected waypoint highlight
    property color waypointSelectedColor: "#FF1744"
    // Halo around selected waypoint (semi-transparent glow)
    property color waypointSelectedHaloColor: Qt.rgba(1, 0.09, 0.27, 0.35)
    // Shared border color for markers
    property color waypointBorderColor: "white"
    // Current GPS/location marker outer circle
    property color currentLocationColor: "#1976D2"
    // Current GPS/location inner dot
    property color currentLocationInnerColor: "white"
    // Search result (pulsing) marker color
    property color searchResultColor: accent
}
