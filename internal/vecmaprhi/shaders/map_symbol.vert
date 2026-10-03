#version 440
// scene.PackedSymbolLayout stores an int16 anchor, an int16 pixel offset and a
// uint16 texture coordinate, each pair in one 32-bit integer, low half first,
// for the reason map_packed.vert gives.
layout(location=0) in int packedAnchor;
layout(location=1) in int packedOffset;
layout(location=2) in int packedTexcoord;
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

// scene.AnchorUnits, PixelUnits and TexcoordUnits.
const float anchorUnits = 64.0;
const float pixelUnits = 32.0;
const float texcoordUnits = 65535.0;

vec2 unpack(int pair) {
    return vec2(float((pair << 16) >> 16), float(pair >> 16));
}

vec2 unpackUnsigned(int pair) {
    return vec2(float(pair & 0xffff), float((pair >> 16) & 0xffff));
}

void main() {
    vec2 position = unpack(packedAnchor) / anchorUnits;
    localPosition = position;
    uv = unpackUnsigned(packedTexcoord) / texcoordUnits;
    vec2 pixelOffset = unpack(packedOffset) / pixelUnits;
    vec3 p = vec3(position,1.0);
    vec2 screen = vec2(dot(state.transformX.xyz,p),dot(state.transformY.xyz,p));
    vec2 offset = pixelOffset;
    if (state.transformX.w != 0.0) {
        float scale = max(length(vec2(state.transformX.x,state.transformY.x)),0.0001);
        offset = vec2(dot(state.transformX.xy,pixelOffset),dot(state.transformY.xy,pixelOffset)) / scale;
    }
    // Resident icons and text are packed at a base size and scaled here.
    if (state.view.z > 0.0) offset *= state.view.z;
    gl_Position = state.matrix * vec4(screen + offset,0.0,1.0);
}
