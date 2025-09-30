import QtQuick 2.15
import "qrc:/themes" as Themes

/*
    ThemeLoader.qml

    Simple theme loader that allows compile-time theme switching.

    To change theme, modify the THEME_VARIANT constant below:
    - "orange" (default)
    - "green"
    - "purple"
    - "adwaita-dark"
    - "nord-polar"
    - "nord-frost"

    Usage in components:
        ThemeLoader {
            id: theme
        }

        Rectangle {
            color: theme.accent
        }
*/

Loader {
    id: themeLoader

    // ============ CHANGE THIS TO SWITCH THEMES ============
    // Theme can be selected at runtime via --theme flag (forms: --theme=value or --theme value).
    // Allowed variants: orange, green, purple, adwaita-dark, nord-polar, nord-frost
    property string themeVariant: computeThemeVariant()

    function computeThemeVariant() {
        var args;
        try {
            args = Qt.application && Qt.application.arguments ? Qt.application.arguments : [];
        } catch (e) {
            args = [];
        }
        var allowed = {
            "orange": true,
            "green": true,
            "purple": true,
            "adwaita-dark": true,
            "nord-polar": true,
            "nord-frost": true
        };
        var fallback = "nord-polar";
        for (var i = 0; i < args.length; i++) {
            var a = args[i];
            if (a.indexOf("--theme=") === 0) {
                var val = a.substring(8).trim();
                if (allowed[val])
                    return val;
            }
            if (a === "--theme" && i + 1 < args.length) {
                var v2 = args[i + 1].trim();
                if (allowed[v2])
                    return v2;
            }
        }
        return fallback;
    }
    // ======================================================

    source: {
        switch (themeVariant) {
        case "green":
            return "qrc:/themes/GreenTheme.qml";
        case "purple":
            return "qrc:/themes/PurpleTheme.qml";
        case "adwaita-dark":
            return "qrc:/themes/AdwaitaDarkTheme.qml";
        case "nord-polar":
            return "qrc:/themes/NordPolarTheme.qml";
        case "nord-frost":
            return "qrc:/themes/NordFrostTheme.qml";
        case "orange":
        default:
            return "qrc:/themes/Theme.qml";
        }
    }

    // Load status logging
    onStatusChanged:
    // Theme loading status can be checked via status property if needed
    {}

    // Forward all theme properties from the loaded theme
    // Fallback theme object to satisfy static analysis when 'item' is null
    QtObject {
        id: __fallback
        // Font scaling defaults now delegated to Fonts singleton (themes may still override locally)
        property int minFontSize: Themes.Fonts.minFontSize
        property real fontScaleRatio: Themes.Fonts.fontScaleRatio
        // (Per-theme fallback scale removed; unified loader-level scale() now handles all scaling.)

        property color background: "#222226"
        property color accent: "#F97700"
        property color primaryText: "#FF8226"
        property color secondaryText: "#BDBDBD"
        property color darkColor: "#222226"
        property color textColor: "#FF8226"
        property QtObject searchBox: QtObject {}
        property QtObject waypointInfoCard: QtObject {}
        property QtObject addWaypointDialog: QtObject {}
        property QtObject mapControlButton: QtObject {}
        property QtObject toolbar: QtObject {
            property color background: "#000000"
            property color backgroundHover: "#111111"
            property color border: "#444"
            property color separator: "#555"
            property color icon: "white"
            property color iconHover: "#F97700"
            property color iconDisabled: "#666666"
            property color text: "#FF8226"
            property color textSecondary: "#BDBDBD"
        }
        property QtObject waypointTable: QtObject {}
        property QtObject mapStatusBar: QtObject {}
        property QtObject snackBar: QtObject {}
        property color transparent: "transparent"
        property color white: "white"
        property color searchBoxBackground: "#FFFFFF"
        property color searchBoxText: "#000000"
        property color searchBoxPlaceholder: "#777777"
        property color searchBoxBorder: "#E6E6E6"
        property int searchBoxRadius: 10
        property int searchBoxHeight: 40
        property color searchBoxButtonBackground: "#F97700"
        property color searchBoxButtonText: "White"
        property int toolbarButtonSize: 24
        property int statusBarTextSize: 14
        property color waypointInfoCardTitleColor: "#F97700"
        property color waypointInfoCardTitleTextColor: "#222226"
        property int waypointInfoCardTitleSize: 22
        property int waypointInfoCardPrimaryInfoFontSize: 22
        property color bookmarkColor: "#9C27B0"
        property int waypointMarkerRadius: 8
        property int clusterMarkerRadius: 16
        property color waypointDefaultColor: "#2196F3"
        property color waypointSelectedColor: "#FF1744"
        property color waypointSelectedHaloColor: Qt.rgba(1, 0.09, 0.27, 0.35)
        property color waypointBorderColor: "white"
        property color currentLocationColor: "#1976D2"
        property color currentLocationInnerColor: "white"
        property color searchResultColor: "#F97700"
    }

    readonly property var __active: item ? item : __fallback

    readonly property color background: __active.background
    readonly property color accent: __active.accent
    readonly property color primaryText: __active.primaryText
    readonly property color secondaryText: __active.secondaryText
    readonly property color darkColor: __active.darkColor
    readonly property color textColor: __active.textColor

    // Search box
    readonly property var searchBox: __active.searchBox

    // Waypoint info card
    readonly property var waypointInfoCard: __active.waypointInfoCard

    // Add waypoint dialog
    readonly property var addWaypointDialog: __active.addWaypointDialog

    // Map control button
    readonly property var mapControlButton: __active.mapControlButton

    // Toolbar palette
    readonly property var toolbar: __active.toolbar
    readonly property color toolbarBackground: toolbar.background
    readonly property color toolbarBackgroundHover: toolbar.backgroundHover
    readonly property color toolbarBorder: toolbar.border
    readonly property color toolbarSeparator: toolbar.separator
    readonly property color toolbarIcon: toolbar.icon
    readonly property color toolbarIconHover: toolbar.iconHover
    readonly property color toolbarIconDisabled: toolbar.iconDisabled
    readonly property color toolbarText: toolbar.text
    readonly property color toolbarTextSecondary: toolbar.textSecondary

    // Waypoint table
    readonly property var waypointTable: __active.waypointTable

    // Map status bar
    readonly property var mapStatusBar: __active.mapStatusBar

    // Snack bar
    readonly property var snackBar: __active.snackBar

    // General UI colors
    readonly property color transparent: __active.transparent
    readonly property color white: __active.white

    // Legacy properties for backward compatibility
    readonly property color searchBoxBackground: __active.searchBoxBackground
    readonly property color searchBoxText: __active.searchBoxText
    readonly property color searchBoxPlaceholder: __active.searchBoxPlaceholder
    readonly property color searchBoxBorder: __active.searchBoxBorder
    readonly property int searchBoxRadius: __active.searchBoxRadius
    readonly property int searchBoxHeight: __active.searchBoxHeight
    readonly property color searchBoxButtonBackground: __active.searchBoxButtonBackground
    readonly property color searchBoxButtonText: __active.searchBoxButtonText

    readonly property int toolbarButtonSize: __active.toolbarButtonSize
    readonly property int statusBarTextSize: __active.statusBarTextSize

    // Font scaling API (forwarded from active theme or fallback)
    // Safe fallback: if active theme does not define minFontSize, use Fonts singleton default
    readonly property int minFontSize: (typeof __active.minFontSize === "number" && __active.minFontSize > 0) ? __active.minFontSize : Themes.Fonts.minFontSize
    // Safe fallback: if active theme does not define fontScaleRatio, use Fonts singleton default
    readonly property real fontScaleRatio: (typeof __active.fontScaleRatio === "number" && __active.fontScaleRatio > 0) ? __active.fontScaleRatio : Themes.Fonts.fontScaleRatio
    function scale(step) {
        // Centralized scaling: always delegate to Fonts singleton using active theme overrides.
        return Themes.Fonts.scale(step, minFontSize, fontScaleRatio);
    }
    readonly property color waypointInfoCardTitleColor: __active.waypointInfoCardTitleColor
    readonly property color waypointInfoCardTitleTextColor: __active.waypointInfoCardTitleTextColor
    readonly property int waypointInfoCardTitleSize: __active.waypointInfoCardTitleSize
    readonly property int waypointInfoCardPrimaryInfoFontSize: __active.waypointInfoCardPrimaryInfoFontSize

    readonly property color bookmarkColor: __active.bookmarkColor

    readonly property int waypointMarkerRadius: __active.waypointMarkerRadius
    readonly property int clusterMarkerRadius: __active.clusterMarkerRadius
    readonly property color clusterMarkerColor: __active.clusterMarkerColor
    readonly property color waypointDefaultColor: __active.waypointDefaultColor
    readonly property color waypointSelectedColor: __active.waypointSelectedColor
    readonly property color waypointSelectedHaloColor: __active.waypointSelectedHaloColor
    readonly property color waypointBorderColor: __active.waypointBorderColor
    readonly property color currentLocationColor: __active.currentLocationColor
    readonly property color currentLocationInnerColor: __active.currentLocationInnerColor
    readonly property color searchResultColor: __active.searchResultColor
}
