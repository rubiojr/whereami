import QtQuick 2.15
import "qrc:/themes" as Themes

/*
  NordPolarTheme.qml
  ------------------
  Refocused on Polar Night (nord0–nord3) + Snow Storm (nord4–nord6) contrast,
  deliberately minimizing Frost (nord7–nord10) usage to differentiate from the
  Nord Frost / Nord Polar Frost variants. Aurora colors only provide semantic
  highlights (selection, destructive, success).
*/

QtObject {
    id: nordThemeRoot

    // Core colors (Polar Night surfaces + Snow Storm text)
    property color background: "#2E3440"          // nord0
    property color darkColor: "#2E3440"
    property color accent: "#D8DEE9"              // nord4 (light neutral used as accent)
    property color primaryText: "#ECEFF4"         // nord6
    property color secondaryText: "#D8DEE9"       // nord4
    property color textColor: nordThemeRoot.primaryText

    // Search box (pure Polar Night, no Frost hints)
    property color searchBoxBackground: "#3B4252"    // nord1
    property color searchBoxText: "#ECEFF4"
    property color searchBoxPlaceholder: "#4C566A"   // nord3 instead of frost cyan
    property color searchBoxBorder: "#4C566A"        // nord3
    property int searchBoxRadius: 10
    property int searchBoxHeight: 40
    property color searchBoxButtonBackground: nordThemeRoot.accent
    property color searchBoxButtonText: "#2E3440"

    property int toolbarButtonSize: 24
    // Qualified access to avoid qmllint unqualified warning
    property int statusBarTextSize: Themes.Fonts.scale(1)
    // Toolbar palette (Nord Polar – restrained, neutral; hover uses subtle gold for differentiation)
    property QtObject toolbar: QtObject {
        property color background: "#2E3440"          // nord0 base
        property color backgroundHover: "#3B4252"     // nord1
        property color border: "#434C5E"              // nord2
        property color separator: "#434C5E"           // same as border for subtle dividers
        property color icon: "#ECEFF4"                // nord6
        property color iconHover: "#EBCB8B"           // aurora gold highlight
        property color iconDisabled: "#4C566A"        // nord3 muted
        property color text: "#ECEFF4"
        property color textSecondary: "#D8DEE9"
    }

    property color waypointInfoCardTitleColor: "#4C566A"   // nord3 header strip
    property color waypointInfoCardTitleTextColor: "#ECEFF4"
    // Qualified access
    property int waypointInfoCardTitleSize: Themes.Fonts.scale(3)
    // Qualified access
    property int waypointInfoCardPrimaryInfoFontSize: Themes.Fonts.scale(3)

    property color bookmarkColor: "#B48EAD"   // retain aurora purple for distinctiveness

    // Marker sizing
    property int waypointMarkerRadius: 8
    property int clusterMarkerRadius: 16
    // Cluster + default markers use Polar Night tones (no Frost blues)
    property color clusterMarkerColor: "#4C566A"      // nord3
    property color waypointDefaultColor: "#434C5E"    // nord2
    property color waypointSelectedColor: "#BF616A"   // nord11 (higher contrast on light map backgrounds)
    property color waypointSelectedHaloColor: Qt.rgba(0.75, 0.38, 0.43, 0.50) // nord11 glow (stronger contrast & visibility)
    property color waypointBorderColor: "white"
    property color currentLocationColor: "#A3BE8C"    // nord14 (success/position)
    property color currentLocationInnerColor: "#ECEFF4"
    property color searchResultColor: nordThemeRoot.accent

    // WaypointInfoCard nested colors
    property QtObject waypointInfoCard: QtObject {
        property color background: "#3B4252"    // nord1
        property color border: "#434C5E"        // nord2
        property color primaryText: "#ECEFF4"
        property color secondaryText: "#D8DEE9"
        property color noTagsText: "#4C566A"

        property QtObject editButton: QtObject {
            // High-contrast aurora gold styling for better separation from header strip
            // Normal state
            property color text: "White"
            //property color background: "#EBCB8B"        // aurora gold (nord13)
            property color background: "#3B4252"        // aurora gold (nord13)
            // Hover state (slightly darker)
            property color textHover: "White"
            property color backgroundHover: "#5D6371"
            // Pressed state (further darkened)
            property color textPressed: "#2E3440"
            property color backgroundPressed: "#C9A660"
            // Borders (subtle delineation)
            property color border: "#3B4252"
            property color borderHover: "#5D6371"
            property color borderPressed: "#C9A660"
        }

        property QtObject cancelButton: QtObject {
            property color text: "white"
            property color background: "#BF616A"
            property color backgroundPressed: "#A54F58"
            property color border: "#A54F58"
        }

        property QtObject bookmarkChip: QtObject {
            property color background: "#B48EAD"
            property color border: "#8F6F8D"
        }

        // GPX chip now neutral (no Frost cyan)
        property QtObject gpxChip: QtObject {
            property color background: "#434C5E"  // nord2
            property color border: "#4C566A"      // nord3
        }

        property color chipText: "#ECEFF4"

        // Time chip neutralized
        property QtObject timeChip: QtObject {
            property color background: "#4C566A"  // nord3
            property color border: "#434C5E"      // nord2
            property color text: "#ECEFF4"
        }

        property QtObject addTagButton: QtObject {
            // Base state (keep subtle light neutral)
            property color text: "#2E3440"
            property color background: "#D8DEE9"          // slightly darker than previous to allow bigger delta
            // Hover state (stronger aurora gold for clear contrast)
            property color textHover: "#2E3440"
            property color backgroundHover: "#EBCB8B"     // aurora gold
            // Pressed state (deepen gold for tactile feedback)
            property color textPressed: "#2E3440"
            property color backgroundPressed: "#C9A660"
            // Border states (provide additional edge contrast)
            property color border: "#D8DEE9"
            property color borderHover: "#EBCB8B"
            property color borderPressed: "#C9A660"
        }

        property QtObject tagChip: QtObject {
            property color background: "#4C566A"
            property color border: "#434C5E"
            property color text: "white"
            property color deleteHover: "#4C566A"
            property color deleteBorderHover: "#4C566A"
        }

        property QtObject tagInput: QtObject {
            property color background: "#2E3440"
            property color text: "white"
            property color placeholder: "#4C566A"
            property color border: "#4C566A"
            property color selection: "#4C566A"
            property color selectedText: "white"
        }

        property QtObject nameField: QtObject {
            property color background: "transparent"
        }
    }

    // SearchBox nested colors (removed Frost highlight)
    property QtObject searchBox: QtObject {
        property color pillBackground: "#3B4252"
        property color pillBorder: "#4C566A"
        property color iconColor: "white"
        property color inputText: "white"
        property color inputBackground: "transparent"

        property color suggestionsBackground: "#2E3440"
        property color suggestionsBorder: "#434C5E"
        property color suggestionRowBorder: "#434C5E"

        property color suggestionText: "#ECEFF4"
        property color suggestionTextHover: "#FFFFFF"
        property color coordText: "#D8DEE9"
        property color coordTextHover: "#E5E9F0"
        property color highlightBackground: "#4C566A"
    }

    // AddWaypointDialog nested colors
    property QtObject addWaypointDialog: QtObject {
        property color background: "#3B4252"
        property color border: "#434C5E"

        property color headerBackground: "#4C566A"
        property color headerBorder: "#434C5E"
        property color headerText: "#ECEFF4"

        property color footerBackground: "#3B4252"
        property color footerBorder: "#434C5E"

        property QtObject button: QtObject {
            property color text: "#ECEFF4"
            property color background: "#4C566A"
            property color backgroundPressed: "#434C5E"
            property color border: "#434C5E"
        }

        property QtObject coordinateLabel: QtObject {
            property color background: "#4C566A"
            property color text: "#ECEFF4"
        }

        property QtObject coordinateValue: QtObject {
            property color background: "#2E3440"
            property color border: "#434C5E"
            property color text: "#ECEFF4"
        }

        property QtObject textField: QtObject {
            property color text: "#ECEFF4"
            property color placeholderText: "#4C566A"
            property color background: "#2E3440"
            property color border: "#4C566A"
            property color selection: "#4C566A"
            property color selectedText: "#ECEFF4"
        }
    }

    // MapControlButton nested colors
    property QtObject mapControlButton: QtObject {
        property color background: "#2E3440"
        property color backgroundHover: "#3B4252"
        property color border: "#434C5E"
        property color text: "white"
        property color textHover: nordThemeRoot.accent
        property QtObject tooltip: QtObject {
            property color background: "#2E3440"
            property color border: "#434C5E"
            property color text: "white"
        }
    }

    // WaypointTable nested colors
    property QtObject waypointTable: QtObject {
        property color background: "#3B4252"
        property color border: "#434C5E"
        property color headerText: "#ECEFF4"
        property color headerSeparator: "#434C5E"
        property color rowAltA: "#343C49"
        property color rowAltB: "#2E3440"
        property color rowSelected: "#4C566A"
        property color rowHover: "#434C5E"
        property color rowBorder: "#434C5E"
        property color nameText: "#ECEFF4"
        property color tagsText: "#D8DEE9"
        property color coordText: "#D8DEE9"
        property color badgeBookmark: "#B48EAD"
        property color separator: "#434C5E"
        property color closeButtonText: "#ECEFF4"
        property color columnHeaderText: "#D8DEE9"
        property color closeButtonBackground: "#434C5E"
        property color closeButtonBackgroundHover: "#4C566A"
        property color closeButtonBorder: "#434C5E"
        property color closeButtonBorderHover: "#4C566A"
    }

    // MapStatusBar colors
    property QtObject mapStatusBar: QtObject {
        property color sectionBackground: "transparent"
    }

    // SnackBar colors (neutral, no Frost)
    property QtObject snackBar: QtObject {
        property color background: "#434C5E"
        property color text: "#ECEFF4"
        property color actionBackground: "#4C566A"
        property color actionText: "#ECEFF4"
    }

    // Utility colors
    property color transparent: "transparent"
    property color white: "white"
}
