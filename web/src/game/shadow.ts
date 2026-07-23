import { Filter, GlProgram, GpuProgram, UniformGroup } from "pixi.js";
import type { Collider } from "../types";

const maxOccluders = 32;
const packedOccluders = maxOccluders * 4;

const glVertex = `
in vec2 aPosition;
out vec2 vTextureCoord;
uniform vec4 uInputSize;
uniform vec4 uOutputFrame;
uniform vec4 uOutputTexture;
void main(void) {
  vec2 position = aPosition * uOutputFrame.zw + uOutputFrame.xy;
  position.x = position.x * (2.0 / uOutputTexture.x) - 1.0;
  position.y = position.y * (2.0 * uOutputTexture.z / uOutputTexture.y) - uOutputTexture.z;
  gl_Position = vec4(position, 0.0, 1.0);
  vTextureCoord = aPosition;
}`;

// Pixi prepends `precision mediump float;` to a fragment source that does not
// declare one of its own, and a driver that honours mediump as fp16 caps every
// intermediate at 65504 — well below the square of a screen coordinate. Asking
// for highp keeps this shader in fp32; the tests below keep every intermediate
// at coordinate scale so it stays correct even where highp is unavailable.
export const glFragment = `precision highp float;
in vec2 vTextureCoord;
out vec4 finalColor;
uniform vec4 uOccluders[${maxOccluders}];
uniform vec2 uOrigin;
uniform vec2 uSize;
uniform float uOpacity;
uniform int uCount;
uniform float uReveal;

// The segment reaches the disc when the closest point on it is within the
// radius. Measuring along a normalised direction keeps every intermediate at
// coordinate scale, unlike the quadratic's discriminant, which squares a screen
// coordinate twice and overflows to a NaN that reads as "no hit".
bool circleHit(vec2 origin, vec2 delta, vec3 circle) {
  float span = length(delta);
  if (span < 0.001) return false;
  vec2 direction = delta / span;
  vec2 toCircle = circle.xy - origin;
  float along = clamp(dot(toCircle, direction), 0.0, span);
  return length(toCircle - direction * along) <= circle.z;
}

bool boxHit(vec2 origin, vec2 delta, vec4 box) {
  vec2 low = box.xy - box.zw, high = box.xy + box.zw;
  float near = 0.001, far = 0.999;
  if (abs(delta.x) < 0.0001) {
    if (origin.x < low.x || origin.x > high.x) return false;
  } else {
    float first = (low.x - origin.x) / delta.x, second = (high.x - origin.x) / delta.x;
    near = max(near, min(first, second)); far = min(far, max(first, second));
  }
  if (abs(delta.y) < 0.0001) {
    if (origin.y < low.y || origin.y > high.y) return false;
  } else {
    float first = (low.y - origin.y) / delta.y, second = (high.y - origin.y) / delta.y;
    near = max(near, min(first, second)); far = min(far, max(first, second));
  }
  return near <= far;
}

void main(void) {
  vec2 point = vTextureCoord * uSize;
  vec2 delta = point - uOrigin;
  // Inside smoke the viewer sees only a small circle around itself: everything
  // past the reveal radius is shadowed regardless of what stands there.
  bool blocked = uReveal > 0.0 && length(delta) > uReveal;
  for (int index = 0; index < ${maxOccluders}; index++) {
    if (blocked || index >= uCount) break;
    vec4 shape = uOccluders[index];
    if ((shape.w < 0.0 && circleHit(uOrigin, delta, shape.xyz)) || (shape.w >= 0.0 && boxHit(uOrigin, delta, shape))) {
      blocked = true;
    }
  }
  vec3 color = vec3(0.027, 0.063, 0.102);
  finalColor = blocked ? vec4(color * uOpacity, uOpacity) : vec4(0.0);
}`;

const gpuSource = `
struct GlobalFilterUniforms {
  uInputSize: vec4<f32>, uInputPixel: vec4<f32>, uInputClamp: vec4<f32>,
  uOutputFrame: vec4<f32>, uGlobalFrame: vec4<f32>, uOutputTexture: vec4<f32>,
};
struct ShadowUniforms {
  uOccluders: array<vec4<f32>, ${maxOccluders}>,
  uOrigin: vec2<f32>, uSize: vec2<f32>, uOpacity: f32, uCount: i32, uReveal: f32, uPadding: f32,
};
@group(0) @binding(0) var<uniform> gfu: GlobalFilterUniforms;
@group(0) @binding(1) var uTexture: texture_2d<f32>;
@group(0) @binding(2) var uSampler: sampler;
@group(1) @binding(0) var<uniform> shadow: ShadowUniforms;

struct VSOutput { @builtin(position) position: vec4<f32>, @location(0) uv: vec2<f32> };
@vertex fn mainVertex(@location(0) aPosition: vec2<f32>) -> VSOutput {
  var position = aPosition * gfu.uOutputFrame.zw + gfu.uOutputFrame.xy;
  position.x = position.x * (2.0 / gfu.uOutputTexture.x) - 1.0;
  position.y = position.y * (2.0 * gfu.uOutputTexture.z / gfu.uOutputTexture.y) - gfu.uOutputTexture.z;
  return VSOutput(vec4(position, 0.0, 1.0), aPosition);
}

// The same closest-point test the GL path runs, so both backends answer alike.
fn circleHit(origin: vec2<f32>, delta: vec2<f32>, circle: vec3<f32>) -> bool {
  let span = length(delta);
  if (span < 0.001) { return false; }
  let direction = delta / span;
  let toCircle = circle.xy - origin;
  let along = clamp(dot(toCircle, direction), 0.0, span);
  return length(toCircle - direction * along) <= circle.z;
}

fn boxHit(origin: vec2<f32>, delta: vec2<f32>, box: vec4<f32>) -> bool {
  let low = box.xy - box.zw;
  let high = box.xy + box.zw;
  var near = 0.001;
  var far = 0.999;
  if (abs(delta.x) < 0.0001) {
    if (origin.x < low.x || origin.x > high.x) { return false; }
  } else {
    let first = (low.x - origin.x) / delta.x;
    let second = (high.x - origin.x) / delta.x;
    near = max(near, min(first, second));
    far = min(far, max(first, second));
  }
  if (abs(delta.y) < 0.0001) {
    if (origin.y < low.y || origin.y > high.y) { return false; }
  } else {
    let first = (low.y - origin.y) / delta.y;
    let second = (high.y - origin.y) / delta.y;
    near = max(near, min(first, second));
    far = min(far, max(first, second));
  }
  return near <= far;
}

@fragment fn mainFragment(@location(0) uv: vec2<f32>) -> @location(0) vec4<f32> {
  let point = uv * shadow.uSize;
  let delta = point - shadow.uOrigin;
  // Inside smoke the viewer sees only a small circle around itself.
  var blocked = shadow.uReveal > 0.0 && length(delta) > shadow.uReveal;
  for (var index = 0; index < ${maxOccluders}; index++) {
    if (blocked || index >= shadow.uCount) { break; }
    let shape = shadow.uOccluders[index];
    if ((shape.w < 0.0 && circleHit(shadow.uOrigin, delta, shape.xyz)) || (shape.w >= 0.0 && boxHit(shadow.uOrigin, delta, shape))) {
      blocked = true;
    }
  }
  let color = vec3<f32>(0.027, 0.063, 0.102);
  return select(vec4<f32>(0.0), vec4<f32>(color * shadow.uOpacity, shadow.uOpacity), blocked);
}`;

/** Packs the nearest occluders into the fixed-size GPU uniform block. */
export function packShadowOccluders(colliders: Collider[], origin: { x: number; y: number }): { count: number; data: Float32Array } {
  const nearest = [...colliders].sort((left, right) =>
    (left.x - origin.x) ** 2 + (left.y - origin.y) ** 2 - ((right.x - origin.x) ** 2 + (right.y - origin.y) ** 2),
  ).slice(0, maxOccluders);
  const data = new Float32Array(packedOccluders);
  nearest.forEach((collider, index) => {
    const offset = index * 4;
    data[offset] = collider.x; data[offset + 1] = collider.y;
    if (collider.shape === "circle") {
      data[offset + 2] = collider.radius; data[offset + 3] = -1;
    } else {
      data[offset + 2] = collider.width / 2; data[offset + 3] = collider.height / 2;
    }
  });
  return { count: nearest.length, data };
}

export class SightShadowFilter extends Filter {
  private readonly shadowUniforms: UniformGroup;

  constructor() {
    const shadowUniforms = new UniformGroup({
      uOccluders: { value: new Float32Array(packedOccluders), type: "vec4<f32>", size: maxOccluders },
      uOrigin: { value: new Float32Array(2), type: "vec2<f32>" },
      uSize: { value: new Float32Array(2), type: "vec2<f32>" },
      uOpacity: { value: 0.27, type: "f32" },
      uCount: { value: 0, type: "i32" },
      uReveal: { value: 0, type: "f32" },
      uPadding: { value: 0, type: "f32" },
    });
    super({
      glProgram: GlProgram.from({ vertex: glVertex, fragment: glFragment, name: "sight-shadow" }),
      gpuProgram: GpuProgram.from({
        vertex: { source: gpuSource, entryPoint: "mainVertex" },
        fragment: { source: gpuSource, entryPoint: "mainFragment" },
      }),
      resources: { shadowUniforms }, resolution: 0.5, antialias: "off", padding: 0,
    });
    this.shadowUniforms = shadowUniforms;
  }

  update(origin: { x: number; y: number }, width: number, height: number, colliders: Collider[], reveal = 0): void {
    const packed = packShadowOccluders(colliders, origin);
    (this.shadowUniforms.uniforms.uOccluders as Float32Array).set(packed.data);
    (this.shadowUniforms.uniforms.uOrigin as Float32Array).set([origin.x, origin.y]);
    (this.shadowUniforms.uniforms.uSize as Float32Array).set([width, height]);
    this.shadowUniforms.uniforms.uCount = packed.count;
    this.shadowUniforms.uniforms.uReveal = reveal;
  }
}
