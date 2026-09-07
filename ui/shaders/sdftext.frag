#version 440

// SDF thresholds and halo equations are derived from MapLibre GL JS.
// See LICENSE in this directory for the retained BSD 3-Clause notices.

layout(location = 0) in vec2 vTexCoord;
layout(location = 0) out vec4 fragColor;
layout(binding = 1) uniform sampler2D atlasTexture;

layout(std140, binding = 0) uniform buf {
    mat4 qt_Matrix;
    float qt_Opacity;
    vec4 fillColor;
    vec4 haloColor;
    float fontScale;
    float haloWidth;
    float haloBlur;
    float devicePixelRatio;
} ubuf;

void main()
{
    const float sdfPixels = 8.0;
    const float sdfBorder = 3.0;
    const float innerEdge = 0.75;
    float scale = max(ubuf.fontScale, 0.0001);
    float edgeGamma = 0.105 / max(ubuf.devicePixelRatio, 1.0);
    float fillGamma = edgeGamma / scale;
    float distanceValue = texture(atlasTexture, vTexCoord).r;
    float fillAlpha = smoothstep(innerEdge - fillGamma, innerEdge + fillGamma, distanceValue);

    float haloGamma = (ubuf.haloBlur * 1.19 / sdfPixels + edgeGamma) / scale;
    float haloWidth = min(max(ubuf.haloWidth / scale, 0.0), sdfBorder);
    float haloEdge = (6.0 - haloWidth) / sdfPixels;
    float haloOuter = smoothstep(haloEdge - haloGamma, haloEdge + haloGamma, distanceValue);
    float haloInner = smoothstep(innerEdge, innerEdge + 2.0 * haloGamma, distanceValue);
    float haloAlpha = min(haloOuter, 1.0 - haloInner);

    vec4 fill = vec4(ubuf.fillColor.rgb * ubuf.fillColor.a, ubuf.fillColor.a)
        * fillAlpha * ubuf.qt_Opacity;
    vec4 halo = vec4(ubuf.haloColor.rgb * ubuf.haloColor.a, ubuf.haloColor.a)
        * haloAlpha * ubuf.qt_Opacity;
    fragColor = fill + (1.0 - fill.a) * halo;
}
