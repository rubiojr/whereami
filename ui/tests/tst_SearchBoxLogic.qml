import QtQuick 2.15
import QtTest 1.2
import "../lib/SearchBoxLogic.js" as SearchBoxLogic

/*
  TestSearchBoxLogic.qml
  Basic QML/JS unit tests for the tag completion logic in SearchBoxLogic.js.

  Run (from project root or any dir where qmltestrunner can resolve imports):
    qmltestrunner -input ui/tests
  or (explicit import path):
    qmltestrunner -import ./ui -input ui/tests

  These tests focus on:
    - Literal / prefix handling of symbol-only tag fragments (***, $$, ###, !!, etc.)
    - Ensuring multiplicity of symbols is NOT collapsed.
    - Mixed prefix expectations (e.g. ** matches ** and ***, but *** only matches ***).
    - Regular alphanumeric fuzzy behavior still works.

  NOTE: We construct a minimal `box` object mimicking the properties consumed
  by SearchBoxLogic.buildTagCompletions. All functions are pure w.r.t. inputs
  except they mutate the provided `box` object fields (suggestions, internalSuggestions, highlightedIndex).
*/

TestCase {
    name: "SearchBoxLogic"

    // Fresh pseudo-SearchBox state object each test
    property var box

    function init() {
        box = {
            showSuggestions: true,
            suggestions: [],
            internalSuggestions: [],
            maxSuggestions: 50,
            highlightedIndex: -1,
            distinctTagVocabulary: ["*", "**", "***", "$", "$$", "$$$", "###", "!!", "!!*", "@", "@@", "@@@", "abc", "abc*", "star$", "mixed-01"],
            distinctTagsLoaded: true
        };
    }

    function _collect(query) {
        // query must start with "tag:"
        var expr = query.substring(4);
        SearchBoxLogic.buildTagCompletions(box, expr, query);
        return box.internalSuggestions.map(function (s) {
            return s.name;
        });
    }

    function _namesSet(list) {
        var m = {};
        for (var i = 0; i < list.length; i++)
            m[list[i]] = true;
        return m;
    }

    // ---------- Symbol multiplicity tests ----------

    function test_tripleStar_isolated() {
        var res = _collect("tag:***");
        var set = _namesSet(res);
        // Should only contain tag:*** (and nothing for * or **)
        verify(set["tag:***"], "Expected tag:*** present");
        verify(!set["tag:**"], "Did not expect tag:** for query ***");
        verify(!set["tag:*"], "Did not expect tag:* for query ***");
    }

    function test_doubleStar_prefix() {
        var res = _collect("tag:**");
        var set = _namesSet(res);
        // ** matches ** and *** (prefix semantics when fragment shorter)
        verify(set["tag:**"], "Expected tag:** present");
        verify(set["tag:***"], "Expected tag:*** present (prefix match)");
        verify(!set["tag:*"], "Did not expect single star *");
    }

    function test_singleStar_prefix() {
        var res = _collect("tag:*");
        var set = _namesSet(res);
        // * matches *, **, ***
        verify(set["tag:*"], "Expected tag:*");
        verify(set["tag:**"], "Expected tag:**");
        verify(set["tag:***"], "Expected tag:***");
    }

    function test_tripleDollar_isolated() {
        var res = _collect("tag:$$$");
        var set = _namesSet(res);
        verify(set["tag:$$$"], "Expected tag:$$$");
        verify(!set["tag:$$"], "Did not expect tag:$$ when querying $$$");
        verify(!set["tag:$"], "Did not expect tag:$ when querying $$$");
    }

    function test_doubleDollar_prefix() {
        var res = _collect("tag:$$");
        var set = _namesSet(res);
        verify(set["tag:$$"], "Expected tag:$$");
        verify(set["tag:$$$"], "Expected tag:$$$ (prefix)");
        verify(!set["tag:$"], "Did not expect tag:$");
    }

    function test_singleDollar_prefix() {
        var res = _collect("tag:$");
        var set = _namesSet(res);
        verify(set["tag:$"], "Expected tag:$");
        verify(set["tag:$$"], "Expected tag:$$");
        verify(set["tag:$$$"], "Expected tag:$$$");
    }

    function test_hashTriple_isolated() {
        var res = _collect("tag:###");
        var set = _namesSet(res);
        verify(set["tag:###"], "Expected tag:###");
        // Should not accidentally match single or double hashes (they are not in vocab,
        // but we assert absence to catch future additions)
        verify(!set["tag:#"], "Unexpected tag:#");
        verify(!set["tag:##"], "Unexpected tag:##");
    }

    function test_exclamation_prefix() {
        var res = _collect("tag:!!");
        var set = _namesSet(res);
        verify(set["tag:!!"], "Expected tag:!!");
        verify(set["tag:!!*"], "Expected tag:!!* as prefix");
    }

    function test_atSymbols() {
        var res = _collect("tag:@@");
        var set = _namesSet(res);
        verify(set["tag:@@@"], "Expected tag:@@@ (prefix)"); // Adjusted: prefix semantics for symbol-only fragment
        // Re-run with understanding that prefix semantics apply:
        res = _collect("tag:@@");
        set = _namesSet(res);
        verify(set["tag:@@"], "Expected tag:@@");
        verify(set["tag:@@@"], "Expected tag:@@@ (prefix)");
        verify(!set["tag:@"], "Did not expect single @");
    }

    // ---------- Mixed / alphanumeric fuzzy tests ----------

    function test_alphanumeric_fuzzy() {
        var res = _collect("tag:ab");
        var set = _namesSet(res);
        verify(set["tag:abc"], "Expected fuzzy/prefix match for abc");
        verify(set["tag:abc*"], "Expected fuzzy match for abc*");
        verify(!set["tag:star$"], "Did not expect unrelated star$");
    }

    function test_alphanumeric_exact() {
        var res = _collect("tag:abc");
        var set = _namesSet(res);
        verify(set["tag:abc"], "Expected exact abc");
        verify(set["tag:abc*"], "Expected abc* still present (prefix)");
    }

    function test_mixed_symbol_alnum() {
        var res = _collect("tag:star$");
        var set = _namesSet(res);
        verify(set["tag:star$"], "Expected star$ exact");
        // Should not pull in purely symbol sequences
        verify(!set["tag:$"], "Did not expect $");
    }

    // ---------- Regression: ensure previous symbol collapse bug stays fixed ----------

    function test_regression_noCollapse() {
        var res = _collect("tag:***");
        var set = _namesSet(res);
        // If collapse returned, we'd incorrectly see tag:* or tag:** here
        verify(!set["tag:*"], "Regression: single star appeared unexpectedly");
        verify(!set["tag:**"], "Regression: double star appeared unexpectedly");
        verify(set["tag:***"], "Expected only triple star");
    }

    // ---------- Highlight index sanity ----------

    function test_highlightIndex_after_build() {
        _collect("tag:*");
        compare(box.highlightedIndex, 0, "First suggestion should be highlighted");
    }

    // ---------- Empty fragment (just 'tag:') ----------
    function test_empty_fragment_listsAll() {
        var res = _collect("tag:");
        // Should not crash; should list up to maxSuggestions
        verify(res.length > 0, "Expected some completions for empty fragment");
    }
}
