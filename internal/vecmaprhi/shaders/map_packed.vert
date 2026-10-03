#version 440
// The packed layouts (scene.PackedPositionLayout, PackedOffsetLayout and
// PackedDashedLayout) store int16 pairs. Qt's GL backend reads 16-bit integer
// vertex formats as floats while Vulkan reads them as integers, so each pair
// travels as one 32-bit integer, low half first, and is unpacked here.
layout(location=0) in int packedPosition;
layout(location=1) in int packedOffset;
layout(location=2) in float distance;
layout(location=0) out vec2 localPosition;
layout(location=1) out vec2 uv;
layout(std140,binding=0) uniform State {
    mat4 matrix;
    vec4 transformX;
    vec4 transformY;
    vec4 color;
    vec4 clipRect;
    vec4 parameters; // kind, font scale, halo width, halo blur
    vec4 pattern; // width, height, phase x, phase y
    vec4 view; // DPR, inherited opacity, pixel offset scale (zero means one), vertex layout
} state;

// scene.PositionUnits and scene.OffsetUnits.
const float positionUnits = 32.0;
const float offsetUnits = 4096.0;

vec2 unpack(int pair) {
    return vec2(float((pair << 16) >> 16), float(pair >> 16));
}

void main() {
    vec2 position = unpack(packedPosition) / positionUnits;
    localPosition = position;
    // Layout three has a position only and layout four no distance. Their inputs
    // for the missing attributes alias the position and are not read here.
    vec2 pixelOffset = state.view.w > 3.5 ? unpack(packedOffset) / offsetUnits : vec2(0.0);
    uv = vec2(state.view.w > 4.5 ? distance : 0.0, 0.0);
    vec3 p = vec3(position,1.0);
    vec2 screen = vec2(dot(state.transformX.xyz,p),dot(state.transformY.xyz,p));
    vec2 offset = pixelOffset;
    if (state.transformX.w != 0.0) {
        float scale = max(length(vec2(state.transformX.x,state.transformY.x)),0.0001);
        offset = vec2(dot(state.transformX.xy,pixelOffset),dot(state.transformY.xy,pixelOffset)) / scale;
    }
    // Extruded lines store unit directions and supply their half width here, so
    // one resident mesh serves every evaluated line width.
    if (state.view.z > 0.0) offset *= state.view.z;
    gl_Position = state.matrix * vec4(screen + offset,0.0,1.0);
}
