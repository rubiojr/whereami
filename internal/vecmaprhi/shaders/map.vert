#version 440
layout(location=0) in vec2 position;
layout(location=1) in vec2 pixelOffset;
layout(location=2) in vec2 texcoord;
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
    vec4 view; // DPR, inherited opacity, reserved, reserved
} state;
void main() {
    localPosition = position;
    uv = texcoord;
    vec3 p = vec3(position,1.0);
    vec2 screen = vec2(dot(state.transformX.xyz,p),dot(state.transformY.xyz,p));
    vec2 offset = pixelOffset;
    if (state.transformX.w != 0.0) {
        float scale = max(length(vec2(state.transformX.x,state.transformY.x)),0.0001);
        offset = vec2(dot(state.transformX.xy,pixelOffset),dot(state.transformY.xy,pixelOffset)) / scale;
    }
    gl_Position = state.matrix * vec4(screen + offset,0.0,1.0);
}
