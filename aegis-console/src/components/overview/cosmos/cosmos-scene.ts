import * as THREE from "three";
import { EffectComposer } from "three/examples/jsm/postprocessing/EffectComposer.js";
import { OutputPass } from "three/examples/jsm/postprocessing/OutputPass.js";
import { RenderPass } from "three/examples/jsm/postprocessing/RenderPass.js";
import { UnrealBloomPass } from "three/examples/jsm/postprocessing/UnrealBloomPass.js";
import {
  AU_LY,
  EARTH_RADIUS_LY,
  ECLIPTIC_BASIS,
  GALACTIC_CENTER_LY,
  GLY,
  KLY,
  LOCAL_GROUP,
  MILKY_WAY_IMAGE_LY,
  MPC_LY,
  NORTH_CELESTIAL_POLE,
  OBSERVABLE_RADIUS_LY,
  PLANETS,
  STRUCTURES,
  planetPosition,
  smoothstep,
  subsolarPoint,
  windowed,
  type Vec3
} from "./cosmos-math";

/*
 * 宇宙彩蛋的场景。所有图层的内容以各自的单位存放（地球半径、天文单位、千光年、Mpc、Gly），
 * 每帧按当前视距 D（光年）把图层缩放为 单位/D、平移到 (原点 − 视点)/D，
 * 相机始终在距视点 1 个场景单位处。这样跨二十个数量级也不丢 float32 精度。
 */

export type CosmosAssets = {
  earthDay: THREE.Texture;
  earthNight: THREE.Texture;
  earthClouds: THREE.Texture;
  earthWater: THREE.Texture;
  sky: THREE.Texture;
  milkyWay: THREE.Texture;
  /** 银河系俯视图的 512² 采样（行 0 为图像底部），用于撒立体恒星 */
  milkyWayPixels: ImageData;
  survey: { count: number; positions: Int16Array; mags: Uint8Array };
  web: { count: number; positions: Int16Array };
  cmb: THREE.Texture;
};

export type FrameState = {
  logDistance: number;
  focus: Vec3;
  /** 相机相对视点的方向（银道坐标，单位向量） */
  direction: THREE.Vector3;
  up: THREE.Vector3;
  time: number;
};

export type LabelAnchor = {
  key: string;
  text: string;
  kind: "major" | "minor";
  position: Vec3;
  visibility: (logDistance: number) => number;
};

type Layer = {
  group: THREE.Group;
  origin: Vec3;
  unit: number;
  opacity: (logDistance: number) => number;
  uniforms: Array<{ uOpacity: { value: number } }>;
};

const pointVertex = /* glsl */ `
attribute vec3 color;
attribute float size;
uniform float uPixelRatio;
uniform float uSizeScale;
varying vec3 vColor;
void main() {
  vColor = color;
  vec4 mv = modelViewMatrix * vec4(position, 1.0);
  gl_Position = projectionMatrix * mv;
  gl_PointSize = max(1.0, size * uSizeScale * uPixelRatio);
}
`;

const pointFragment = /* glsl */ `
uniform float uOpacity;
varying vec3 vColor;
void main() {
  float r = length(gl_PointCoord - 0.5);
  float a = smoothstep(0.5, 0.0, r);
  gl_FragColor = vec4(vColor, a * a * uOpacity);
}
`;

function pointsMaterial(pixelRatio: number, sizeScale = 1) {
  return new THREE.ShaderMaterial({
    uniforms: { uOpacity: { value: 0 }, uPixelRatio: { value: pixelRatio }, uSizeScale: { value: sizeScale } },
    vertexShader: pointVertex,
    fragmentShader: pointFragment,
    transparent: true,
    depthWrite: false,
    blending: THREE.AdditiveBlending
  });
}

function pointsGeometry(positions: Float32Array, colors: Float32Array, sizes: Float32Array) {
  const geometry = new THREE.BufferGeometry();
  geometry.setAttribute("position", new THREE.BufferAttribute(positions, 3));
  geometry.setAttribute("color", new THREE.BufferAttribute(colors, 3));
  geometry.setAttribute("size", new THREE.BufferAttribute(sizes, 1));
  geometry.computeBoundingSphere();
  return geometry;
}

/** 等距圆柱全天图的取样：图像中心 l=0、经度向左增大，与 build-cosmos-assets.py 一致。 */
const skyLookup = /* glsl */ `
vec2 skyUv(vec3 d) {
  float l = atan(d.y, d.x);
  float b = asin(clamp(d.z, -1.0, 1.0));
  return vec2(fract(0.5 - l / 6.28318530718), 0.5 + b / 3.14159265359);
}
`;

const skyVertex = /* glsl */ `
varying vec3 vDirection;
void main() {
  vDirection = normalize((modelMatrix * vec4(position, 1.0)).xyz - cameraPosition);
  gl_Position = projectionMatrix * viewMatrix * modelMatrix * vec4(position, 1.0);
}
`;

const skyFragment = /* glsl */ `
uniform sampler2D uSky;
uniform float uOpacity;
varying vec3 vDirection;
${skyLookup}
void main() {
  vec3 color = texture2D(uSky, skyUv(normalize(vDirection))).rgb;
  gl_FragColor = vec4(color * uOpacity, 1.0);
}
`;

const cmbVertex = /* glsl */ `
varying vec3 vLocal;
void main() {
  vLocal = normalize(position);
  gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
}
`;

const cmbFragment = /* glsl */ `
uniform sampler2D uMap;
uniform float uOpacity;
varying vec3 vLocal;
${skyLookup}
void main() {
  vec3 color = texture2D(uMap, skyUv(vLocal)).rgb * 0.62;
  gl_FragColor = vec4(color, uOpacity);
}
`;

const earthVertex = /* glsl */ `
varying vec2 vUv;
varying vec3 vNormal;
varying vec3 vPosition;
void main() {
  vUv = uv;
  vec4 world = modelMatrix * vec4(position, 1.0);
  vPosition = world.xyz;
  vNormal = normalize(mat3(modelMatrix) * normal);
  gl_Position = projectionMatrix * viewMatrix * world;
}
`;

const earthFragment = /* glsl */ `
uniform sampler2D uDay;
uniform sampler2D uNight;
uniform sampler2D uClouds;
uniform sampler2D uWater;
uniform vec3 uSun;
uniform float uOpacity;
varying vec2 vUv;
varying vec3 vNormal;
varying vec3 vPosition;
void main() {
  vec3 n = normalize(vNormal);
  vec3 view = normalize(cameraPosition - vPosition);
  float sunGeo = dot(n, uSun);
  float diffuse = clamp(sunGeo * 1.05, 0.0, 1.0);
  float cloud = smoothstep(0.16, 0.86, texture2D(uClouds, vUv).r);
  vec3 surface = texture2D(uDay, vUv).rgb * diffuse;
  float water = texture2D(uWater, vUv).r;
  vec3 halfway = normalize(uSun + view);
  surface += vec3(1.0, 0.95, 0.86) * pow(max(dot(n, halfway), 0.0), 160.0) * water * (1.0 - cloud) * 0.28 * step(0.0, sunGeo);
  surface = mix(surface, vec3(0.96) * clamp(sunGeo * 1.2 + 0.02, 0.0, 1.0), cloud * 0.9);
  float glow = smoothstep(0.16, 0.6, dot(texture2D(uNight, vUv).rgb, vec3(0.299, 0.587, 0.114)));
  surface += vec3(1.0, 0.78, 0.48) * glow * 1.4 * (1.0 - smoothstep(-0.22, 0.04, sunGeo)) * (1.0 - cloud * 0.8);
  float rim = pow(1.0 - max(dot(n, view), 0.0), 4.0);
  surface += vec3(0.6, 0.76, 1.0) * rim * smoothstep(-0.25, 0.5, sunGeo) * 0.35;
  gl_FragColor = vec4(surface, uOpacity);
}
`;

const haloVertex = /* glsl */ `
varying vec3 vNormal;
varying vec3 vWorldNormal;
void main() {
  vNormal = normalize(normalMatrix * normal);
  vWorldNormal = normalize(mat3(modelMatrix) * normal);
  gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
}
`;

const haloFragment = /* glsl */ `
uniform vec3 uSun;
uniform float uOpacity;
varying vec3 vNormal;
varying vec3 vWorldNormal;
void main() {
  float rim = pow(clamp(0.62 - dot(vNormal, vec3(0.0, 0.0, 1.0)), 0.0, 1.0), 4.5);
  float lit = 0.1 + 0.9 * smoothstep(-0.3, 0.6, dot(vWorldNormal, uSun));
  gl_FragColor = vec4(vec3(0.55, 0.72, 1.0), rim * lit * 0.9 * uOpacity);
}
`;

const discFragment = /* glsl */ `
uniform sampler2D uMap;
uniform vec3 uTint;
uniform float uOpacity;
uniform float uGain;
varying vec2 vUv;
void main() {
  vec3 color = texture2D(uMap, vUv).rgb * uTint * uGain;
  gl_FragColor = vec4(color, uOpacity);
}
`;

const discVertex = /* glsl */ `
varying vec2 vUv;
void main() {
  vUv = uv;
  gl_Position = projectionMatrix * modelViewMatrix * vec4(position, 1.0);
}
`;

function gaussian(random: () => number) {
  let u = 0;
  let v = 0;
  while (u === 0) u = random();
  while (v === 0) v = random();
  return Math.sqrt(-2 * Math.log(u)) * Math.cos(2 * Math.PI * v);
}

function seeded(seed: number) {
  let state = seed >>> 0;
  return () => {
    state = (state + 0x6d2b79f5) >>> 0;
    let t = state;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

export class CosmosScene {
  readonly scene = new THREE.Scene();
  readonly camera = new THREE.PerspectiveCamera(50, 1, 1e-4, 1e5);
  readonly labels: LabelAnchor[] = [];
  private readonly composer: EffectComposer;
  private readonly bloom: UnrealBloomPass;
  private readonly layers: Layer[] = [];
  private readonly sky: THREE.Mesh;
  private readonly skyMaterial: THREE.ShaderMaterial;
  private readonly cmbMaterial: THREE.ShaderMaterial;
  private readonly earthUniforms: Record<string, { value: unknown }>;
  private readonly haloUniforms: Record<string, { value: unknown }>;
  readonly earthPosition: Vec3;
  /** 地球固连系 → 银道坐标的旋转（轴指北天极，直射点朝向太阳） */
  readonly earthRotation = new THREE.Quaternion();
  private readonly projected = new THREE.Vector3();

  constructor(
    private readonly renderer: THREE.WebGLRenderer,
    assets: CosmosAssets,
    date: Date
  ) {
    const pixelRatio = renderer.getPixelRatio();
    this.earthPosition = planetPosition(PLANETS.find((p) => p.key === "earth")!, date);

    this.composer = new EffectComposer(renderer);
    this.composer.addPass(new RenderPass(this.scene, this.camera));
    this.bloom = new UnrealBloomPass(new THREE.Vector2(512, 512), 0.45, 0.5, 0.78);
    this.composer.addPass(this.bloom);
    this.composer.addPass(new OutputPass());

    // ── 天空：随相机移动的全天星图，太阳附近才看得到 ──
    this.skyMaterial = new THREE.ShaderMaterial({
      uniforms: { uSky: { value: assets.sky }, uOpacity: { value: 1 } },
      vertexShader: skyVertex,
      fragmentShader: skyFragment,
      side: THREE.BackSide,
      depthWrite: false,
      depthTest: false
    });
    this.sky = new THREE.Mesh(new THREE.SphereGeometry(100, 64, 32), this.skyMaterial);
    this.sky.renderOrder = -100;
    this.sky.frustumCulled = false;
    this.scene.add(this.sky);

    // ── 地球 ──
    const sunFromEarth = new THREE.Vector3(...this.earthPosition).negate().normalize();
    this.earthRotation.copy(this.computeEarthRotation(sunFromEarth, date));
    this.earthUniforms = {
      uDay: { value: assets.earthDay },
      uNight: { value: assets.earthNight },
      uClouds: { value: assets.earthClouds },
      uWater: { value: assets.earthWater },
      uSun: { value: sunFromEarth },
      uOpacity: { value: 1 }
    };
    this.haloUniforms = { uSun: { value: sunFromEarth }, uOpacity: { value: 1 } };
    {
      const group = new THREE.Group();
      const earth = new THREE.Mesh(
        new THREE.SphereGeometry(1, 128, 96),
        new THREE.ShaderMaterial({ uniforms: this.earthUniforms, vertexShader: earthVertex, fragmentShader: earthFragment, transparent: true })
      );
      earth.rotation.y = -Math.PI / 2;
      const spin = new THREE.Group();
      spin.quaternion.copy(this.earthRotation);
      spin.add(earth);
      group.add(spin);
      const halo = new THREE.Mesh(
        new THREE.SphereGeometry(1.1, 64, 48),
        new THREE.ShaderMaterial({
          uniforms: this.haloUniforms,
          vertexShader: haloVertex,
          fragmentShader: haloFragment,
          side: THREE.BackSide,
          transparent: true,
          depthWrite: false,
          blending: THREE.AdditiveBlending
        })
      );
      halo.renderOrder = 1;
      group.add(halo);
      this.addLayer(group, this.earthPosition, EARTH_RADIUS_LY, (L) => 1 - smoothstep(-6.9, -6.0, L), [
        this.earthUniforms as { uOpacity: { value: number } },
        this.haloUniforms as { uOpacity: { value: number } }
      ]);
    }

    this.buildSolarSystem(pixelRatio, date);
    this.buildMilkyWay(assets, pixelRatio);
    this.buildLocalGroup(assets, pixelRatio);
    this.buildSurvey(assets, pixelRatio);
    this.buildWeb(assets, pixelRatio);

    // ── 宇宙微波背景外壳 ──
    this.cmbMaterial = new THREE.ShaderMaterial({
      uniforms: { uMap: { value: assets.cmb }, uOpacity: { value: 0 } },
      vertexShader: cmbVertex,
      fragmentShader: cmbFragment,
      transparent: true,
      depthWrite: false
    });
    const cmbGroup = new THREE.Group();
    const shell = new THREE.Mesh(new THREE.SphereGeometry(OBSERVABLE_RADIUS_LY / GLY, 128, 64), this.cmbMaterial);
    shell.renderOrder = -50;
    cmbGroup.add(shell);
    this.addLayer(cmbGroup, [0, 0, 0], GLY, () => 1, []);
  }

  /** 地球固连系（y 为自转轴、经度 0 朝 +z）到银道坐标的旋转。 */
  private computeEarthRotation(sunDirection: THREE.Vector3, date: Date) {
    const { lat, lng } = subsolarPoint(date);
    const subsolar = new THREE.Vector3(Math.cos(lat) * Math.sin(lng), Math.sin(lat), Math.cos(lat) * Math.cos(lng));
    const basis = (axis: THREE.Vector3, toward: THREE.Vector3) => {
      const e1 = axis.clone().normalize();
      const e2 = toward.clone().sub(e1.clone().multiplyScalar(toward.dot(e1))).normalize();
      const e3 = new THREE.Vector3().crossVectors(e1, e2);
      return new THREE.Matrix4().makeBasis(e1, e2, e3);
    };
    const local = basis(new THREE.Vector3(0, 1, 0), subsolar);
    const galactic = basis(new THREE.Vector3(...NORTH_CELESTIAL_POLE), sunDirection);
    const rotation = galactic.multiply(local.transpose());
    return new THREE.Quaternion().setFromRotationMatrix(rotation);
  }

  private addLayer(group: THREE.Group, origin: Vec3, unit: number, opacity: (L: number) => number, uniforms: Layer["uniforms"]) {
    this.scene.add(group);
    this.layers.push({ group, origin, unit, opacity, uniforms });
  }

  private buildSolarSystem(pixelRatio: number, date: Date) {
    const group = new THREE.Group();
    const orbitMaterial = new THREE.LineBasicMaterial({ color: "#8aa0c8", transparent: true, opacity: 0, depthWrite: false });
    const u = new THREE.Vector3(...ECLIPTIC_BASIS.u);
    const v = new THREE.Vector3(...ECLIPTIC_BASIS.v);
    const positions: number[] = [];
    const colors: number[] = [];
    const sizes: number[] = [];
    for (const planet of PLANETS) {
      const points: THREE.Vector3[] = [];
      for (let i = 0; i <= 256; i++) {
        const a = (i / 256) * Math.PI * 2;
        points.push(u.clone().multiplyScalar(Math.cos(a) * planet.au).add(v.clone().multiplyScalar(Math.sin(a) * planet.au)));
      }
      group.add(new THREE.Line(new THREE.BufferGeometry().setFromPoints(points), orbitMaterial));
      const p = planetPosition(planet, date);
      positions.push(p[0] / AU_LY, p[1] / AU_LY, p[2] / AU_LY);
      const color = new THREE.Color(planet.color);
      colors.push(color.r * 1.6, color.g * 1.6, color.b * 1.6);
      sizes.push(planet.au > 4 ? 6 : 4.5);
      if (planet.key !== "earth") {
        this.labels.push({ key: planet.key, text: planet.name, kind: "minor", position: p, visibility: (L) => windowed(L, -5.4, -5.0, -2.2, -1.6) });
      }
    }
    const planetMaterial = pointsMaterial(pixelRatio);
    group.add(new THREE.Points(pointsGeometry(new Float32Array(positions), new Float32Array(colors), new Float32Array(sizes)), planetMaterial));
    const orbitUniform = {
      get value() {
        return orbitMaterial.opacity / 0.35;
      },
      set value(next: number) {
        orbitMaterial.opacity = next * 0.35;
      }
    };
    this.addLayer(group, [0, 0, 0], AU_LY, (L) => windowed(L, -6.0, -5.2, -2.0, -1.2), [planetMaterial.uniforms as Layer["uniforms"][number], { uOpacity: orbitUniform }]);

    // 太阳：恒定像素大小的光点，一直留到银河系尺度
    const sunMaterial = pointsMaterial(pixelRatio);
    const sunGroup = new THREE.Group();
    sunGroup.add(
      new THREE.Points(
        pointsGeometry(new Float32Array([0, 0, 0]), new Float32Array([2.4, 2.1, 1.6]), new Float32Array([16])),
        sunMaterial
      )
    );
    this.addLayer(sunGroup, [0, 0, 0], 1, (L) => windowed(L, -6.2, -5.4, 4.6, 5.6), [sunMaterial.uniforms as Layer["uniforms"][number]]);
    this.labels.push({ key: "sun", text: "太阳", kind: "minor", position: [0, 0, 0], visibility: (L) => windowed(L, -5.4, -5.0, -1.4, -0.8) });
    this.labels.push({ key: "sun-mw", text: "太阳系", kind: "major", position: [0, 0, 0], visibility: (L) => windowed(L, 3.6, 4.0, 5.4, 5.8) });
  }

  private buildMilkyWay(assets: CosmosAssets, pixelRatio: number) {
    const group = new THREE.Group();
    const gcKly = GALACTIC_CENTER_LY / KLY;
    const widthKly = MILKY_WAY_IMAGE_LY / KLY;

    // 盘面：俯视图贴在银道面上。图像上方指向银心方向（+x），右方为 l=270°（−y），太阳在中心正下方。
    const discUniforms = {
      uMap: { value: assets.milkyWay },
      uTint: { value: new THREE.Color(1, 1, 1) },
      uOpacity: { value: 0 },
      uGain: { value: 0.85 }
    };
    const disc = new THREE.Mesh(
      new THREE.PlaneGeometry(widthKly, widthKly),
      new THREE.ShaderMaterial({
        uniforms: discUniforms,
        vertexShader: discVertex,
        fragmentShader: discFragment,
        transparent: true,
        depthWrite: false,
        side: THREE.DoubleSide,
        blending: THREE.AdditiveBlending
      })
    );
    disc.rotation.z = -Math.PI / 2;
    disc.position.set(gcKly, 0, 0);
    group.add(disc);

    // 立体恒星：按俯视图亮度撒点，盘面薄、核球厚，侧看时盘面有厚度
    const random = seeded(20260809);
    const { data, width, height } = assets.milkyWayPixels;
    const count = 60_000;
    const positions = new Float32Array(count * 3);
    const colors = new Float32Array(count * 3);
    const sizes = new Float32Array(count);
    let filled = 0;
    let guard = 0;
    while (filled < count && guard < count * 60) {
      guard++;
      const px = Math.floor(random() * width);
      const py = Math.floor(random() * height);
      const index = (py * width + px) * 4;
      const r = data[index]! / 255;
      const g = data[index + 1]! / 255;
      const b = data[index + 2]! / 255;
      const luminance = 0.299 * r + 0.587 * g + 0.114 * b;
      if (random() > luminance ** 1.6) continue;
      const u = (px + random()) / width;
      const v = (py + random()) / height; // 行 0 是图像底部
      const x = gcKly + (v - 0.5) * widthKly;
      const y = -(u - 0.5) * widthKly;
      const radius = Math.hypot(x - gcKly, y);
      const thickness = radius < 4 ? 1.1 : 0.18 + 0.004 * radius;
      positions.set([x, y, gaussian(random) * thickness], filled * 3);
      const boost = 1.25 + random() * 0.5;
      colors.set([r * boost, g * boost, b * boost], filled * 3);
      sizes[filled] = 1.1 + random() * 1.3;
      filled++;
    }
    const starMaterial = pointsMaterial(pixelRatio);
    group.add(new THREE.Points(pointsGeometry(positions.subarray(0, filled * 3), colors.subarray(0, filled * 3), sizes.subarray(0, filled)), starMaterial));

    // 核球与棒：棒长轴与日心-银心连线约成 27°，近端在 l > 0 一侧
    const bulgeCount = 9_000;
    const bulge = new Float32Array(bulgeCount * 3);
    const bulgeColors = new Float32Array(bulgeCount * 3);
    const bulgeSizes = new Float32Array(bulgeCount);
    const angle = 27 * (Math.PI / 180);
    for (let i = 0; i < bulgeCount; i++) {
      const a = gaussian(random) * 2.4;
      const bAxis = gaussian(random) * 0.95;
      const c = gaussian(random) * 0.75;
      bulge.set([gcKly - a * Math.cos(angle) + bAxis * Math.sin(angle), a * Math.sin(angle) + bAxis * Math.cos(angle), c], i * 3);
      bulgeColors.set([1.0, 0.82 + random() * 0.08, 0.6 + random() * 0.1], i * 3);
      bulgeSizes[i] = 1 + random() * 1.2;
    }
    const bulgeMaterial = pointsMaterial(pixelRatio);
    group.add(new THREE.Points(pointsGeometry(bulge, bulgeColors, bulgeSizes), bulgeMaterial));

    // 球状星团：银晕里约 150 个
    const clusterCount = 150;
    const clusters = new Float32Array(clusterCount * 3);
    const clusterColors = new Float32Array(clusterCount * 3);
    const clusterSizes = new Float32Array(clusterCount);
    for (let i = 0; i < clusterCount; i++) {
      const r = Math.abs(gaussian(random)) * 18 + 2;
      const theta = Math.acos(2 * random() - 1);
      const phi = random() * Math.PI * 2;
      clusters.set([gcKly + r * Math.sin(theta) * Math.cos(phi), r * Math.sin(theta) * Math.sin(phi), r * Math.cos(theta)], i * 3);
      clusterColors.set([1.2, 1.05, 0.85], i * 3);
      clusterSizes[i] = 2.2;
    }
    const clusterMaterial = pointsMaterial(pixelRatio);
    group.add(new THREE.Points(pointsGeometry(clusters, clusterColors, clusterSizes), clusterMaterial));

    // 立体恒星在远处会挤进几个像素、叠加后发白，比盘面早淡出；盘面贴图留到本星系群尺度
    group.remove(disc);
    const discGroup = new THREE.Group();
    discGroup.add(disc);
    this.addLayer(discGroup, [0, 0, 0], KLY, (L) => windowed(L, 2.9, 3.8, 7.0, 7.7), [discUniforms]);
    this.addLayer(group, [0, 0, 0], KLY, (L) => windowed(L, 2.9, 3.8, 5.8, 6.4), [
      starMaterial.uniforms as Layer["uniforms"][number],
      bulgeMaterial.uniforms as Layer["uniforms"][number],
      clusterMaterial.uniforms as Layer["uniforms"][number]
    ]);
    this.labels.push({ key: "mw", text: "银河系", kind: "major", position: [GALACTIC_CENTER_LY, 0, 0], visibility: (L) => windowed(L, 5.7, 6.1, 7.3, 7.8) });
    this.labels.push({ key: "gc", text: "银心", kind: "minor", position: [GALACTIC_CENTER_LY, 0, 0], visibility: (L) => windowed(L, 3.9, 4.3, 5.3, 5.7) });
  }

  private buildLocalGroup(assets: CosmosAssets, pixelRatio: number) {
    const group = new THREE.Group();
    const uniforms: Layer["uniforms"] = [];
    // 麦哲伦云的点云离银河系太近，远看会和银河系挤成一团白光，单独一层提前淡出
    const irregular = new THREE.Group();
    const irregularUniforms: Layer["uniforms"] = [];
    const random = seeded(31);
    const dwarfPositions: number[] = [];
    const dwarfColors: number[] = [];
    const dwarfSizes: number[] = [];

    for (const galaxy of LOCAL_GROUP) {
      const position = new THREE.Vector3(...galaxy.position).divideScalar(KLY);
      if (galaxy.kind === "spiral" && galaxy.normal) {
        const discUniforms = {
          uMap: { value: assets.milkyWay },
          uTint: { value: new THREE.Color(galaxy.tint) },
          uOpacity: { value: 0 },
          uGain: { value: 1.45 }
        };
        const disc = new THREE.Mesh(
          new THREE.PlaneGeometry(galaxy.diameter / KLY, galaxy.diameter / KLY),
          new THREE.ShaderMaterial({
            uniforms: discUniforms,
            vertexShader: discVertex,
            fragmentShader: discFragment,
            transparent: true,
            depthWrite: false,
            side: THREE.DoubleSide,
            blending: THREE.AdditiveBlending
          })
        );
        disc.quaternion.setFromUnitVectors(new THREE.Vector3(0, 0, 1), new THREE.Vector3(...galaxy.normal).normalize());
        disc.position.copy(position);
        group.add(disc);
        uniforms.push(discUniforms);
      } else if (galaxy.kind === "irregular") {
        const count = galaxy.key === "lmc" ? 2_600 : 1_400;
        const spread = galaxy.diameter / KLY / 4;
        const positions = new Float32Array(count * 3);
        const colors = new Float32Array(count * 3);
        const sizes = new Float32Array(count);
        const tint = new THREE.Color(galaxy.tint);
        for (let i = 0; i < count; i++) {
          positions.set(
            [position.x + gaussian(random) * spread * 1.4, position.y + gaussian(random) * spread, position.z + gaussian(random) * spread * 0.6],
            i * 3
          );
          const pink = random() < 0.04;
          colors.set(pink ? [1.4, 0.6, 0.8] : [tint.r * 0.9, tint.g * 0.9, tint.b * 1.05], i * 3);
          sizes[i] = 1 + random() * 1.4;
        }
        const material = pointsMaterial(pixelRatio);
        irregular.add(new THREE.Points(pointsGeometry(positions, colors, sizes), material));
        irregularUniforms.push(material.uniforms as Layer["uniforms"][number]);
      } else {
        dwarfPositions.push(position.x, position.y, position.z);
        dwarfColors.push(0.85, 0.9, 1.1);
        dwarfSizes.push(2.4);
      }
      if (galaxy.name) {
        const visibility =
          galaxy.kind === "irregular" ? (L: number) => windowed(L, 4.6, 5.0, 5.7, 6.2) : (L: number) => windowed(L, 5.8, 6.2, 7.4, 7.9);
        this.labels.push({ key: galaxy.key, text: galaxy.name, kind: "major", position: galaxy.position, visibility });
      }
    }
    const dwarfMaterial = pointsMaterial(pixelRatio);
    group.add(new THREE.Points(pointsGeometry(new Float32Array(dwarfPositions), new Float32Array(dwarfColors), new Float32Array(dwarfSizes)), dwarfMaterial));
    uniforms.push(dwarfMaterial.uniforms as Layer["uniforms"][number]);
    this.addLayer(group, [0, 0, 0], KLY, (L) => windowed(L, 4.9, 5.7, 8.2, 8.9), uniforms);
    this.addLayer(irregular, [0, 0, 0], KLY, (L) => windowed(L, 4.3, 5.0, 5.9, 6.4), irregularUniforms);
  }

  private buildSurvey(assets: CosmosAssets, pixelRatio: number) {
    const { count, positions: raw, mags } = assets.survey;
    const positions = new Float32Array(count * 3);
    const colors = new Float32Array(count * 3);
    const sizes = new Float32Array(count);
    const random = seeded(2011);
    for (let i = 0; i < count; i++) {
      positions[i * 3] = raw[i * 3]! * 0.02;
      positions[i * 3 + 1] = raw[i * 3 + 1]! * 0.02;
      positions[i * 3 + 2] = raw[i * 3 + 2]! * 0.02;
      const k = mags[i]! / 10;
      const brightness = Math.min(1.6, Math.max(0.28, 1.25 - (k - 8) * 0.22));
      const warm = random();
      colors.set([brightness * (0.9 + warm * 0.15), brightness * (0.88 + warm * 0.04), brightness * (1.05 - warm * 0.2)], i * 3);
      sizes[i] = Math.min(4.2, Math.max(1.1, 3.4 - (k - 6) * 0.42));
    }
    const material = pointsMaterial(pixelRatio);
    const group = new THREE.Group();
    group.add(new THREE.Points(pointsGeometry(positions, colors, sizes), material));
    this.addLayer(group, [0, 0, 0], MPC_LY, (L) => windowed(L, 7.0, 7.8, 9.3, 9.9), [material.uniforms as Layer["uniforms"][number]]);
    this.labels.push({ key: "lg", text: "本星系群", kind: "major", position: [0, 0, 0], visibility: (L) => windowed(L, 7.5, 7.9, 8.9, 9.4) });
    for (const structure of STRUCTURES) {
      this.labels.push({ key: structure.key, text: structure.name, kind: "major", position: structure.position, visibility: (L) => windowed(L, 7.8, 8.2, 9.5, 10.0) });
    }
  }

  private buildWeb(assets: CosmosAssets, pixelRatio: number) {
    const { count, positions: raw } = assets.web;
    const positions = new Float32Array(count * 3);
    const colors = new Float32Array(count * 3);
    const sizes = new Float32Array(count).fill(1.3);
    for (let i = 0; i < count * 3; i++) positions[i] = raw[i]! * 0.0001;
    for (let i = 0; i < count; i++) colors.set([0.36, 0.44, 0.62], i * 3);
    const material = pointsMaterial(pixelRatio);
    const group = new THREE.Group();
    group.add(new THREE.Points(pointsGeometry(positions, colors, sizes), material));
    this.addLayer(group, [0, 0, 0], GLY, (L) => windowed(L, 8.9, 9.6, 10.45, 10.75), [material.uniforms as Layer["uniforms"][number]]);
  }

  setSky(texture: THREE.Texture) {
    const previous = this.skyMaterial.uniforms.uSky!.value as THREE.Texture;
    this.skyMaterial.uniforms.uSky!.value = texture;
    if (previous !== texture) previous.dispose();
  }

  resize(width: number, height: number) {
    this.camera.aspect = width / height;
    this.camera.updateProjectionMatrix();
    this.composer.setSize(width, height);
    this.bloom.resolution.set(width / 2, height / 2);
  }

  /** 每帧：按视距排布各图层并渲染。 */
  render(state: FrameState) {
    const D = 10 ** state.logDistance;
    const { focus } = state;

    for (const layer of this.layers) {
      const opacity = layer.opacity(state.logDistance);
      layer.group.visible = opacity > 0.002;
      if (!layer.group.visible) continue;
      for (const uniform of layer.uniforms) uniform.uOpacity.value = opacity;
      layer.group.position.set((layer.origin[0] - focus[0]) / D, (layer.origin[1] - focus[1]) / D, (layer.origin[2] - focus[2]) / D);
      layer.group.scale.setScalar(layer.unit / D);
    }

    // 可观测宇宙外壳：在内部时作淡淡的背景，在外部时是一颗完整的球
    const inside = D < OBSERVABLE_RADIUS_LY;
    this.cmbMaterial.side = inside ? THREE.BackSide : THREE.FrontSide;
    this.cmbMaterial.uniforms.uOpacity!.value = inside ? 0.12 * smoothstep(10.1, 10.6, state.logDistance) : 1;
    const cmbLayer = this.layers[this.layers.length - 1]!;
    cmbLayer.group.visible = state.logDistance > 9.85;

    this.skyMaterial.uniforms.uOpacity!.value = 1 - smoothstep(2.6, 3.7, state.logDistance);
    this.sky.visible = state.logDistance < 3.7;

    this.camera.up.copy(state.up);
    this.camera.position.copy(state.direction);
    this.camera.lookAt(0, 0, 0);
    this.sky.position.copy(this.camera.position);

    this.composer.render();
  }

  /** 光年坐标 → 屏幕像素；在相机背后或屏幕外时返回 null。 */
  project(position: Vec3, focus: Vec3, logDistance: number, width: number, height: number) {
    const D = 10 ** logDistance;
    this.projected.set((position[0] - focus[0]) / D, (position[1] - focus[1]) / D, (position[2] - focus[2]) / D);
    this.projected.project(this.camera);
    if (this.projected.z > 1 || this.projected.z < -1) return null;
    const x = (this.projected.x * 0.5 + 0.5) * width;
    const y = (-this.projected.y * 0.5 + 0.5) * height;
    if (x < -40 || y < -40 || x > width + 40 || y > height + 40) return null;
    return { x, y };
  }

  dispose() {
    this.scene.traverse((object) => {
      const mesh = object as THREE.Mesh;
      mesh.geometry?.dispose();
      const material = mesh.material as THREE.Material | THREE.Material[] | undefined;
      if (Array.isArray(material)) material.forEach((m) => m.dispose());
      else material?.dispose();
    });
    this.composer.dispose();
    this.bloom.dispose();
  }
}
