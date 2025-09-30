import QtQuick 2.15
import QtQuick.Controls 2.15
import "../themes"
import QtQuick.Layouts 1.15
import QtQuick.Shapes 1.15
import "../lib/SearchBoxLogic.js" as SearchBoxLogic

/*
  SearchBox.qml
  Pill-shaped search input with detached floating suggestions panel.
  History stored only on explicit action (Enter / selection).
  Core imperative logic has been externalized to `SearchBoxLogic.js` (imported
  as `SearchBoxLogic`) so this component now delegates filtering, tag
  completion, recent-history handling and key navigation to the shared library.
*/

Rectangle {
    id: searchBox
    ThemeLoader {
        id: theme
    }

    // Public API -----------------------------------------------------------
    property alias text: input.text
    property alias input: input
    property var suggestions: []          // array of objects {name,lat,lon,source,...}
    property int maxSuggestions: 8
    property bool showSuggestions: true
    property int debounceInterval: 250
    property var api: null                // API service object
    property var recentSearches: []       // array of objects { name, source:"recent", lat?, lon? }
    property int maxRecent: 10
    property var waypoints: []            // Provided by parent for local tag: searches (array of waypoint/bookmark objects)
    signal search(string query)
    signal suggestionChosen(var suggestion)
    signal hideRequested
    // Tag filter state (active when query starts with "tag:")
    property bool tagFilterActive: false
    property var tagFilterMatchedWaypoints: []   // Full waypoint objects matching current tag expression
    signal tagFilterChanged(bool active, var matched)
    // Distinct tag vocabulary fetched once from backend (enriched objects: { raw, emoji?, display })
    property var distinctTagVocabulary: []
    property bool distinctTagsLoaded: false
    property int _distinctFetchId: 0    // fetch guard token for async distinct tag loads

    // Internal state -------------------------------------------------------
    property var internalSuggestions: []
    property int highlightedIndex: -1

    // Visual container is transparent; pill + panel render their own surfaces
    color: theme.transparent
    Component.onCompleted: {
        // Preload distinct tags once (backend provides enriched objects)
        if (api && api.apiPort >= 0 && !distinctTagsLoaded) {
            _distinctFetchId++;
            let fid = _distinctFetchId;
            api.fetchDistinctTags(function (list) {
                if (fid !== _distinctFetchId)
                    return; // stale
                distinctTagVocabulary = list || [];
                distinctTagsLoaded = true;
            });
        }
    }
    border.width: 0
    clip: false
    implicitWidth: 360

    // ---------------- Helper Functions -----------------------------------
    function refreshFilter() {
        SearchBoxLogic.refreshFilter(searchBox);
    }

    function clearSuggestions() {
        SearchBoxLogic.clearSuggestions(searchBox);
    }

    function showRecentIfEmpty() {
        SearchBoxLogic.showRecentIfEmpty(searchBox);
    }

    function moveHighlight(delta) {
        SearchBoxLogic.moveHighlight(searchBox, delta, suggestionList);
    }

    function activateHighlighted() {
        SearchBoxLogic.activateHighlighted(searchBox);
    }

    // Tag filter helpers ----------------------------------------------------
    // Build tag completions from distinctTagVocabulary (enriched objects)
    function _buildTagCompletions(expr, fullQuery) {
        SearchBoxLogic.buildTagCompletions(searchBox, expr, fullQuery);
    }
    function clearTagFilter() {
        SearchBoxLogic.clearTagFilter(searchBox);
    }

    function computeTagFilterForQuery(q) {
        SearchBoxLogic.computeTagFilterForQuery(searchBox, q);
    }

    // ---------------- Layout ----------------------------------------------
    Column {
        id: column
        anchors.fill: parent
        spacing: 6

        // Pill input
        Rectangle {
            id: inputPill
            width: parent.width
            implicitHeight: 44
            radius: height / 2
            color: theme.searchBox.pillBackground
            border.color: theme.searchBox.pillBorder
            border.width: 1
            layer.enabled: true
            layer.samples: 4

            RowLayout {
                id: pillRow
                anchors.fill: parent
                anchors.leftMargin: 14
                anchors.rightMargin: 14
                spacing: 8

                Image {
                    id: icon
                    source: "qrc:/icons/magnifier.svg"
                    sourceSize.width: 18
                    sourceSize.height: 18
                    Layout.alignment: Qt.AlignVCenter
                }

                TextField {
                    id: input
                    Layout.fillWidth: true
                    color: theme.searchBox.inputText
                    selectionColor: theme.accent
                    selectedTextColor: theme.white
                    background: Rectangle {
                        color: theme.searchBox.inputBackground
                    }
                    font.pixelSize: theme.scale ? theme.scale(2) : 14
                    implicitHeight: 40
                    leftPadding: 0
                    rightPadding: 0

                    onTextChanged: {
                        var result = SearchBoxLogic.handleTextChanged(searchBox);
                        switch (result.action) {
                        case "recent":
                            fetchDebounce.restart();
                            if (searchBox.api)
                                searchBox.api.getRecentSearches(searchBox.maxRecent);
                            break;
                        case "backendTagSuggest":
                            if (searchBox.api)
                                searchBox.api.suggest(result.query);
                            break;
                        case "tagFetchDistinct":
                            searchBox._distinctFetchId++;
                            let fid = searchBox._distinctFetchId;
                            if (searchBox.api)
                                searchBox.api.fetchDistinctTags(function (list) {
                                    if (fid !== searchBox._distinctFetchId)
                                        return;
                                    searchBox.distinctTagVocabulary = list || [];
                                    searchBox.distinctTagsLoaded = true;
                                    SearchBoxLogic.buildTagCompletions(searchBox, result.tagExpr, result.query);
                                });
                            break;
                        case "backendSuggest":
                            fetchDebounce.restart();
                            break;
                        case "noop":
                        default:
                            break;
                        }
                    }

                    onAccepted: {
                        activateHighlighted();
                        searchBox.hideRequested();
                    }

                    Keys.onPressed: function (event) {
                        SearchBoxLogic.handleKeyNavigation(searchBox, event, suggestionList);
                    }
                }
            }
        }

        // Suggestions panel (floating)
        Rectangle {
            id: suggestionsPanel
            width: parent.width
            visible: searchBox.showSuggestions && searchBox.internalSuggestions.length > 0
            radius: 12
            color: theme.searchBox.suggestionsBackground
            border.color: theme.searchBox.suggestionsBorder
            border.width: 1
            implicitHeight: suggestionList.implicitHeight
            anchors.topMargin: 8    // larger gap so pill doesn't visually square off the rounded corners
            z: 201
            clip: true               // ensure rounded corners clip delegate backgrounds
            antialiasing: true
            layer.enabled: true
            layer.samples: 4

            ListView {
                id: suggestionList
                anchors.fill: parent
                model: searchBox.internalSuggestions
                interactive: searchBox.internalSuggestions.length > searchBox.maxSuggestions
                clip: true
                currentIndex: searchBox.highlightedIndex
                implicitHeight: Math.min(searchBox.internalSuggestions.length, searchBox.maxSuggestions) * 36
                boundsBehavior: Flickable.StopAtBounds
                spacing: 0

                delegate: Rectangle {
                    id: rowRect
                    width: parent.width
                    height: 36
                    // Canvas-based highlight:
                    //  - First row: only top corners rounded, bottom edge square
                    //  - Other rows: full rectangle
                    // This removes accidental bottom rounding when first item is selected.
                    property color highlightColor: theme.searchBox.highlightBackground
                    property bool selected: (index === searchBox.highlightedIndex) || hover.containsMouse
                    color: theme.transparent
                    clip: false
                    border.color: theme.searchBox.suggestionRowBorder
                    border.width: (index === 0 ? 0 : 0.5)

                    Item {
                        id: highlightLayer
                        anchors.fill: parent
                        visible: rowRect.selected
                        property bool isFirst: index === 0
                        property bool isLast: index === suggestionList.count - 1
                        property bool single: isFirst && isLast
                        // Path recomputed automatically via binding when geometry/state changes
                        property string pathString: SearchBoxLogic.computeHighlightPath(searchBox, highlightLayer.width, highlightLayer.height, suggestionsPanel.radius, highlightLayer.isFirst, highlightLayer.isLast, highlightLayer.single)
                        Shape {
                            anchors.fill: parent
                            antialiasing: true
                            ShapePath {
                                id: highlightShapePath
                                strokeWidth: 0
                                fillColor: rowRect.highlightColor
                                PathSvg {
                                    path: highlightLayer.pathString
                                }
                            }
                        }
                    }

                    Row {
                        id: textRow
                        anchors.verticalCenter: parent.verticalCenter
                        anchors.left: parent.left
                        anchors.right: parent.right
                        anchors.leftMargin: 10
                        anchors.rightMargin: 10
                        spacing: 4
                        clip: true
                        property bool hasCoords: (typeof modelData === "object") && (modelData.lat !== undefined && modelData.lon !== undefined)
                        // Main (prefix + name) text
                        Text {
                            id: mainLabel
                            text: {
                                if (typeof modelData === "object") {
                                    if (modelData.source === "tagloading") {
                                        return "⏳ " + (modelData.display || "Loading tags…");
                                    }
                                    if (modelData.source === "tagvocab") {
                                        // Show emoji+raw (display) without the leading 'tag:' prefix
                                        var lbl = modelData.display || modelData.completionTag || modelData.name;
                                        return "🏷 " + lbl;
                                    }
                                    var prefix = modelData.source === "recent" ? "🕘 " : (modelData.source === "bookmark" ? "★ " : (modelData.source === "waypoint" ? "• " : "🌐 "));
                                    return prefix + modelData.name;
                                }
                                return "" + modelData;
                            }
                            color: hover.containsMouse ? theme.searchBox.suggestionTextHover : theme.searchBox.suggestionText
                            elide: Text.ElideRight
                            wrapMode: Text.NoWrap
                            maximumLineCount: 1
                            clip: true
                            // Reserve space for coords label when visible
                            width: parent.width - (coordLabel.visible ? (coordLabel.implicitWidth + textRow.spacing) : 0)
                        }
                        // Coordinates (dimmed)
                        Text {
                            id: coordLabel
                            visible: textRow.hasCoords
                            text: textRow.hasCoords ? "(" + modelData.lat.toFixed(4) + ", " + modelData.lon.toFixed(4) + ")" : ""
                            color: hover.containsMouse ? theme.searchBox.coordTextHover : theme.searchBox.coordText
                            opacity: hover.containsMouse ? 0.85 : 0.65
                            font.italic: false
                            elide: Text.ElideRight
                            wrapMode: Text.NoWrap
                            maximumLineCount: 1
                            clip: true
                        }
                    }

                    MouseArea {
                        id: hover
                        anchors.fill: parent
                        hoverEnabled: true
                        cursorShape: Qt.PointingHandCursor
                        onClicked: {
                            searchBox.highlightedIndex = index;
                            // Tag vocabulary completion: update text only
                            if (modelData && modelData.source === "tagvocab") {
                                searchBox.input.text = modelData.name;
                                searchBox.computeTagFilterForQuery(modelData.name);
                                searchBox.input.forceActiveFocus();
                                searchBox.input.cursorPosition = searchBox.input.text.length;
                                searchBox.refreshFilter();
                                return;
                            }
                            if (searchBox.suggestionChosen)
                                searchBox.suggestionChosen(modelData);
                            var n = (modelData && typeof modelData === "object") ? modelData.name : ("" + modelData);
                            if (searchBox.api && n && n.length > 0) {
                                searchBox.api.recordHistory(n, modelData.lat, modelData.lon);
                            }
                            searchBox.input.forceActiveFocus();
                            searchBox.input.selectAll();
                            searchBox.hideRequested();
                        }
                        onEntered: {
                            searchBox.highlightedIndex = index;
                            suggestionList.currentIndex = index;
                        }
                    }
                }
            }
        }
    }

    // ---------------- Debounce Timer --------------------------------------
    Timer {
        id: fetchDebounce
        interval: searchBox.debounceInterval
        repeat: false
        onTriggered: {
            var q = searchBox.input.text.trim();
            if (!q || q.length === 0) {
                showRecentIfEmpty();
                return;
            }

            // Tag search delegated to backend; if offline (no api or apiPort < 0) fall back to simple local literal match.
            if (q.length >= 4 && q.substring(0, 4).toLowerCase() === "tag:") {
                if (searchBox.api && searchBox.api.apiPort >= 0) {
                    searchBox.api.suggest(q);
                    return;
                }
                // Offline fallback
                var tagQuery = q.substring(4).trim().toLowerCase();
                var offlineResults = [];
                if (tagQuery.length > 0 && searchBox.waypoints && searchBox.waypoints.length > 0) {
                    for (var i = 0; i < searchBox.waypoints.length; i++) {
                        var w = searchBox.waypoints[i];
                        if (!w || !w.tags || w.tags.length === 0)
                            continue;
                        for (var t = 0; t < w.tags.length; t++) {
                            var tg = w.tags[t];
                            if (tg && tg.toString().toLowerCase() === tagQuery) {
                                offlineResults.push({
                                    name: w.name,
                                    lat: w.lat,
                                    lon: w.lon,
                                    source: w.bookmark ? "bookmark" : "waypoint",
                                    tags: w.tags
                                });
                                break;
                            }
                        }
                    }
                }
                searchBox.suggestions = offlineResults;
                searchBox.highlightedIndex = (offlineResults.length > 0 ? 0 : -1);
                searchBox.refreshFilter();
                return;
            }

            if (searchBox.api)
                searchBox.api.suggest(q);
        }
    }

    // ---------------- API Connections -------------------------------------
    Connections {
        target: searchBox.api
        enabled: !!searchBox.api

        function onSuggestResults(resultObject, query) {
            SearchBoxLogic.applySuggestResults(searchBox, resultObject, query);
        }

        function onSuggestFailed(errorMessage, query) {
            var current = searchBox.input.text.trim();
            if (current !== query.trim())
                return;
            console.error("SearchBox: suggest failed:", errorMessage);
        }

        function onRecentSearchesFetched(queries, limit) {
            SearchBoxLogic.applyRecentSearches(searchBox, queries);
        }

        function onRecentSearchEntriesFetched(entries, limit) {
            SearchBoxLogic.applyRecentSearchEntries(searchBox, entries);
        }

        function onRecentSearchesFetchFailed(errorMessage, limit) {
            if (searchBox.input.text.trim().length === 0) {
                searchBox.recentSearches = [];
                showRecentIfEmpty();
            }
        }

        function onTagAdded(name, lat, lon, tags, tag) {
            if (!searchBox.api || searchBox.api.apiPort < 0)
                return;
            SearchBoxLogic.reloadDistinctTags(searchBox, function (cb) {
                searchBox.api.fetchDistinctTags(cb);
            });
        }
        function onTagDeleted(name, lat, lon, tags, tag) {
            onTagAdded(name, lat, lon, tags, tag);
        }
    }

    // React to external changes to suggestions
    onSuggestionsChanged: {
        SearchBoxLogic.onExternalSuggestionsChanged(searchBox);
    }

    onShowSuggestionsChanged: {
        SearchBoxLogic.onShowSuggestionsChanged(searchBox);
    }
}
