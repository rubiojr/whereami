import QtQuick 2.15
import "qrc:/themes" as Themes

/*
  PolarFrostTheme.qml
  -------------------
  Nord “Frost‑forward” variant emphasizing the cool cyan/blue Frost range
  (nord7–nord10) while retaining dark Polar Night backgrounds for contrast.

  Nord reference:
    Polar Night:  nord0 #2E3440  nord1 #3B4252  nord2 #434C5E  nord3 #4C566A
    Snow Storm:   nord4 #D8DEE9  nord5 #E5E9F0  nord6 #ECEFF4
    Frost:        nord7 #8FBCBB  nord8 #88C0D0  nord9 #81A1C1  nord10 #5E81AC
    Aurora:       nord11 #BF616A nord12 #D08770 nord13 #EBCB8B nord14 #A3BE8C nord15 #B48EAD

  Mapping rationale:
    accent (primary interactive / highlight text)  -> nord7 (#8FBCBB) softer teal
    clusterMarkerColor (prominent grouped marker)  -> nord10 (#5E81AC) deeper blue
    waypointDefaultColor                           -> accent (nord7)
    waypointSelectedColor                          -> nord11 (#BF616A) (high contrast selection)
    bookmarkColor                                  -> nord15 (#B48EAD) stays distinctive
    currentLocationColor                           -> nord14 (#A3BE8C) (semantic “success/position”)
    searchResultColor                              -> nord8 (#88C0D0) slightly brighter than accent
    Surfaces: background/darkColor                 -> nord1 (#3B4252) (slightly lighter than nord0)
                 elevated cards                    -> nord2 / nord3 mixes
*/

QtObject {
    id: polarFrostThemeRoot

    // Core base colors
    property color background: "#3B4252"        // nord1
    property color darkColor: "#2E3440"         // nord0
    property color accent: "#8FBCBB"            // nord7
    property color primaryText: "#ECEFF4"       // nord6
    property color secondaryText: "#D8DEE9"     // nord4
    property color textColor: primaryText

    // Search box styling (cool dark pill)
    property color searchBoxBackground: "#2E3440"   // slightly deeper
    property color searchBoxText: "#ECEFF4"
    property color searchBoxPlaceholder: "#81A1C1"  // nord9
    property color searchBoxBorder: "#4C566A"       // nord3
    property int searchBoxRadius: 10
    property int searchBoxHeight: 40
    property color searchBoxButtonBackground: polarFrostThemeRoot.accent
    property color searchBoxButtonText: "white"

    property int toolbarButtonSize: 32
    property int statusBarTextSize: Themes.Fonts.scale(1)
    // Toolbar palette (Nord Frost – uses Frost blues for hover emphasis)
    property QtObject toolbar: QtObject {
        property color background: "#3B4252"       // nord1
        property color backgroundHover: "#434C5E"  // nord2
        property color border: "#4C566A"           // nord3
        property color separator: "#4C566A"
        property color icon: "#ECEFF4"             // nord6
        property color iconHover: polarFrostThemeRoot.accent           // accent (nord7–nord8 range)
        property color iconDisabled: "#4C566A"     // muted
        property color text: polarFrostThemeRoot.primaryText
        property color textSecondary: polarFrostThemeRoot.secondaryText
    }

    property color waypointInfoCardTitleColor: polarFrostThemeRoot.accent
    property color waypointInfoCardTitleTextColor: background
    property int waypointInfoCardTitleSize: Themes.Fonts.scale(3)
    property int waypointInfoCardPrimaryInfoFontSize: Themes.Fonts.scale(3)

    // Bookmark distinctive color
    property color bookmarkColor: "#B48EAD"   // nord15

    // Marker sizing
    property int waypointMarkerRadius: 8
    property int clusterMarkerRadius: 16

    // Marker palette
    property color clusterMarkerColor: "#5E81AC"    // nord10
    property color waypointDefaultColor: polarFrostThemeRoot.accent
    property color waypointSelectedColor: "#BF616A" // nord11
    property color waypointSelectedHaloColor: Qt.rgba(0.75, 0.38, 0.43, 0.35) // nord11 soft halo
    property color waypointBorderColor: "white"
    property color currentLocationColor: "#A3BE8C"  // nord14
    property color currentLocationInnerColor: "#ECEFF4"
    property color searchResultColor: "#88C0D0"     // nord8 (a touch brighter than accent)

    // Waypoint Info Card
    property QtObject waypointInfoCard: QtObject {
        property color background: "#434C5E"   // nord2
        property color border: "#4C566A"       // nord3
        property color primaryText: "#ECEFF4"
        property color secondaryText: "#D8DEE9"
        property color noTagsText: "#81A1C1"

        property QtObject editButton: QtObject {
            property color text: "#2E3440"
            property color background: "#E5E9F0"  // nord5
            property color textHover: "#2E3440"
            property color textPressed: "#2E3440"
            property color backgroundHover: "#D8DEE9"  // nord4
            property color backgroundPressed: "#C7CED7" // darkened nord4
            property color border: "#5E81AC"            // nord10 accent border for focus
            property color borderHover: "#88C0D0"       // nord8 lighter border hover
            property color borderPressed: "#4C566A"     // nord3 pressed
        }
        property QtObject cancelButton: QtObject {
            property color text: "white"
            property color background: "#BF616A"
            property color backgroundPressed: "#A54F58"
            property color border: "#A54F58"
        }

        property QtObject bookmarkChip: QtObject {
            property color background: "#B48EAD"
            property color border: "#926F95"
        }
        property QtObject gpxChip: QtObject {
            property color background: "#81A1C1"
            property color border: "#5E81AC"
        }
        property color chipText: "#2E3440"

        property QtObject timeChip: QtObject {
            property color background: "#8FBCBB"
            property color border: "#81A1C1"
            property color text: "#2E3440"
        }

        property QtObject addTagButton: QtObject {
            property color text: "white"
            property color background: polarFrostThemeRoot.accent
            // Hover state
            property color textHover: "white"
            property color backgroundHover: "#A1D5D4"    // lighter accent
            // Pressed state
            property color textPressed: "white"
            property color backgroundPressed: "#6FA9A8"  // darker accent
            // Optional border states
            property color border: polarFrostThemeRoot.accent
            property color borderHover: "#A1D5D4"
            property color borderPressed: "#4C7F7E"
        }

        property QtObject tagChip: QtObject {
            property color background: "#5E81AC"
            property color border: "#4C566A"
            property color text: "white"
            property color deleteHover: "#81A1C1"
            property color deleteBorderHover: "#81A1C1"
        }

        property QtObject tagInput: QtObject {
            property color background: "#2E3440"
            property color text: "white"
            property color placeholder: "#81A1C1"
            property color border: polarFrostThemeRoot.accent
            property color selection: polarFrostThemeRoot.accent
            property color selectedText: "white"
        }

        property QtObject nameField: QtObject {
            property color background: "transparent"
        }
    }

    // SearchBox nested
    property QtObject searchBox: QtObject {
        property color pillBackground: "#2E3440"
        property color pillBorder: "#4C566A"
        property color iconColor: "white"
        property color inputText: "white"
        property color inputBackground: "transparent"

        property color suggestionsBackground: "#3B4252"
        property color suggestionsBorder: "#4C566A"
        property color suggestionRowBorder: "#434C5E"

        property color suggestionText: "#ECEFF4"
        property color suggestionTextHover: "#FFFFFF"
        property color coordText: "#81A1C1"
        property color coordTextHover: "#D8DEE9"
        property color highlightBackground: polarFrostThemeRoot.accent
    }

    // AddWaypointDialog
    property QtObject addWaypointDialog: QtObject {
        property color background: "#434C5E"
        property color border: "#4C566A"

        property color headerBackground: polarFrostThemeRoot.accent
        property color headerBorder: "#4C566A"
        property color headerText: "white"

        property color footerBackground: "#434C5E"
        property color footerBorder: "#4C566A"

        property QtObject button: QtObject {
            property color text: "white"
            property color background: polarFrostThemeRoot.accent
            property color backgroundPressed: "#5E81AC"
            property color border: "#5E81AC"
        }

        property QtObject coordinateLabel: QtObject {
            property color background: polarFrostThemeRoot.accent
            property color text: "white"
        }

        property QtObject coordinateValue: QtObject {
            property color background: "#2E3440"
            property color border: "#4C566A"
            property color text: "white"
        }

        property QtObject textField: QtObject {
            property color text: "white"
            property color placeholderText: "#81A1C1"
            property color background: "#2E3440"
            property color border: polarFrostThemeRoot.accent
            property color selection: polarFrostThemeRoot.accent
            property color selectedText: "white"
        }
    }

    // MapControlButton
    property QtObject mapControlButton: QtObject {
        property color background: "#2E3440"
        property color backgroundHover: "#434C5E"
        property color border: "#4C566A"
        property color text: "white"
        property color textHover: polarFrostThemeRoot.accent
        property QtObject tooltip: QtObject {
            property color background: "#2E3440"
            property color border: "#4C566A"
            property color text: "white"
        }
    }

    // WaypointTable
    property QtObject waypointTable: QtObject {
        property color background: "#3B4252"
        property color border: "#4C566A"
        property color headerText: "white"
        property color headerSeparator: "#4C566A"
        property color rowAltA: "#343C49"
        property color rowAltB: "#2E3440"
        property color rowSelected: "#5E81AC"
        property color rowHover: "#434C5E"
        property color rowBorder: "#434C5E"
        property color nameText: "white"
        property color tagsText: "#81A1C1"
        property color coordText: "#D8DEE9"
        property color badgeBookmark: "#B48EAD"
        property color separator: "#4C566A"
        property color closeButtonText: "white"
        property color columnHeaderText: "#D8DEE9"
        property color closeButtonBackground: "#434C5E"
        property color closeButtonBackgroundHover: polarFrostThemeRoot.accent
        property color closeButtonBorder: "#4C566A"
        property color closeButtonBorderHover: polarFrostThemeRoot.accent
    }

    // MapStatusBar
    property QtObject mapStatusBar: QtObject {
        property color sectionBackground: "transparent"
    }

    // SnackBar
    property QtObject snackBar: QtObject {
        property color background: "#434C5E"
        property color text: "white"
        property color actionBackground: polarFrostThemeRoot.accent
        property color actionText: "white"
    }

    // Utility
    property color transparent: "transparent"
    property color white: "white"
}
