import QtQuick 2.15
import "qrc:/themes" as Themes

QtObject {
    id: purpleThemeRoot
    // Purple theme - elegant and sophisticated color palette
    // Global minimum font size & modular scale
    // Usage: font.pixelSize = purpleThemeRoot.scale(1|2|3...)
    // Explicit typography knobs (avoid undefined -> int in ThemeLoader)
    // minFontSize and fontScaleRatio inherited from Fonts singleton via ThemeLoader

    property color background: "#2A1B2A"
    property color accent: "#9C27B0"
    property color primaryText: "#CE93D8"
    property color secondaryText: "#E1BEE7"
    property color darkColor: "#2A1B2A"
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
    property color searchBoxButtonBackground: purpleThemeRoot.accent
    property color searchBoxButtonText: "White"

    property int toolbarButtonSize: 32
    property int statusBarTextSize: Themes.Fonts.scale(1)
    // Toolbar palette (Purple theme)
    property QtObject toolbar: QtObject {
        property color background: Qt.darker(purpleThemeRoot.accent, 2.2)
        property color backgroundHover: Qt.darker(purpleThemeRoot.accent, 1.8)
        property color border: "#613583"          // deep purple (nord15-like)
        property color separator: "#813d9c"       // lighter purple for separators
        property color icon: "white"
        property color iconHover: purpleThemeRoot.accent
        property color iconDisabled: "#6e5a74"
        property color text: purpleThemeRoot.primaryText
        property color textSecondary: purpleThemeRoot.secondaryText
    }

    property color waypointInfoCardTitleColor: purpleThemeRoot.accent
    property color waypointInfoCardTitleTextColor: background
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
            // Base (normal) state
            property color text: Qt.darker("DeepOrange", 1.3)
            property color background: "White"
            // Hover state
            property color textHover: Qt.darker("DeepOrange", 1.15)
            property color backgroundHover: Qt.darker("White", 1.05)
            // Pressed state
            property color textPressed: Qt.darker("DeepOrange", 1.3)
            property color backgroundPressed: Qt.darker("White", 1.15)
            // Border states (can be used by button background for clearer affordance)
            property color border: Qt.darker("DeepOrange", 1.2)
            property color borderHover: Qt.darker("DeepOrange", 1.3)
            property color borderPressed: Qt.darker("DeepOrange", 1.4)
        }

        property QtObject cancelButton: QtObject {
            property color text: "white"
            property color background: "#7B1FA2"
            property color backgroundPressed: Qt.darker("#7B1FA2", 1.2)
            property color border: Qt.darker("#7B1FA2", 1.3)
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
            property color background: "#9C27B0"
            // Hover state
            property color textHover: "white"
            property color backgroundHover: "#AB47BC"   // lighter purple
            // Pressed state
            property color textPressed: "white"
            property color backgroundPressed: "#7B1FA2" // darker purple for press feedback
            // Border states (optional for components to use)
            property color border: "#7B1FA2"
            property color borderHover: "#9C27B0"
            property color borderPressed: "#6A1B99"
        }

        // Tag chips
        property QtObject tagChip: QtObject {
            property color background: "#AB47BC"
            property color border: "#9C27B0"
            property color text: "white"
            property color deleteHover: "#CE93D8"
            property color deleteBorderHover: "#9C27B0"
        }

        // Tag input field
        property QtObject tagInput: QtObject {
            property color background: "white"
            property color text: "black"
            property color placeholder: "#555"
            property color border: "#555"
            property color selection: purpleThemeRoot.accent
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
        property color coordTextHover: "#F3E5F5"
        property color highlightBackground: "#9C27B0"
    }

    // AddWaypointDialog nested colors
    property QtObject addWaypointDialog: QtObject {
        // Main dialog
        property color background: Qt.rgba(0, 0, 0, 0.82)
        property color border: "#444"

        // Header
        property color headerBackground: "#9C27B0"
        property color headerBorder: "#444"
        property color headerText: "white"

        // Footer
        property color footerBackground: Qt.rgba(0, 0, 0, 0.82)
        property color footerBorder: "#444"

        // Buttons
        property QtObject button: QtObject {
            property color text: "white"
            property color background: "#9C27B0"
            property color backgroundPressed: Qt.darker("#9C27B0", 1.2)
            property color border: Qt.darker("#9C27B0", 1.3)
        }

        // Coordinate labels
        property QtObject coordinateLabel: QtObject {
            property color background: "#9C27B0"
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
            property color border: "#9C27B0"
            property color selection: purpleThemeRoot.accent
            property color selectedText: "white"
        }
    }

    // MapControlButton nested colors
    property QtObject mapControlButton: QtObject {
        property color background: Qt.rgba(0, 0, 0, 0.6)
        property color backgroundHover: Qt.rgba(0, 0, 0, 0.75)
        property color border: "#444"
        property color text: "white"
        property color textHover: purpleThemeRoot.accent

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
        property color rowSelected: "#9C27B0"
        property color rowHover: "#333333"
        property color rowBorder: "#383838"
        property color nameText: "white"
        property color tagsText: "#E1BEE7"
        property color coordText: "#F3E5F5"
        property color badgeBookmark: "#CE93D8"
        property color separator: "#484848"
        property color closeButtonText: "white"
        property color columnHeaderText: "#F3E5F5"
        property color closeButtonBackground: "#555555"
        property color closeButtonBackgroundHover: purpleThemeRoot.accent
        property color closeButtonBorder: "#666666"
        property color closeButtonBorderHover: purpleThemeRoot.accent
    }

    // MapStatusBar colors
    property QtObject mapStatusBar: QtObject {
        property color sectionBackground: "transparent"
    }

    // SnackBar colors
    property QtObject snackBar: QtObject {
        property color background: "#7B1FA2"
        property color text: "white"
        property color actionBackground: "#9C5FB6"
        property color actionText: "white"
    }

    // General UI colors
    property color transparent: "transparent"
    property color white: "white"

    property int waypointMarkerRadius: 8
    property int clusterMarkerRadius: 16
    property color clusterMarkerColor: "#7B1FA2"
    // Map marker colors (moved from MapView hard-coded literals)
    property color waypointDefaultColor: "#2196F3"
    property color waypointSelectedColor: "#FF1744"
    property color waypointSelectedHaloColor: Qt.rgba(1, 0.09, 0.27, 0.35)
    property color waypointBorderColor: "white"
    property color currentLocationColor: "#1976D2"
    property color currentLocationInnerColor: "white"
    property color searchResultColor: purpleThemeRoot.accent
}
