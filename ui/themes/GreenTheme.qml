import QtQuick 2.15
import "qrc:/themes" as Themes

QtObject {
    id: greenThemeRoot
    // Green theme - nature inspired color palette

    property color background: "#1B2B1B"
    property color accent: "#4CAF50"
    property color primaryText: "#66BB6A"
    property color secondaryText: "#A5D6A7"
    property color darkColor: "#1B2B1B"
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
    property color searchBoxButtonBackground: greenThemeRoot.accent
    property color searchBoxButtonText: "White"

    property int toolbarButtonSize: 32
    property int statusBarTextSize: Themes.Fonts.scale(1)
    // Toolbar palette (Green theme)
    property QtObject toolbar: QtObject {
        // Accent-derived opaque background (darker base, slightly lighter hover)
        property color background: Qt.darker(greenThemeRoot.accent, 2.2)
        property color backgroundHover: Qt.darker(greenThemeRoot.accent, 1.8)
        property color border: "#388E3C"
        property color separator: "#2E7D32"
        property color icon: "white"
        property color iconHover: greenThemeRoot.accent
        property color iconDisabled: "#557755"
        property color text: greenThemeRoot.primaryText
        property color textSecondary: greenThemeRoot.secondaryText
    }

    property color waypointInfoCardTitleColor: greenThemeRoot.accent
    property color waypointInfoCardTitleTextColor: greenThemeRoot.background
    property int waypointInfoCardTitleSize: Themes.Fonts.scale(3)
    property int waypointInfoCardPrimaryInfoFontSize: Themes.Fonts.scale(3)

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
            // Normal state
            property color text: Qt.darker("DeepOrange", 1.3)
            property color background: "White"
            // Hover state
            property color textHover: Qt.darker("DeepOrange", 1.15)
            property color backgroundHover: Qt.darker("White", 1.05)
            // Pressed state
            property color textPressed: Qt.darker("DeepOrange", 1.3)
            property color backgroundPressed: Qt.darker("White", 1.15)
            // Border states (can be used by button background if desired)
            property color border: Qt.darker("DeepOrange", 1.2)
            property color borderHover: Qt.darker("DeepOrange", 1.3)
            property color borderPressed: Qt.darker("DeepOrange", 1.4)
        }

        property QtObject cancelButton: QtObject {
            property color text: "white"
            property color background: "#388E3C"
            property color backgroundPressed: Qt.darker("#388E3C", 1.2)
            property color border: Qt.darker("#388E3C", 1.3)
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
            property color text: "white"
            property color background: "#4CAF50"
            // Hover state
            property color textHover: "white"
            property color backgroundHover: "#57E389"   // lighter (nord-style success lift)
            // Pressed state
            property color textPressed: "white"
            property color backgroundPressed: "#388E3C" // darker green for press feedback
            // Border states (optional use by components)
            property color border: "#388E3C"
            property color borderHover: "#4CAF50"
            property color borderPressed: "#2E7030"
        }

        // Tag chips
        property QtObject tagChip: QtObject {
            property color background: "#66BB6A"
            property color border: "#4CAF50"
            property color text: "white"
            property color deleteHover: "#81C784"
            property color deleteBorderHover: "#4CAF50"
        }

        // Tag input field
        property QtObject tagInput: QtObject {
            property color background: "white"
            property color text: "black"
            property color placeholder: "#555"
            property color border: "#555"
            property color selection: greenThemeRoot.accent
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
        property color coordTextHover: "#C8E6C9"
        property color highlightBackground: "#4CAF50"
    }

    // AddWaypointDialog nested colors
    property QtObject addWaypointDialog: QtObject {
        // Main dialog
        property color background: Qt.rgba(0, 0, 0, 0.82)
        property color border: "#444"

        // Header
        property color headerBackground: "#4CAF50"
        property color headerBorder: "#444"
        property color headerText: "white"

        // Footer
        property color footerBackground: Qt.rgba(0, 0, 0, 0.82)
        property color footerBorder: "#444"

        // Buttons
        property QtObject button: QtObject {
            property color text: "white"
            property color background: "#4CAF50"
            property color backgroundPressed: Qt.darker("#4CAF50", 1.2)
            property color border: Qt.darker("#4CAF50", 1.3)
        }

        // Coordinate labels
        property QtObject coordinateLabel: QtObject {
            property color background: "#4CAF50"
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
            property color border: "#4CAF50"
            property color selection: greenThemeRoot.accent
            property color selectedText: "white"
        }
    }

    // MapControlButton nested colors
    property QtObject mapControlButton: QtObject {
        property color background: Qt.rgba(0, 0, 0, 0.6)
        property color backgroundHover: Qt.rgba(0, 0, 0, 0.75)
        property color border: "#444"
        property color text: "white"
        property color textHover: greenThemeRoot.accent

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
        property color rowSelected: "#4CAF50"
        property color rowHover: "#333333"
        property color rowBorder: "#383838"
        property color nameText: "white"
        property color tagsText: "#A5D6A7"
        property color coordText: "#C8E6C9"
        property color badgeBookmark: "#66BB6A"
        property color separator: "#484848"
        property color closeButtonText: "white"
        property color columnHeaderText: "#C8E6C9"
        // Close button styling (moved from component usage into theme)
        property color closeButtonBackground: "#555555"
        property color closeButtonBackgroundHover: "#4CAF50"
        property color closeButtonBorder: "#666666"
        property color closeButtonBorderHover: "#4CAF50"
    }

    // MapStatusBar colors
    property QtObject mapStatusBar: QtObject {
        property color sectionBackground: "transparent"
    }

    // SnackBar colors
    property QtObject snackBar: QtObject {
        property color background: "#2E7D32"
        property color text: "white"
        property color actionBackground: "#66BB6A"
        property color actionText: "white"
    }

    // General UI colors
    property color transparent: "transparent"
    property color white: "white"

    property int waypointMarkerRadius: 8
    property int clusterMarkerRadius: 16
    property color clusterMarkerColor: "#2E7D32"
    // Map marker colors (moved from MapView hard-coded literals)
    property color waypointDefaultColor: "#2196F3"
    property color waypointSelectedColor: "#FF1744"
    property color waypointSelectedHaloColor: Qt.rgba(1, 0.09, 0.27, 0.35)
    property color waypointBorderColor: "white"
    property color currentLocationColor: "#1976D2"
    property color currentLocationInnerColor: "white"
    property color searchResultColor: greenThemeRoot.accent
}
