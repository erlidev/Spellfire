import { Filter, GlProgram, GpuProgram, UniformGroup } from "pixi.js";
import { deployableByKind } from "../tuning";
import type { Collider } from "../types";
import { smokeCenterRadiusScale, smokeOffsetScale, smokeOuterRadiusScale } from "./smoke";

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

const glFragment = `
in vec2 vTextureCoord;
out vec4 finalColor;
uniform vec4 uOccluders[${maxOccluders}];
uniform vec2 uOrigin;
uniform vec2 uSize;
uniform float uOpacity;
uniform int uCount;

bool circleHit(vec2 origin, vec2 delta, vec3 circle) {
  vec2 offset = origin - circle.xy;
  float a = dot(delta, delta);
  float b = 2.0 * dot(offset, delta);
  float c = dot(offset, offset) - circle.z * circle.z;
  float discriminant = b * b - 4.0 * a * c;
  if (discriminant < 0.0 || a < 0.0001) return false;
  float root = sqrt(discriminant);
  float near = (-b - root) / (2.0 * a);
  float far = (-b + root) / (2.0 * a);
  return far > 0.001 && near < 0.999;
}

vec3 smokeCircle(vec3 smoke, int index) {
  if (index == 0) return vec3(smoke.xy, smoke.z * ${smokeCenterRadiusScale});
  float offset = smoke.z * ${smokeOffsetScale};
  vec2 direction = index == 1 ? vec2(1.0, 0.0) : index == 2 ? vec2(-1.0, 0.0) : index == 3 ? vec2(0.0, 1.0) : vec2(0.0, -1.0);
  return vec3(smoke.xy + direction * offset, smoke.z * ${smokeOuterRadiusScale});
}

bool smokeHit(vec2 origin, vec2 delta, vec3 smoke) {
  vec2 point = origin + delta;
  bool originInside = false;
  bool pointInViewerCircle = false;
  for (int index = 0; index < 5; index++) {
    vec3 circle = smokeCircle(smoke, index);
    bool containsOrigin = distance(origin, circle.xy) <= circle.z;
    originInside = originInside || containsOrigin;
    pointInViewerCircle = pointInViewerCircle || (containsOrigin && distance(point, circle.xy) <= circle.z);
  }
  if (originInside) return !pointInViewerCircle;
  for (int index = 0; index < 5; index++) if (circleHit(origin, delta, smokeCircle(smoke, index))) return true;
  return false;
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
  bool blocked = false;
  for (int index = 0; index < ${maxOccluders}; index++) {
    if (index >= uCount) break;
    vec4 shape = uOccluders[index];
    if ((shape.w < -1.5 && smokeHit(uOrigin, delta, shape.xyz)) ||
        (shape.w >= -1.5 && shape.w < 0.0 && circleHit(uOrigin, delta, shape.xyz)) ||
        (shape.w >= 0.0 && boxHit(uOrigin, delta, shape))) {
      blocked = true;
      break;
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
  uOrigin: vec2<f32>, uSize: vec2<f32>, uOpacity: f32, uCount: i32, uPadding: vec2<f32>,
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

fn circleHit(origin: vec2<f32>, delta: vec2<f32>, circle: vec3<f32>) -> bool {
  let offset = origin - circle.xy;
  let a = dot(delta, delta);
  let b = 2.0 * dot(offset, delta);
  let c = dot(offset, offset) - circle.z * circle.z;
  let discriminant = b * b - 4.0 * a * c;
  if (discriminant < 0.0 || a < 0.0001) { return false; }
  let root = sqrt(discriminant);
  let near = (-b - root) / (2.0 * a);
  let far = (-b + root) / (2.0 * a);
  return far > 0.001 && near < 0.999;
}

fn smokeCircle(smoke: vec3<f32>, index: i32) -> vec3<f32> {
  if (index == 0) { return vec3<f32>(smoke.xy, smoke.z * ${smokeCenterRadiusScale}); }
  let offset = smoke.z * ${smokeOffsetScale};
  var direction = vec2<f32>(0.0, -1.0);
  if (index == 1) { direction = vec2<f32>(1.0, 0.0); }
  else if (index == 2) { direction = vec2<f32>(-1.0, 0.0); }
  else if (index == 3) { direction = vec2<f32>(0.0, 1.0); }
  return vec3<f32>(smoke.xy + direction * offset, smoke.z * ${smokeOuterRadiusScale});
}

fn smokeHit(origin: vec2<f32>, delta: vec2<f32>, smoke: vec3<f32>) -> bool {
  let point = origin + delta;
  var originInside = false;
  var pointInViewerCircle = false;
  for (var index = 0; index < 5; index++) {
    let circle = smokeCircle(smoke, index);
    let containsOrigin = distance(origin, circle.xy) <= circle.z;
    originInside = originInside || containsOrigin;
    pointInViewerCircle = pointInViewerCircle || (containsOrigin && distance(point, circle.xy) <= circle.z);
  }
  if (originInside) { return !pointInViewerCircle; }
  for (var index = 0; index < 5; index++) {
    if (circleHit(origin, delta, smokeCircle(smoke, index))) { return true; }
  }
  return false;
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
  var blocked = false;
  for (var index = 0; index < ${maxOccluders}; index++) {
    if (index >= shadow.uCount) { break; }
    let shape = shadow.uOccluders[index];
    if ((shape.w < -1.5 && smokeHit(shadow.uOrigin, delta, shape.xyz)) ||
        (shape.w >= -1.5 && shape.w < 0.0 && circleHit(shadow.uOrigin, delta, shape.xyz)) ||
        (shape.w >= 0.0 && boxHit(shadow.uOrigin, delta, shape))) {
      blocked = true;
      break;
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
      data[offset + 2] = collider.radius; data[offset + 3] = deployableByKind(collider.kind)?.conceals ? -2 : -1;
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
      uPadding: { value: new Float32Array(2), type: "vec2<f32>" },
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

  update(origin: { x: number; y: number }, width: number, height: number, colliders: Collider[]): void {
    const packed = packShadowOccluders(colliders, origin);
    (this.shadowUniforms.uniforms.uOccluders as Float32Array).set(packed.data);
    (this.shadowUniforms.uniforms.uOrigin as Float32Array).set([origin.x, origin.y]);
    (this.shadowUniforms.uniforms.uSize as Float32Array).set([width, height]);
    this.shadowUniforms.uniforms.uCount = packed.count;
  }
}
