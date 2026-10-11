"use client";

import { memo, useEffect, useRef } from "react";
import * as THREE from "three";
import { cn } from "@/lib/utils";

/*
 * 概览首屏的写实地球。
 *
 * 与 background/dither 相同，不走 @react-three/fiber：在一个 effect 里手动建
 * WebGLRenderer，用原生 requestAnimationFrame 驱动，帧循环完全由这里掌控。
 *
 * 贴图全部来自 NASA Visible Earth（公有领域），由 scripts/build-earth-textures.py 生成：
 *   day    Blue Marble Next Generation 2004 年 10 月（地形与海底地形）
 *   night  Black Marble 2016 城市灯光
 *   clouds Blue Marble 合成云图
 *   water  由 Blue Marble 海洋色抠出的水体遮罩（海面高光）
 *   height GEBCO 2008 陆地高程（地形凹凸）
 * 地球在页面上只有几百像素宽，4K 已经比屏幕像素更密；8K 的解码与约 500MB 显存
 * 换不来可见的差别，所以只用 4K，并在后台线程解码（ImageBitmap），不卡主线程。
 *
 * 光照对应真实时间：太阳直射点按当前 UTC 时刻与日期计算，
 * 晨昏线、夜半球灯光都随时间推进；浏览器所在时区的中央经线画成一条细线，
 * 地球默认把它转到正面。
 */

const DEG = Math.PI / 180;
const TEXTURE_BASE = "/textures/earth";

/** 经纬度 → 单位球坐标。经度 0 朝 +z（相机方向），北极朝 +y。 */
function toVector(latRad: number, lngRad: number, radius = 1, target = new THREE.Vector3()) {
  const cos = Math.cos(latRad);
  return target.set(cos * Math.sin(lngRad) * radius, Math.sin(latRad) * radius, cos * Math.cos(lngRad) * radius);
}

/** 太阳直射点（弧度）。赤纬与时差用常见近似式，误差在一度以内。 */
function subsolarPoint(date: Date) {
  const startOfYear = Date.UTC(date.getUTCFullYear(), 0, 0);
  const dayOfYear = (date.getTime() - startOfYear) / 86_400_000;
  const declination = -23.44 * Math.cos(((2 * Math.PI) / 365) * (dayOfYear + 10));
  const b = ((2 * Math.PI) / 364) * (dayOfYear - 81);
  const equationOfTime = 9.87 * Math.sin(2 * b) - 7.53 * Math.cos(b) - 1.5 * Math.sin(b);
  const utcHours = date.getUTCHours() + date.getUTCMinutes() / 60 + date.getUTCSeconds() / 3600;
  let lng = (12 - utcHours - equationOfTime / 60) * 15;
  lng = ((((lng + 180) % 360) + 360) % 360) - 180;
  return { lat: declination * DEG, lng: lng * DEG };
}

/** 浏览器时区的中央经线（弧度）。 */
function localZoneLongitude(date: Date) {
  return (-date.getTimezoneOffset() / 60) * 15 * DEG;
}

const earthVertex = /* glsl */ `
varying vec2 vUv;
varying vec3 vWorldNormal;
varying vec3 vWorldPosition;
void main() {
  vUv = uv;
  vec4 world = modelMatrix * vec4(position, 1.0);
  vWorldPosition = world.xyz;
  vWorldNormal = normalize(mat3(modelMatrix) * normal);
  gl_Position = projectionMatrix * viewMatrix * world;
}
`;

const earthFragment = /* glsl */ `
uniform sampler2D uDay;
uniform sampler2D uNight;
uniform sampler2D uClouds;
uniform sampler2D uWater;
uniform sampler2D uHeight;
uniform vec3 uSun;
uniform float uCloudShift;
uniform float uBump;
uniform float uReveal;
uniform vec3 uAtmosphere;
varying vec2 vUv;
varying vec3 vWorldNormal;
varying vec3 vWorldPosition;

// 与 three.js bumpmap 相同的屏幕空间法线扰动
vec3 perturbNormal(vec3 position, vec3 normal, float scale) {
  vec2 dSTdx = dFdx(vUv);
  vec2 dSTdy = dFdy(vUv);
  float h = texture2D(uHeight, vUv).r;
  float dBx = scale * (texture2D(uHeight, vUv + dSTdx).r - h);
  float dBy = scale * (texture2D(uHeight, vUv + dSTdy).r - h);
  vec3 sigmaX = dFdx(position);
  vec3 sigmaY = dFdy(position);
  vec3 r1 = cross(sigmaY, normal);
  vec3 r2 = cross(normal, sigmaX);
  float det = dot(sigmaX, r1);
  vec3 grad = sign(det) * (dBx * r1 + dBy * r2);
  return normalize(abs(det) * normal - grad);
}

void main() {
  vec3 n = normalize(vWorldNormal);
  vec3 view = normalize(cameraPosition - vWorldPosition);
  float water = texture2D(uWater, vUv).r;
  vec3 bumped = perturbNormal(vWorldPosition, n, uBump * (1.0 - water));

  float sunGeo = dot(n, uSun);
  float daylight = smoothstep(-0.16, 0.2, sunGeo);
  float diffuse = clamp(dot(bumped, uSun) * 1.05 + 0.04, 0.0, 1.0);

  vec3 day = texture2D(uDay, vUv).rgb;
  vec2 cloudUv = vec2(vUv.x + uCloudShift, vUv.y);
  float cloud = smoothstep(0.16, 0.86, texture2D(uClouds, cloudUv).r);
  // 云影：沿太阳方向略微错开采样
  float cloudShadow = smoothstep(0.16, 0.86, texture2D(uClouds, cloudUv + vec2(0.0018, -0.0012)).r);

  vec3 surface = day * diffuse * (1.0 - 0.38 * cloudShadow);

  // 海面高光
  vec3 halfway = normalize(uSun + view);
  float spec = pow(max(dot(n, halfway), 0.0), 70.0) * water * (1.0 - cloud) * daylight;
  surface += vec3(1.0, 0.94, 0.82) * spec * 0.85;

  // 云层：受光面偏白，晨昏线附近染一点暖色
  float twilight = exp(-pow(sunGeo / 0.16, 2.0));
  vec3 cloudLit = mix(vec3(0.96), vec3(1.0, 0.72, 0.5), twilight * 0.55) * clamp(sunGeo * 1.2 + 0.12, 0.0, 1.0);
  surface = mix(surface, cloudLit, cloud * 0.92);

  // 夜半球的城市灯光，云层会挡掉一部分
  vec3 lights = pow(texture2D(uNight, vUv).rgb, vec3(1.9)) * vec3(2.6, 2.1, 1.5);
  float night = 1.0 - smoothstep(-0.22, 0.04, sunGeo);
  surface += lights * night * (1.0 - cloud * 0.75);

  // 晨昏线散射与边缘大气
  surface += vec3(1.0, 0.45, 0.18) * twilight * 0.06;
  float rim = pow(1.0 - max(dot(n, view), 0.0), 3.0);
  surface += uAtmosphere * rim * (0.15 + 0.85 * smoothstep(-0.35, 0.5, sunGeo)) * 0.9;

  gl_FragColor = vec4(surface, uReveal);
  #include <tonemapping_fragment>
  #include <colorspace_fragment>
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
uniform vec3 uColor;
uniform vec3 uSun;
uniform float uIntensity;
uniform float uReveal;
varying vec3 vNormal;
varying vec3 vWorldNormal;
void main() {
  float rim = pow(clamp(0.74 - dot(vNormal, vec3(0.0, 0.0, 1.0)), 0.0, 1.0), 3.2);
  float lit = 0.25 + 0.75 * smoothstep(-0.45, 0.55, dot(vWorldNormal, uSun));
  gl_FragColor = vec4(uColor, clamp(rim * lit * uIntensity * uReveal, 0.0, 1.0));
}
`;

/** 后台线程解码贴图；不支持 createImageBitmap 的浏览器退回 TextureLoader。 */
function createTextureLoader() {
  if (typeof createImageBitmap === "function") {
    const loader = new THREE.ImageBitmapLoader();
    loader.setOptions({ imageOrientation: "flipY", premultiplyAlpha: "none" });
    return async (url: string) => {
      const bitmap = await loader.loadAsync(url);
      const texture = new THREE.Texture(bitmap);
      texture.flipY = false;
      texture.needsUpdate = true;
      return texture;
    };
  }
  const loader = new THREE.TextureLoader();
  return (url: string) => loader.loadAsync(url);
}

async function loadTexture(load: (url: string) => Promise<THREE.Texture>, url: string, color: boolean, anisotropy: number) {
  const texture = await load(url);
  texture.colorSpace = color ? THREE.SRGBColorSpace : THREE.NoColorSpace;
  texture.anisotropy = anisotropy;
  texture.wrapS = THREE.RepeatWrapping;
  return texture;
}

/** 帧率上限：缓慢自转与云层漂移 30 帧已足够顺滑，入场动画期间放开到满帧。 */
const FRAME_INTERVAL = 1000 / 30;

export const HeroGlobe = memo(function HeroGlobe({
  theme,
  reducedMotion,
  onReady,
  className
}: {
  theme: "dark" | "light";
  reducedMotion: boolean;
  onReady?: () => void;
  className?: string;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const themeRef = useRef(theme);
  const onReadyRef = useRef(onReady);
  const applyThemeRef = useRef<((theme: "dark" | "light") => void) | null>(null);

  useEffect(() => {
    onReadyRef.current = onReady;
  }, [onReady]);

  useEffect(() => {
    themeRef.current = theme;
    applyThemeRef.current?.(theme);
  }, [theme]);

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    let renderer: THREE.WebGLRenderer;
    try {
      // 高分屏本身足够细，多重采样只在 1 倍屏上开
      renderer = new THREE.WebGLRenderer({ antialias: (window.devicePixelRatio || 1) < 1.5, alpha: true, powerPreference: "high-performance" });
    } catch {
      return; // 没有 WebGL：首屏照常显示文字与指标，只是没有地球
    }
    const motion = reducedMotion ? 0 : 1;
    renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 1.5));
    renderer.setClearColor(0x000000, 0);
    renderer.toneMapping = THREE.ACESFilmicToneMapping;
    renderer.toneMappingExposure = 1.05;
    renderer.outputColorSpace = THREE.SRGBColorSpace;
    renderer.domElement.style.width = "100%";
    renderer.domElement.style.height = "100%";
    renderer.domElement.style.display = "block";
    container.appendChild(renderer.domElement);

    // 帧循环状态放在最前：下面的主题初始化就会请求重画
    let frame = 0;
    let queued = 0;
    let visible = true;
    let pageVisible = !document.hidden;
    let disposed = false;

    const scene = new THREE.Scene();
    const camera = new THREE.PerspectiveCamera(26, 1, 0.1, 100);

    // 外层倾斜让北半球朝向观众，内层自转到本地时区
    const tilt = new THREE.Group();
    tilt.rotation.x = 0.3;
    tilt.position.y = -0.12;
    const globe = new THREE.Group();
    tilt.add(globe);
    scene.add(tilt);

    const placeholder = new THREE.Texture();
    const earthUniforms = {
      uDay: { value: placeholder as THREE.Texture },
      uNight: { value: placeholder as THREE.Texture },
      uClouds: { value: placeholder as THREE.Texture },
      uWater: { value: placeholder as THREE.Texture },
      uHeight: { value: placeholder as THREE.Texture },
      uSun: { value: new THREE.Vector3(0, 0, 1) },
      uCloudShift: { value: 0 },
      uBump: { value: 0.9 },
      uReveal: { value: 0 },
      uAtmosphere: { value: new THREE.Color("#4f8cff") }
    };
    const earthMaterial = new THREE.ShaderMaterial({
      uniforms: earthUniforms,
      vertexShader: earthVertex,
      fragmentShader: earthFragment,
      transparent: true
    });
    // SphereGeometry 的经度 0 在 +x，转 -90° 对齐到 +z
    const earth = new THREE.Mesh(new THREE.SphereGeometry(1, 128, 96), earthMaterial);
    earth.rotation.y = -Math.PI / 2;
    globe.add(earth);

    const haloUniforms = {
      uColor: { value: new THREE.Color("#3d7bff") },
      uSun: { value: new THREE.Vector3(0, 0, 1) },
      uIntensity: { value: 1.25 },
      uReveal: { value: 0 }
    };
    const haloMaterial = new THREE.ShaderMaterial({
      uniforms: haloUniforms,
      vertexShader: haloVertex,
      fragmentShader: haloFragment,
      side: THREE.BackSide,
      transparent: true,
      depthWrite: false,
      blending: THREE.AdditiveBlending
    });
    const halo = new THREE.Mesh(new THREE.SphereGeometry(1.16, 64, 48), haloMaterial);
    // 地球先画并写深度，挡住辉光球正对镜头的那片背面
    earth.renderOrder = 0;
    halo.renderOrder = 1;
    tilt.add(halo);

    // 本地时区中央经线
    const now = new Date();
    const zoneLng = localZoneLongitude(now);
    const meridianPoints: THREE.Vector3[] = [];
    for (let i = 0; i <= 128; i++) meridianPoints.push(toVector(-Math.PI / 2 + (i / 128) * Math.PI, zoneLng, 1.004));
    const meridianMaterial = new THREE.LineBasicMaterial({ color: "#7dd3fc", transparent: true, opacity: 0, depthWrite: false });
    const meridian = new THREE.Line(new THREE.BufferGeometry().setFromPoints(meridianPoints), meridianMaterial);
    globe.add(meridian);

    // 轨道环与一颗绕行的卫星
    const orbit = new THREE.Group();
    orbit.rotation.set(1.2, 0.18, -0.42);
    const ringPoints: THREE.Vector3[] = [];
    for (let i = 0; i <= 192; i++) {
      const a = (i / 192) * Math.PI * 2;
      ringPoints.push(new THREE.Vector3(Math.cos(a) * 1.34, Math.sin(a) * 1.34, 0));
    }
    const ringMaterial = new THREE.LineBasicMaterial({ color: "#a1a1aa", transparent: true, opacity: 0, depthWrite: false });
    orbit.add(new THREE.Line(new THREE.BufferGeometry().setFromPoints(ringPoints), ringMaterial));
    const satelliteMaterial = new THREE.MeshBasicMaterial({ color: "#e0f2fe", transparent: true, opacity: 0 });
    const satellite = new THREE.Mesh(new THREE.SphereGeometry(0.016, 16, 16), satelliteMaterial);
    orbit.add(satellite);
    tilt.add(orbit);

    const applyTheme = (name: "dark" | "light") => {
      // 浅色底上加法混合的辉光会被吃掉，改用普通混合的深一点的蓝
      haloUniforms.uColor.value.set(name === "dark" ? "#3d7bff" : "#3b82f6");
      haloUniforms.uIntensity.value = name === "dark" ? 1.25 : 0.7;
      haloMaterial.blending = name === "dark" ? THREE.AdditiveBlending : THREE.NormalBlending;
      haloMaterial.needsUpdate = true;
      ringMaterial.color.set(name === "dark" ? "#a1a1aa" : "#52525b");
      meridianMaterial.color.set(name === "dark" ? "#7dd3fc" : "#0284c7");
      satelliteMaterial.color.set(name === "dark" ? "#e0f2fe" : "#0369a1");
      requestRender();
    };
    applyThemeRef.current = applyTheme;
    applyTheme(themeRef.current);

    // ── 贴图 ──
    let ready = false;
    let revealStart = 0;
    const owned: THREE.Texture[] = [];
    const load = createTextureLoader();
    const anisotropy = Math.min(4, renderer.capabilities.getMaxAnisotropy());

    Promise.all(
      [
        ["day-4k.webp", true],
        ["night-4k.webp", true],
        ["clouds-4k.webp", false],
        ["water-4k.png", false],
        ["height-4k.jpg", false]
      ].map(([file, color]) => loadTexture(load, `${TEXTURE_BASE}/${file}`, color as boolean, anisotropy))
    )
      .then((textures) => {
        if (disposed) {
          textures.forEach((texture) => texture.dispose());
          return;
        }
        const [day, night, clouds, water, height] = textures as [THREE.Texture, THREE.Texture, THREE.Texture, THREE.Texture, THREE.Texture];
        earthUniforms.uDay.value = day;
        earthUniforms.uNight.value = night;
        earthUniforms.uClouds.value = clouds;
        earthUniforms.uWater.value = water;
        earthUniforms.uHeight.value = height;
        owned.push(...textures);
        // 先上传到显卡，揭幕时不卡第一帧
        textures.forEach((texture) => renderer.initTexture(texture));
        ready = true;
        revealStart = performance.now();
        onReadyRef.current?.();
        requestRender();
      })
      .catch(() => {
        /* 贴图加载失败时不显示地球，首屏其他部分不受影响 */
      });

    // ── 交互：在地球区域内移动指针时轻微偏转 ──
    const pointer = { x: 0, y: 0, tx: 0, ty: 0 };
    const hoverTarget = container.parentElement ?? container;
    const onPointerMove = (event: PointerEvent) => {
      const rect = container.getBoundingClientRect();
      pointer.tx = Math.max(-1, Math.min(1, ((event.clientX - rect.left) / rect.width - 0.5) * 2));
      pointer.ty = Math.max(-1, Math.min(1, ((event.clientY - rect.top) / rect.height - 0.5) * 2));
    };
    const onPointerLeave = () => {
      pointer.tx = 0;
      pointer.ty = 0;
    };
    if (motion) {
      hoverTarget.addEventListener("pointermove", onPointerMove);
      hoverTarget.addEventListener("pointerleave", onPointerLeave);
    }

    const resize = () => {
      const width = Math.max(1, container.clientWidth);
      const height = Math.max(1, container.clientHeight);
      renderer.setSize(width, height, false);
      camera.aspect = width / height;
      // 窄而高的容器里按宽度收住，地球不被左右裁掉
      camera.zoom = Math.min(1, camera.aspect / 0.95);
      camera.updateProjectionMatrix();
      requestRender();
    };
    const resizeObserver = new ResizeObserver(resize);
    resizeObserver.observe(container);

    const sunLocal = new THREE.Vector3();
    const globeQuaternion = new THREE.Quaternion();
    const startedAt = performance.now();
    const baseYaw = -zoneLng;
    const restDistance = 4.15;

    const renderOnce = (time: number) => {
      const t = (time - startedAt) / 1000;
      const progress = !ready ? 0 : reducedMotion ? 1 : Math.min(1, (time - revealStart) / 2200);
      const eased = 1 - Math.pow(1 - progress, 4);

      pointer.x += (pointer.tx - pointer.x) * 0.05;
      pointer.y += (pointer.ty - pointer.y) * 0.05;

      globe.rotation.y = baseYaw + motion * (Math.sin(t * 0.06) * 0.35 + pointer.x * 0.2) + (1 - eased) * 1.1;
      tilt.rotation.x = 0.3 + motion * pointer.y * 0.07;
      camera.position.set(0, 0, restDistance + (1 - eased) * 1.6);
      camera.lookAt(0, 0, 0);

      earthUniforms.uReveal.value = eased;
      haloUniforms.uReveal.value = eased;
      earthUniforms.uCloudShift.value = motion * t * 0.0009;
      meridianMaterial.opacity = (0.28 + 0.22 * (0.5 + 0.5 * Math.sin(t * 2)) * motion) * eased;
      ringMaterial.opacity = 0.16 * eased;
      satelliteMaterial.opacity = eased;

      const angle = t * 0.3;
      satellite.position.set(Math.cos(angle) * 1.34, Math.sin(angle) * 1.34, 0);

      const sun = subsolarPoint(new Date());
      toVector(sun.lat, sun.lng, 1, sunLocal);
      globe.updateWorldMatrix(true, false);
      globe.getWorldQuaternion(globeQuaternion);
      sunLocal.applyQuaternion(globeQuaternion);
      earthUniforms.uSun.value.copy(sunLocal);
      haloUniforms.uSun.value.copy(sunLocal);

      renderer.render(scene, camera);
      return ready && progress < 1;
    };

    // ── 帧循环：离开视野或标签页隐藏时暂停 ──
    const active = () => visible && pageVisible && !disposed;

    let lastFrame = 0;
    let introRunning = true;
    const loop = (time: number) => {
      frame = 0;
      // 入场动画满帧，之后限到 30 帧；跳过的帧不渲染，只续排下一帧
      if (introRunning || time - lastFrame >= FRAME_INTERVAL - 1) {
        lastFrame = time;
        introRunning = renderOnce(time);
      }
      // 贴图到位前不空转
      if (active() && ready && (motion || introRunning)) frame = requestAnimationFrame(loop);
    };
    function requestRender() {
      if (frame || queued || disposed) return;
      queued = requestAnimationFrame((time) => {
        queued = 0;
        if (!frame) loop(time);
      });
    }
    const stopLoop = () => {
      cancelAnimationFrame(frame);
      cancelAnimationFrame(queued);
      frame = 0;
      queued = 0;
    };

    const intersection = new IntersectionObserver(([entry]) => {
      visible = Boolean(entry?.isIntersecting);
      if (visible) requestRender();
      else stopLoop();
    });
    intersection.observe(container);
    const onVisibility = () => {
      pageVisible = !document.hidden;
      if (pageVisible) requestRender();
      else stopLoop();
    };
    document.addEventListener("visibilitychange", onVisibility);

    // 静态模式下每分钟重画一次，晨昏线仍随真实时间推进
    const staticTimer = motion ? 0 : window.setInterval(requestRender, 60_000);

    resize();

    return () => {
      disposed = true;
      applyThemeRef.current = null;
      stopLoop();
      window.clearInterval(staticTimer);
      intersection.disconnect();
      resizeObserver.disconnect();
      document.removeEventListener("visibilitychange", onVisibility);
      hoverTarget.removeEventListener("pointermove", onPointerMove);
      hoverTarget.removeEventListener("pointerleave", onPointerLeave);
      owned.forEach((texture) => texture.dispose());
      scene.traverse((object) => {
        const mesh = object as THREE.Mesh;
        mesh.geometry?.dispose();
        const material = mesh.material as THREE.Material | THREE.Material[] | undefined;
        if (Array.isArray(material)) material.forEach((m) => m.dispose());
        else material?.dispose();
      });
      renderer.dispose();
      renderer.forceContextLoss();
      renderer.domElement.remove();
    };
  }, [reducedMotion]);

  return <div ref={containerRef} aria-hidden className={cn("pointer-events-none", className)} />;
});
