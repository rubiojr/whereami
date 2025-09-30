import QtQuick 2.15
import "qrc:/themes" as Themes

/*
  AdwaitaDarkTheme.qml
  --------------------
  Adwaita (Dark) color theme adapted for the WhereAmI app.

  Source palette (Adwaita Dark 1.5 docs):
    Accent:            @accent_bg_color  #3584e4  (bg), @accent_color #78aeed (standalone)
    Window bg/fg:      #242424 / #ffffff
    View bg/fg:        #1e1e1e / #ffffff
    Headerbar bg:      #303030
    Card bg:           rgba(255,255,255,0.08)
    Snack/Popover/Dialog bg: #383838
    Success bg:        #26a269
    Warning bg:        #cd9309
    Error bg:          #c01c28

  Notes:
    - We keep the existing theme object property structure for drop‑in compatibility.
    - accent            -> standalone accent (@accent_color) for text emphasis (#78aeed)
    - primaryText       -> accent-tinted light text (same as accent)
    - secondaryText     -> a mid neutral from the light ramp (#c0bfbc)
    - darkColor         -> primary window background (#242424)
    - background        -> main app background (#242424)
    - textColor         -> primaryText
    - waypoint / cluster marker colors chosen for readable contrast on dark tiles.
*/

QtObject {
    id: adwaitaThemeRoot

    // Core brand / base colors
    property color background: "#242424"
    property color darkColor: "#242424"
    property color accent: "#b5835a"          // switched to Adwaita brown_2
    property color primaryText: "#cdab8f"     // brown_1 (slightly lighter for readability)
    property color secondaryText: "#c0bfbc"
    property color textColor: primaryText

    // Search box (kept similar to default, dark-transparent pill)
    property color searchBoxBackground: "#1e1e1e"
    property color searchBoxText: "#ffffff"
    property color searchBoxPlaceholder: "#9a9996"
    property color searchBoxBorder: "#3d3846"
    property int searchBoxRadius: 10
    property int searchBoxHeight: 40
    property color searchBoxButtonBackground: "#b5835a"   // brown accent bg
    property color searchBoxButtonText: "white"

    property int toolbarButtonSize: 32
    // Derive status bar size from modular scale
    property int statusBarTextSize: Themes.Fonts.scale(1)
    // Toolbar palette (Adwaita Dark) – switched to neutral charcoal with subtle contrast hovers
    property QtObject toolbar: QtObject {
        // Neutral base (#353535) with gentle lightening on hover; keeps brown accent for hover icons
        property color background: "#353535"                // new base background
        property color backgroundHover: "#404040"           // hover lift
        property color border: "#4a4a4a"                    // subtle edge
        property color separator: "#555555"                 // divider
        property color icon: "#f2e4d8"                      // readable on dark neutral
        property color iconHover: adwaitaThemeRoot.accent   // brown accent hover
        property color iconDisabled: "#8c7464"              // muted
        property color text: adwaitaThemeRoot.primaryText
        property color textSecondary: adwaitaThemeRoot.secondaryText
    }

    property color waypointInfoCardTitleColor: "#b5835a"
    property color waypointInfoCardTitleTextColor: background
    property int waypointInfoCardTitleSize: Themes.Fonts.scale(3)
    property int waypointInfoCardPrimaryInfoFontSize: Themes.Fonts.scale(3)

    // Distinct bookmark color (purple from Adwaita palette)
    property color bookmarkColor: "#9141ac"

    // Waypoint/cluster marker sizing
    property int waypointMarkerRadius: 8
    property int clusterMarkerRadius: 16
    // Cluster color uses a slightly deeper accent variant for contrast
    property color clusterMarkerColor: "#865e3c"

    // Map marker extended colors
    property color waypointDefaultColor: "#b5835a"
    property color waypointSelectedColor: "#63452c"          // deep brown highlight
    property color waypointSelectedHaloColor: Qt.rgba(0.7, 0.47, 0.37, 0.35)
    property color waypointBorderColor: "white"
    property color currentLocationColor: "#986a44"           // brown_3 repurposed
    property color currentLocationInnerColor: "#cdab8f"
    property color searchResultColor: accent   // now brown accent

    // WaypointInfoCard nested colors
    property QtObject waypointInfoCard: QtObject {
        property color background: "#383838"
        property color border: "#3d3846"
        property color primaryText: "white"
        property color secondaryText: "#deddda"
        property color noTagsText: "#9a9996"

        property QtObject editButton: QtObject {
            // Normal state (neutral pill)
            property color text: "#242424"
            property color background: "#f6f5f4"
            // Hover state (accent brown for strong contrast)
            property color textHover: "#242424"
            property color backgroundHover: "#E7D8CC"
            // Pressed state (darker accent brown)
            property color textPressed: "white"
            property color backgroundPressed: "#8c643f"
            // Border states to frame button on dark background
            property color border: "#b5835a"
            property color borderHover: "#c9966d"
            property color borderPressed: "#734f31"
            // Optional focus outline (retain accent)
            property color focusOutline: "#b5835a"
        }

        property QtObject cancelButton: QtObject {
            property color text: "white"
            property color background: "#c01c28"
            property color backgroundPressed: "#a51d2d"
            property color border: "#a51d2d"
        }

        property QtObject bookmarkChip: QtObject {
            property color background: "#dc8add"
            property color border: "#c061cb"
        }

        property QtObject gpxChip: QtObject {
            property color background: "#99c1f1"
            property color border: "#62a0ea"
        }

        property color chipText: "#241f31"

        property QtObject timeChip: QtObject {
            property color background: "#33d17a"
            property color border: "#26a269"
            property color text: "white"
        }

        property QtObject addTagButton: QtObject {
            // Base state
            property color text: "white"
            property color background: "#b5835a"
            // Hover state (slightly lighter / more lifted)
            property color textHover: "white"
            property color backgroundHover: "#d0a076"
            // Pressed state (darker, tactile)
            property color textPressed: "white"
            property color backgroundPressed: "#a06b3e"
            // Optional border states (can be used by the component later)
            property color border: "#b5835a"
            property color borderHover: "#d0a076"
            property color borderPressed: "#8c5c34"
        }

        property QtObject tagChip: QtObject {
            property color background: "#c061cb"
            property color border: "#9141ac"
            property color text: "white"
            property color deleteHover: "#dc8add"
            property color deleteBorderHover: "#9141ac"
        }

        property QtObject tagInput: QtObject {
            property color background: "#1e1e1e"
            property color text: "white"
            property color placeholder: "#9a9996"
            property color border: "#b5835a"
            property color selection: "#b5835a"
            property color selectedText: "white"
        }

        property QtObject nameField: QtObject {
            property color background: "transparent"
        }
    }

    // SearchBox nested colors
    property QtObject searchBox: QtObject {
        property color pillBackground: "#1e1e1e"
        property color pillBorder: "#3d3846"
        property color iconColor: "white"
        property color inputText: "white"
        property color inputBackground: "transparent"

        property color suggestionsBackground: "#242424"
        property color suggestionsBorder: "#3d3846"
        property color suggestionRowBorder: "#3d3846"

        property color suggestionText: "white"
        property color suggestionTextHover: "#ffffff"
        property color coordText: "#c0bfbc"
        property color coordTextHover: "#f6f5f4"
        property color highlightBackground: "#b5835a"
    }

    // AddWaypointDialog nested colors
    property QtObject addWaypointDialog: QtObject {
        property color background: "#383838"
        property color border: "#3d3846"

        property color headerBackground: "#b5835a"
        property color headerBorder: "#3d3846"
        property color headerText: "white"

        property color footerBackground: "#383838"
        property color footerBorder: "#3d3846"

        property QtObject button: QtObject {
            property color text: "white"
            property color background: "#b5835a"
            property color backgroundPressed: "#865e3c"
            property color border: "#865e3c"
        }

        property QtObject coordinateLabel: QtObject {
            property color background: "#b5835a"
            property color text: "white"
        }

        property QtObject coordinateValue: QtObject {
            property color background: "#1e1e1e"
            property color border: "#3d3846"
            property color text: "white"
        }

        property QtObject textField: QtObject {
            property color text: "white"
            property color placeholderText: "#9a9996"
            property color background: "#1e1e1e"
            property color border: "#b5835a"
            property color selection: "#b5835a"
            property color selectedText: "white"
        }
    }

    // MapControlButton nested colors
    property QtObject mapControlButton: QtObject {
        property color background: "#2a2a2a"
        property color backgroundHover: "#3a322d"
        property color border: "#3d3846"
        property color text: "white"
        property color textHover: "#cdab8f"

        property QtObject tooltip: QtObject {
            property color background: "#1e1e1e"
            property color border: "#3d3846"
            property color text: "white"
        }
    }

    // WaypointTable nested colors
    property QtObject waypointTable: QtObject {
        property color background: "#303030"
        property color border: "#3d3846"
        property color headerText: "white"
        property color headerSeparator: "#3d3846"
        property color rowAltA: "#2a2a2a"
        property color rowAltB: "#242424"
        property color rowSelected: "#865e3c"
        property color rowHover: "#383838"
        property color rowBorder: "#3d3846"
        property color nameText: "white"
        property color tagsText: "#c0bfbc"
        property color coordText: "#deddda"
        property color badgeBookmark: "#b5835a"
        property color separator: "#3d3846"
        property color closeButtonText: "white"
        property color columnHeaderText: "#c0bfbc"
        // New close button styling moved from component:
        property color closeButtonBackground: "#2a2a2a"
        property color closeButtonBackgroundHover: "#b5835a"
        property color closeButtonBorder: "#3d3846"
        property color closeButtonBorderHover: "#b5835a"
    }

    // MapStatusBar colors
    property QtObject mapStatusBar: QtObject {
        property color sectionBackground: "transparent"
    }

    // SnackBar colors (using popover/dialog bg)
    property QtObject snackBar: QtObject {
        property color background: "#383838"
        property color text: "white"
        property color actionBackground: "#b5835a"
        property color actionText: "white"
    }

    // General / utility
    property color transparent: "transparent"
    property color white: "white"
}
