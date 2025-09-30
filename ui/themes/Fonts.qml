pragma Singleton
import QtQuick 2.15

/*
  Fonts.qml (centralized typography scaling)
  ------------------------------------------
  Provides a single modular type scale that all themes and components can
  leverage. Themes may override `minFontSize` and `fontScaleRatio` locally;
  components should prefer `theme.scale(step)` if the active theme already
  exports one (as forwarded by `ThemeLoader`). When that is not available
  (e.g. extremely early in boot or for legacy themes), they can fall back
  to this singleton.

  Rationale:
    - Keeps a single place to tune global typography rhythm.
    - Prevents copy/paste scale logic across every theme file.
    - Allows themes to opt-in to customized ratios (larger base, bigger steps).

  Scale definition (default values here):
      scale(1) = minFontSize
      scale(n) = minFontSize * fontScaleRatio^(n-1)

  With defaults (12, 1.15):
      step 1 ≈ 12
      step 2 ≈ 14
      step 3 ≈ 16
      step 4 ≈ 18–19
      step 5 ≈ 21
      step 6 ≈ 24

  Usage Patterns:
    1. Preferred (theme-aware):
         font.pixelSize = theme.scale(2)

    2. Fallback to Fonts singleton when theme not yet loaded:
         import "themes/Fonts.qml" as Fonts
         font.pixelSize = (theme && theme.scale) ? theme.scale(2) : Fonts.scale(2)

    3. For a one-off override (different base or ratio):
         font.pixelSize = Fonts.scale(3, 14, 1.2)

    4. If a theme wants to override the default base or ratio:
         property int minFontSize: 13
         property real fontScaleRatio: 1.18
         function scale(step) { return Fonts.scale(step, minFontSize, fontScaleRatio); }

  Guidance for choosing steps:
     - scale(1): Small labels, secondary metadata
     - scale(2): Body text, normal UI labels
     - scale(3): Section headers, dialog headings
     - scale(4): Prominent headings, card titles
     - scale(5): Large emphasis / sparse hero text
     - scale(6+): Rare, only for major splash or banner emphasis

  NOTE:
    This file is intentionally lean—no theme colors or other UI tokens here.
*/

QtObject {
    id: fonts

    // Central defaults (themes may override locally by providing their own
    // minFontSize / fontScaleRatio and delegating to Fonts.scale(step, minFontSize, fontScaleRatio)).
    // Exposed directly (no 'default*' prefix) so components can import Fonts
    // and read these if needed.
    property int minFontSize: 12
    property real fontScaleRatio: 1.15

    // Maximum defensive clamp – prevents runaway requests (e.g. scale(200)).
    property int maxReasonableStep: 32

    // Core scaling function.
    // step: 1-based integer scale level (1 = base).
    // overrideMin (optional): alternate minimum font size.
    // overrideRatio (optional): alternate ratio.
    function scale(step, overrideMin, overrideRatio) {
        var s = (typeof step === "number" && step > 0) ? Math.min(step, maxReasonableStep) : 1;
        var base = (typeof overrideMin === "number" && overrideMin > 0) ? overrideMin : minFontSize;
        var ratio = (typeof overrideRatio === "number" && overrideRatio > 0) ? overrideRatio : fontScaleRatio;
        if (s === 1)
            return base;
        // Use Math.round to keep integers (Qt/font engines prefer whole pixel sizes).
        return Math.round(base * Math.pow(ratio, s - 1));
    }

    // Convenience aliases for common semantic steps (optional syntactic sugar).
    function body() {
        return scale(2);
    }
    function small() {
        return scale(1);
    }
    function heading() {
        return scale(3);
    }
    function title() {
        return scale(4);
    }
    function largeTitle() {
        return scale(5);
    }
    function display() {
        return scale(6);
    }

    // Helper to safely attempt theme-based scaling inside JS expressions:
    // Fonts.tryTheme(theme, 3) -> either theme.scale(3) or fallback Fonts.scale(3)
    function tryTheme(themeObj, step) {
        if (themeObj && typeof themeObj.scale === "function")
            return themeObj.scale(step);
        return scale(step);
    }

    // Diagnostic utility (optional; can be invoked from console):

}
