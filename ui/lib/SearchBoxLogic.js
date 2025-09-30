/*
 * SearchBoxLogic.js
 *
 * Library extracted from `SearchBox.qml` so the imperative / data‑transformation
 * logic lives outside the visual component. This keeps the QML file lean while
 * preserving behavior.
 *
 * Usage inside `SearchBox.qml`:
 *
 *   import "../lib/SearchBoxLogic.js" as SearchBoxLogic
 *
 *   // Examples:
 *   onSuggestionsChanged: SearchBoxLogic.refreshFilter(searchBox)
 *   Keys.onPressed: SearchBoxLogic.handleKeyNavigation(searchBox, event, suggestionList)
 *   // etc.
 *
 * All functions take the `box` parameter which is the `SearchBox` root object
 * (the Rectangle with id: searchBox in the original file). Where a ListView
 * currentIndex sync is needed, you can pass the list view as the optional
 * second argument (e.g. `suggestionList`). Functions avoid direct access to
 * specific ids so they remain decoupled.
 */

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

function _trim(s) {
  return (s || "").trim();
}

function _normSym(s) {
  // Mirrors normalization in original tag completion logic
  return (s || "")
    .toLowerCase()
    .replace(/⭐/g, "*")
    .replace(/💲/g, "$")
    .replace(/\*+/g, "*")
    .replace(/\$+/g, "$")
    .trim();
}

function _isTagQuery(q) {
  return q && q.length >= 4 && q.substring(0, 4).toLowerCase() === "tag:";
}

function _booleanExprPresent(expr) {
  return /(\sAND\s|\sOR\s)/i.test(expr) || expr.indexOf('"') !== -1;
}

// Copy first N entries of an array (defensive against null)
function _cap(arr, maxCount) {
  if (!arr || !arr.length) return [];
  var out = [];
  for (var i = 0; i < arr.length && i < maxCount; i++) out.push(arr[i]);
  return out;
}

// Sync highlightedIndex keeping it within bounds.
function _normalizeHighlight(box) {
  if (!box.internalSuggestions || box.internalSuggestions.length === 0) {
    box.highlightedIndex = -1;
    return;
  }
  if (
    box.highlightedIndex < 0 ||
    box.highlightedIndex >= box.internalSuggestions.length
  )
    box.highlightedIndex = 0;
}

// ---------------------------------------------------------------------------
// Public API (exported)
// ---------------------------------------------------------------------------

/**
 * Refresh the visible internalSuggestions from the public `suggestions` list.
 */
function refreshFilter(box) {
  if (
    !box.showSuggestions ||
    !box.suggestions ||
    box.suggestions.length === 0
  ) {
    box.internalSuggestions = [];
    box.highlightedIndex = -1;
    return;
  }
  box.internalSuggestions = _cap(box.suggestions, box.maxSuggestions);
  _normalizeHighlight(box);
}

/**
 * Clear all suggestion state.
 */
function clearSuggestions(box) {
  box.internalSuggestions = [];
  box.highlightedIndex = -1;
}

/**
 * Replace suggestions with the recent history snapshot if the input is empty.
 */
function showRecentIfEmpty(box) {
  if (_trim(box.input.text).length !== 0) return;
  if (!box.showSuggestions) {
    clearSuggestions(box);
    return;
  }
  if (!box.recentSearches || box.recentSearches.length === 0) {
    clearSuggestions(box);
    return;
  }
  box.internalSuggestions = _cap(box.recentSearches, box.maxRecent);
  box.highlightedIndex = box.internalSuggestions.length > 0 ? 0 : -1;
}

/**
 * Move highlight up/down in the internal suggestions.
 * delta: +1 or -1 (or any integer).
 * suggestionList (optional): ListView to sync currentIndex if provided.
 */
function moveHighlight(box, delta, suggestionList) {
  if (!box.internalSuggestions || box.internalSuggestions.length === 0) return;
  if (box.highlightedIndex < 0) box.highlightedIndex = 0;
  else
    box.highlightedIndex =
      (box.highlightedIndex + delta + box.internalSuggestions.length) %
      box.internalSuggestions.length;
  if (suggestionList) suggestionList.currentIndex = box.highlightedIndex;
}

/**
 * Activate the highlighted suggestion if any. If none is highlighted,
 * treat as a free-text search submission.
 *
 * distinction:
 *  - Tag vocabulary completion (source === 'tagvocab'): update text, recompute tag filter,
 *    keep focus, do not emit suggestionChosen/history yet.
 */
function activateHighlighted(box) {
  if (
    box.highlightedIndex >= 0 &&
    box.internalSuggestions &&
    box.highlightedIndex < box.internalSuggestions.length
  ) {
    var chosen = box.internalSuggestions[box.highlightedIndex];
    if (chosen && chosen.source === "tagvocab") {
      box.input.text = chosen.name;
      computeTagFilterForQuery(box, chosen.name);
      box.input.forceActiveFocus();
      box.input.cursorPosition = box.input.text.length;
      box.input.selectAll();
      return;
    }
    if (box.suggestionChosen) box.suggestionChosen(chosen);
    if (box.api && chosen && chosen.name)
      box.api.recordHistory(chosen.name, chosen.lat, chosen.lon);
    box.input.forceActiveFocus();
    box.input.selectAll();
    return;
  }

  // No highlighted suggestion: treat as raw search
  var q = _trim(box.input.text);
  if (q !== "") {
    if (box.search) box.search(q);
    if (box.api) box.api.recordHistory(q);
  }
  box.input.forceActiveFocus();
  box.input.selectAll();
}

/**
 * Clear active tag filter state and emit tagFilterChanged(false, []) if previously active.
 */
function clearTagFilter(box) {
  if (
    box.tagFilterActive ||
    (box.tagFilterMatchedWaypoints && box.tagFilterMatchedWaypoints.length > 0)
  ) {
    box.tagFilterActive = false;
    box.tagFilterMatchedWaypoints = [];
    if (box.tagFilterChanged) box.tagFilterChanged(false, []);
  }
}

/**
 * Compute local tag filter for simple (single-term) tag queries.
 * For complex expressions (boolean / quoted) we delegate to backend and clear local filter.
 */
function computeTagFilterForQuery(box, q) {
  if (!_isTagQuery(q)) {
    clearTagFilter(box);
    return;
  }
  var expr = _trim(q.substring(4));
  if (_booleanExprPresent(expr)) {
    // Complex expression -> backend only
    clearTagFilter(box);
    return;
  }
  var term = expr.toLowerCase();
  if (!term || !box.waypoints) {
    clearTagFilter(box);
    return;
  }
  var matched = [];
  for (var i = 0; i < box.waypoints.length; i++) {
    var wp = box.waypoints[i];
    if (!wp || !wp.tags) continue;
    for (var t = 0; t < wp.tags.length; t++) {
      var tg = wp.tags[t];
      var rawTag =
        typeof tg === "object" && tg.raw !== undefined ? "" + tg.raw : "" + tg;
      if (rawTag.toLowerCase() === term) {
        matched.push(wp);
        break;
      }
    }
  }
  if (matched.length === 0) {
    clearTagFilter(box);
    return;
  }
  box.tagFilterActive = true;
  box.tagFilterMatchedWaypoints = matched;
  if (box.tagFilterChanged) box.tagFilterChanged(true, matched);
}

/**
 * Build tag vocabulary completions for a simple (non-boolean) tag query fragment.
 * fullQuery: full text ("tag:...") used to also keep live filter current.
 *
 * If completions found:
 *   - overwrites box.suggestions & box.internalSuggestions
 *   - sets box.highlightedIndex = 0
 * Else:
 *   - falls back to computing live local tag filter for immediate Enter usage.
 */
function buildTagCompletions(box, expr, fullQuery) {
  expr = _trim(expr);
  var lowerExpr = _normSym(expr);
  var op = "";
  if (lowerExpr.indexOf(" and ") !== -1) {
    op = "AND";
  } else if (lowerExpr.indexOf(" or ") !== -1) {
    op = "OR";
  }

  var parts =
    op !== "" ? expr.split(new RegExp("\\s+" + op + "\\s+", "i")) : [expr];
  var lastRaw = _trim(parts[parts.length - 1]).replace(/^\"|\"$/g, "");
  var fragment = _normSym(lastRaw);
  if (fragment.length === 0 && lastRaw.length > 0) fragment = _normSym(lastRaw);
  // Preserve multiplicity of * and $ so tags like *, **, *** or $,$$, $$$ remain distinct.
  // If the raw fragment is composed solely of * and/or $, we do literal (prefix) matching
  // without collapsing repeated symbols.
  // Treat any symbol-only (non alphanumeric) fragment literally (examples: **, ***, $$$, ###, !!!, @@, :-:, etc.)
  // This preserves multiplicity and disables the broader fuzzy/normalized matching so users can refine purely symbolic tags.
  var preserveMultiplicity = /^[^a-z0-9\s]+$/i.test(lastRaw);
  if (preserveMultiplicity) {
    fragment = lastRaw.toLowerCase();
  }

  var completions = [];
  var vocab = box.distinctTagVocabulary || [];
  for (var i = 0; i < vocab.length; i++) {
    var tagObj = vocab[i];
    if (!tagObj) continue;
    var raw =
      typeof tagObj === "object" && tagObj.raw !== undefined
        ? "" + tagObj.raw
        : "" + tagObj;
    var emoji =
      typeof tagObj === "object" && tagObj.emoji ? "" + tagObj.emoji : "";
    var display =
      typeof tagObj === "object" && tagObj.display
        ? tagObj.display
        : emoji
          ? emoji + " " + raw
          : raw;

    var rawLC = raw.toLowerCase();
    var displayLC = display.toLowerCase();
    var rawNorm = _normSym(rawLC);
    var displayNorm = _normSym(displayLC);

    var matches;
    if (preserveMultiplicity) {
      // Strict prefix: typing *** only matches tags starting with exactly ***
      matches = raw.indexOf(lastRaw) === 0;
    } else {
      matches =
        fragment.length === 0 ||
        rawLC.indexOf(fragment) === 0 ||
        displayLC.indexOf(fragment) === 0 ||
        rawLC.indexOf(fragment) !== -1 ||
        displayLC.indexOf(fragment) !== -1 ||
        rawNorm.indexOf(fragment) !== -1 ||
        displayNorm.indexOf(fragment) !== -1;
    }

    if (matches) {
      var assembled;
      if (op !== "") {
        var head = parts.slice(0, parts.length - 1).join(" " + op + " ");
        assembled =
          "tag:" + (head.length > 0 ? head + " " + op + " " : "") + raw;
      } else {
        assembled = "tag:" + raw;
      }
      completions.push({
        name: assembled,
        source: "tagvocab",
        completionTag: raw,
        display: display,
        emoji: emoji,
      });
      if (completions.length >= box.maxSuggestions) break;
    }
  }

  if (completions.length > 0) {
    box.suggestions = completions;
    box.internalSuggestions = completions;
    box.highlightedIndex = 0;
  } else {
    // Still compute live filter; user can press Enter immediately.
    computeTagFilterForQuery(box, fullQuery);
  }
  // Keep live filter updated in any case
  computeTagFilterForQuery(box, fullQuery);
}

/**
 * Handle onTextChanged logic (excluding distinct tag async fetch & backend calls).
 * This centralizes branching so the QML side can stay slimmer. Returns an object
 * describing what action (if any) the caller should take:
 *
 * {
 *   action: "recent" | "backendSuggest" | "backendTagSuggest" | "tagFetchDistinct" | "noop",
 *   query: <trimmed query>,
 *   simpleTag: <bool>,            // only when tag path
 *   tagExpr: <string>,            // raw expr after 'tag:'
 * }
 *
 * The QML caller can then perform side effects (restart debounce timer, invoke API, etc).
 */
function handleTextChanged(box) {
  var q = _trim(box.input.text);
  if (q.length === 0) {
    // request recent & show
    showRecentIfEmpty(box);
    clearTagFilter(box);
    return { action: "recent", query: q };
  }

  if (_isTagQuery(q)) {
    var expr = q.substring(4);
    var hasBoolean = _booleanExprPresent(expr);
    if (hasBoolean) {
      // Backend handles complex tag search
      box.internalSuggestions = [];
      box.suggestions = [];
      box.highlightedIndex = -1;
      return {
        action: "backendTagSuggest",
        query: q,
        simpleTag: false,
        tagExpr: expr,
      };
    }
    // Simple tag query path
    if (!box.distinctTagsLoaded && box.api) {
      box.suggestions = [
        {
          name: q,
          source: "tagloading",
          display: "Loading tags…",
        },
      ];
      box.internalSuggestions = box.suggestions;
      box.highlightedIndex = 0;
      return {
        action: "tagFetchDistinct",
        query: q,
        simpleTag: true,
        tagExpr: expr,
      };
    } else {
      buildTagCompletions(box, expr, q);
      return {
        action: "noop",
        query: q,
        simpleTag: true,
        tagExpr: expr,
      };
    }
  }

  // Normal (non-tag) query => backend debounce path
  if (box.tagFilterActive) clearTagFilter(box);

  return { action: "backendSuggest", query: q };
}

/**
 * Convenience: handle key navigation (Up/Down/Tab/Backtab). Returns true if consumed.
 */
function handleKeyNavigation(box, event, suggestionList) {
  switch (event.key) {
    case Qt.Key_Down:
      moveHighlight(box, 1, suggestionList);
      event.accepted = true;
      return true;
    case Qt.Key_Up:
      moveHighlight(box, -1, suggestionList);
      event.accepted = true;
      return true;
    case Qt.Key_Tab:
      moveHighlight(box, 1, suggestionList);
      event.accepted = true;
      return true;
    case Qt.Key_Backtab:
      moveHighlight(box, -1, suggestionList);
      event.accepted = true;
      return true;
    default:
      return false;
  }
}

/**
 * Apply backend suggest results (mirrors original onSuggestResults body).
 */
function applySuggestResults(box, resultObject, query) {
  var current = _trim(box.input.text);
  if (current.length === 0 || current !== _trim(query)) {
    if (current.length === 0) showRecentIfEmpty(box);
    return;
  }
  if (resultObject && resultObject.suggestions) {
    box.suggestions = resultObject.suggestions;
    box.highlightedIndex = box.suggestions.length > 0 ? 0 : -1;
  } else {
    box.suggestions = [];
    box.highlightedIndex = -1;
  }
  refreshFilter(box);
}

/**
 * Apply fetched recent searches (queries only).
 */
function applyRecentSearches(box, queries) {
  var mapped = [];
  for (var i = 0; i < queries.length; i++) {
    mapped.push({ name: queries[i], source: "recent" });
  }
  box.recentSearches = mapped;
  if (_trim(box.input.text).length === 0) showRecentIfEmpty(box);
}

/**
 * Apply fetched recent searches entries with optional coordinates.
 */
function applyRecentSearchEntries(box, entries) {
  var mapped = [];
  for (var i = 0; i < entries.length; i++) {
    var e = entries[i];
    if (!e || !e.query) continue;
    mapped.push({
      name: e.query,
      lat: typeof e.lat === "number" ? e.lat : undefined,
      lon: typeof e.lon === "number" ? e.lon : undefined,
      source: "recent",
    });
  }
  box.recentSearches = mapped;
  if (_trim(box.input.text).length === 0) showRecentIfEmpty(box);
}

/**
 * Refresh distinct tag vocabulary after a tag add/delete.
 * Expects the caller to supply a fetch function: fetchFn(callback)
 * because the library does not own API side-effects.
 *
 * fetchFn(cb) should invoke cb(list || []) when done.
 */
function reloadDistinctTags(box, fetchFn) {
  if (!fetchFn) return;
  box.distinctTagsLoaded = false;
  box._distinctFetchId = (box._distinctFetchId || 0) + 1;
  var fid = box._distinctFetchId;
  var current = _trim(box.input.text);
  var isTagMode = _isTagQuery(current);
  fetchFn(function (list) {
    if (fid !== box._distinctFetchId) return;
    box.distinctTagVocabulary = list || [];
    box.distinctTagsLoaded = true;
    if (isTagMode) {
      var expr = current.substring(4);
      buildTagCompletions(box, expr, current);
    }
  });
}

/**
 * React to external change of suggestions property (mirrors onSuggestionsChanged).
 */
function onExternalSuggestionsChanged(box) {
  if (_trim(box.input.text).length === 0) {
    if (box.api && box.api.getRecentSearches)
      box.api.getRecentSearches(box.maxRecent);
    showRecentIfEmpty(box);
  } else {
    refreshFilter(box);
  }
}

/**
 * React to showSuggestions toggling (mirrors onShowSuggestionsChanged).
 */
function onShowSuggestionsChanged(box) {
  if (!box.showSuggestions) {
    clearSuggestions(box);
    return;
  }
  if (_trim(box.input.text).length === 0) {
    if (box.api && box.api.getRecentSearches)
      box.api.getRecentSearches(box.maxRecent);
    showRecentIfEmpty(box);
  } else {
    refreshFilter(box);
  }
}

// Compute the SVG path used to draw the rounded highlight behind a suggestion row.
// Mirrors the original inline delegate logic so the delegate can just call:
//   pathString: SearchBoxLogic.computeHighlightPath(searchBox, width, height, suggestionsPanel.radius, isFirst, isLast, single)
function computeHighlightPath(box, w, h, panelRadius, isFirst, isLast, single) {
  var r = panelRadius;
  if (r > h) r = h / 2;

  if (single) {
    // Fully rounded pill
    return (
      "M " +
      r +
      " 0 H " +
      (w - r) +
      " Q " +
      w +
      " 0 " +
      w +
      " " +
      r +
      " V " +
      (h - r) +
      " Q " +
      w +
      " " +
      h +
      " " +
      (w - r) +
      " " +
      h +
      " H " +
      r +
      " Q 0 " +
      h +
      " 0 " +
      (h - r) +
      " V " +
      r +
      " Q 0 0 " +
      r +
      " 0 Z"
    );
  } else if (isFirst) {
    // Top rounded, bottom square
    return (
      "M 0 " +
      h +
      " L 0 " +
      r +
      " Q 0 0 " +
      r +
      " 0 " +
      " H " +
      (w - r) +
      " Q " +
      w +
      " 0 " +
      w +
      " " +
      r +
      " L " +
      w +
      " " +
      h +
      " Z"
    );
  } else if (isLast) {
    // Bottom rounded, top square
    return (
      "M 0 0 L 0 " +
      (h - r) +
      " Q 0 " +
      h +
      " " +
      r +
      " " +
      h +
      " H " +
      (w - r) +
      " Q " +
      w +
      " " +
      h +
      " " +
      w +
      " " +
      (h - r) +
      " L " +
      w +
      " 0 Z"
    );
  }
  // Middle row: simple rectangle
  return "M 0 0 H " + w + " V " + h + " H 0 Z";
}

// Exported symbols (optional explicit export map if desired by readers)
// QML JS library automatically exposes top-level function names.
