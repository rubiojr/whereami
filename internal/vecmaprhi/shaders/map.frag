#version 440
// SDF thresholds and halo equations reuse ui/shaders/sdftext.frag, derived
// from MapLibre GL JS. See ui/shaders/LICENSE for the BSD 3-Clause notices.
layout(location=0) in vec2 localPosition;
layout(location=1) in vec2 uv;
layout(location=0) out vec4 fragColor;
layout(binding=1) uniform sampler2D atlas;
layout(std140,binding=0) uniform State {
    mat4 matrix;
    vec4 transformX;
    vec4 transformY;
    vec4 color;
    vec4 clipRect;
    vec4 parameters;
    vec4 pattern;
    vec4 view;
} state;
void main() {
    if (state.clipRect.z > state.clipRect.x &&
        (localPosition.x < state.clipRect.x || localPosition.y < state.clipRect.y ||
         localPosition.x >= state.clipRect.z || localPosition.y >= state.clipRect.w)) discard;
    int kind = int(state.parameters.x);
    vec4 c = state.color;
    if (kind == 1) c *= texture(atlas, uv);
    if (kind == 2) c *= texture(atlas, (localPosition + state.pattern.zw) / state.pattern.xy);
    if (kind >= 3) {
        float distance = texture(atlas, uv).r;
        float scale = max(state.parameters.y, 0.0001);
        float edgeGamma = 0.105 / max(state.view.x,1.0);
        float gamma = edgeGamma / scale;
        float edge = 0.75;
        float coverage = smoothstep(edge-gamma,edge+gamma,distance);
        if (kind == 3) {
            gamma = (state.parameters.w * 1.19 / 8.0 + edgeGamma) / scale;
            edge = (6.0 - min(max(state.parameters.z / scale,0.0),3.0)) / 8.0;
            float outer = smoothstep(edge-gamma,edge+gamma,distance);
            float inner = smoothstep(0.75,0.75+2.0*gamma,distance);
            coverage = min(outer,1.0-inner);
        }
        c.a *= coverage;
    }
    c.a *= state.view.y;
    fragColor = vec4(c.rgb*c.a,c.a);
}
