"use client";

import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { X } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import * as THREE from "three";
import { cn } from "@/lib/utils";
import { CosmosScene, type CosmosAssets, type FrameState } from "./cosmos-scene";
import {
  DEG,
  MAX_LOG,
  MIN_LOG,
  STAGES,
  focusAt,
  formatDistance,
  niceLength,
  stageAt,
  type Vec3
} from "./cosmos-math";

/*
 * 宇宙彩蛋：从地球一路缩小到可观测宇宙。
 *
 * 打开后先自动飞到银河系，之后滚轮或双指缩放继续向外，拖动旋转，右侧阶段列表可直达。
 * 资源只在打开时加载（约 12MB），关闭即释放 WebGL 上下文。
 */

const EARTH_BASE = "/textures/earth";
const COSMOS_BASE = "/cosmos";

function bitmapLoader() {
  const loader = new THREE.ImageBitmapLoader();
  loader.setOptions({ imageOrientation: "flipY", premultiplyAlpha: "none" });
  return async (url: string, options: { color: boolean; mipmaps?: boolean }) => {
    const bitmap = await loader.loadAsync(url);
    const texture = new THREE.Texture(bitmap);
    texture.flipY = false;
    texture.colorSpace = options.color ? THREE.SRGBColorSpace : THREE.NoColorSpace;
    texture.wrapS = THREE.RepeatWrapping;
    if (options.mipmaps === false) {
      // 全天图在经度接缝处导数跳变，开 mipmap 会出现一条竖线
      texture.generateMipmaps = false;
      texture.minFilter = THREE.LinearFilter;
    } else {
      texture.anisotropy = 4;
    }
    texture.needsUpdate = true;
    return { texture, bitmap };
  };
}

async function loadBinary(url: string) {
  const response = await fetch(url);
  if (!response.ok) throw new Error(`${url}: ${response.status}`);
  return response.arrayBuffer();
}

async function loadAssets(onProgress: (value: number) => void): Promise<CosmosAssets> {
  const load = bitmapLoader();
  let done = 0;
  const total = 10;
  const track = <T,>(promise: Promise<T>) =>
    promise.then((value) => {
      done++;
      onProgress(done / total);
      return value;
    });

  const [earthDay, earthNight, earthClouds, earthWater, sky, milkyWay, cmb, surveyBuffer, webBuffer] = await Promise.all([
    track(load(`${EARTH_BASE}/day-4k.webp`, { color: true })),
    track(load(`${EARTH_BASE}/night-4k.webp`, { color: true })),
    track(load(`${EARTH_BASE}/clouds-4k.webp`, { color: false })),
    track(load(`${EARTH_BASE}/water-4k.png`, { color: false })),
    track(load(`${COSMOS_BASE}/sky-4k.webp`, { color: true, mipmaps: false })),
    track(load(`${COSMOS_BASE}/milkyway-4k.webp`, { color: true })),
    track(load(`${COSMOS_BASE}/cmb-4k.webp`, { color: true, mipmaps: false })),
    track(loadBinary(`${COSMOS_BASE}/2mrs.bin`)),
    track(loadBinary(`${COSMOS_BASE}/cosmic-web.bin`))
  ]);

  // 银河系俯视图缩成 512² 采样恒星分布。位图已上下翻转，行 0 是图像底部。
  const canvas = document.createElement("canvas");
  canvas.width = 512;
  canvas.height = 512;
  const context = canvas.getContext("2d", { willReadFrequently: true })!;
  context.drawImage(milkyWay.bitmap, 0, 0, 512, 512);
  const milkyWayPixels = context.getImageData(0, 0, 512, 512);
  done++;
  onProgress(1);

  const surveyCount = new DataView(surveyBuffer).getUint32(0, true);
  const webCount = new DataView(webBuffer).getUint32(0, true);
  return {
    earthDay: earthDay.texture,
    earthNight: earthNight.texture,
    earthClouds: earthClouds.texture,
    earthWater: earthWater.texture,
    sky: sky.texture,
    milkyWay: milkyWay.texture,
    milkyWayPixels,
    cmb: cmb.texture,
    survey: {
      count: surveyCount,
      positions: new Int16Array(surveyBuffer.slice(4, 4 + surveyCount * 6)),
      mags: new Uint8Array(surveyBuffer, 4 + surveyCount * 6, surveyCount)
    },
    web: { count: webCount, positions: new Int16Array(webBuffer.slice(4, 4 + webCount * 6)) }
  };
}

const easeInOutCubic = (t: number) => (t < 0.5 ? 4 * t * t * t : 1 - Math.pow(-2 * t + 2, 3) / 2);

type Flight = { start: number; duration: number; from: number; to: number; dirFrom?: THREE.Vector3; dirTo?: THREE.Vector3; upFrom?: THREE.Vector3 };

const Z_UP = new THREE.Vector3(0, 0, 1);

function directionFromAngles(azimuth: number, elevation: number, target = new THREE.Vector3()) {
  return target.set(Math.cos(elevation) * Math.cos(azimuth), Math.cos(elevation) * Math.sin(azimuth), Math.sin(elevation));
}

export default function CosmosJourney({ zoneLongitude, onClose }: { zoneLongitude: number; onClose: () => void }) {
  const hostRef = useRef<HTMLDivElement>(null);
  const labelsRef = useRef<HTMLDivElement>(null);
  const scaleBarRef = useRef<HTMLDivElement>(null);
  const scaleTextRef = useRef<HTMLSpanElement>(null);
  const flyToRef = useRef<((log: number) => void) | null>(null);
  const [progress, setProgress] = useState(0);
  const [ready, setReady] = useState(false);
  const [failed, setFailed] = useState(false);
  const [stageKey, setStageKey] = useState(STAGES[0]!.key);
  const [labels, setLabels] = useState<CosmosScene["labels"]>([]);
  const [closing, setClosing] = useState(false);
  const onCloseRef = useRef(onClose);

  useEffect(() => {
    onCloseRef.current = onClose;
  }, [onClose]);

  const close = () => setClosing(true);

  useEffect(() => {
    if (!closing) return;
    const id = window.setTimeout(() => onCloseRef.current(), 450);
    return () => window.clearTimeout(id);
  }, [closing]);

  // 打开期间锁住页面滚动
  useEffect(() => {
    const previous = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") setClosing(true);
    };
    window.addEventListener("keydown", onKey);
    return () => {
      document.body.style.overflow = previous;
      window.removeEventListener("keydown", onKey);
    };
  }, []);

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    let disposed = false;
    let frame = 0;
    let renderer: THREE.WebGLRenderer | null = null;
    let cosmos: CosmosScene | null = null;
    const cleanups: Array<() => void> = [];

    try {
      renderer = new THREE.WebGLRenderer({ antialias: false, powerPreference: "high-performance" });
    } catch {
      queueMicrotask(() => setFailed(true));
      return;
    }
    renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 1.5));
    renderer.setClearColor(0x000000, 1);
    renderer.toneMapping = THREE.ACESFilmicToneMapping;
    renderer.toneMappingExposure = 1.0;
    renderer.domElement.style.display = "block";
    renderer.domElement.style.width = "100%";
    renderer.domElement.style.height = "100%";
    renderer.domElement.style.touchAction = "none";
    host.appendChild(renderer.domElement);

    const size = () => ({ width: host.clientWidth || window.innerWidth, height: host.clientHeight || window.innerHeight });
    const resize = () => {
      const { width, height } = size();
      renderer!.setSize(width, height, false);
      cosmos?.resize(width, height);
    };
    resize();
    window.addEventListener("resize", resize);
    cleanups.push(() => window.removeEventListener("resize", resize));

    loadAssets((value) => !disposed && setProgress(value))
      .then((assets) => {
        if (disposed) return;
        const date = new Date();
        cosmos = new CosmosScene(renderer!, assets, date);
        resize();
        setLabels([...cosmos.labels]);
        start(cosmos, date);
        setReady(true);

        // 星空先用 4K，空闲后换 8K
        const upgrade = () => {
          bitmapLoader()(`${COSMOS_BASE}/sky-8k.webp`, { color: true, mipmaps: false })
            .then(({ texture }) => {
              if (disposed || !cosmos) return texture.dispose();
              renderer!.initTexture(texture);
              cosmos.setSky(texture);
            })
            .catch(() => undefined);
        };
        const idle = window.setTimeout(upgrade, 2500);
        cleanups.push(() => window.clearTimeout(idle));
      })
      .catch(() => !disposed && setFailed(true));

    function start(scene: CosmosScene, date: Date) {
      // ── 视角状态 ──
      let log = STAGES[0]!.target;
      let targetLog = log;
      let azimuth = Math.PI;
      let elevation = 50 * DEG;
      const direction = new THREE.Vector3();
      const up = new THREE.Vector3();
      let flight: Flight | null = null;
      let manual = false;

      // 起始视角与首屏地球一致：本地时区中央经线、北纬 17° 朝向镜头，上方为北天极
      const startDirection = new THREE.Vector3(
        Math.cos(17 * DEG) * Math.sin(zoneLongitude),
        Math.sin(17 * DEG),
        Math.cos(17 * DEG) * Math.cos(zoneLongitude)
      ).applyQuaternion(scene.earthRotation);
      const startUp = new THREE.Vector3(0, 1, 0).applyQuaternion(scene.earthRotation);
      direction.copy(startDirection);
      up.copy(startUp);

      const milkyWay = STAGES.find((stage) => stage.key === "milkyway")!;
      const autopilotTarget = directionFromAngles(Math.PI, 50 * DEG);
      flight = {
        start: performance.now() + 700,
        duration: 9000,
        from: log,
        to: milkyWay.target,
        dirFrom: startDirection.clone(),
        dirTo: autopilotTarget,
        upFrom: startUp.clone()
      };

      const takeControl = () => {
        if (manual) return;
        manual = true;
        flight = null;
        // 从当前方向接管，之后以北银极为上
        const d = direction.clone().normalize();
        elevation = Math.asin(Math.max(-1, Math.min(1, d.z)));
        azimuth = Math.atan2(d.y, d.x);
        targetLog = log;
      };

      flyToRef.current = (next: number) => {
        takeControl();
        flight = { start: performance.now(), duration: 2600, from: log, to: next };
      };

      // ── 输入 ──
      const canvas = renderer!.domElement;
      const onWheel = (event: WheelEvent) => {
        event.preventDefault();
        takeControl();
        flight = null;
        const scale = event.ctrlKey ? 0.012 : event.deltaMode === 1 ? 0.08 : 0.0024;
        targetLog = Math.min(MAX_LOG, Math.max(MIN_LOG, targetLog + event.deltaY * scale));
      };
      canvas.addEventListener("wheel", onWheel, { passive: false });

      const pointers = new Map<number, { x: number; y: number }>();
      let pinchDistance = 0;
      const onPointerDown = (event: PointerEvent) => {
        canvas.setPointerCapture(event.pointerId);
        pointers.set(event.pointerId, { x: event.clientX, y: event.clientY });
        if (pointers.size === 2) {
          const [a, b] = [...pointers.values()];
          pinchDistance = Math.hypot(a!.x - b!.x, a!.y - b!.y);
        }
      };
      const onPointerMove = (event: PointerEvent) => {
        const previous = pointers.get(event.pointerId);
        if (!previous) return;
        const current = { x: event.clientX, y: event.clientY };
        pointers.set(event.pointerId, current);
        takeControl();
        if (pointers.size === 2) {
          const [a, b] = [...pointers.values()];
          const distance = Math.hypot(a!.x - b!.x, a!.y - b!.y);
          if (pinchDistance > 0) targetLog = Math.min(MAX_LOG, Math.max(MIN_LOG, targetLog + Math.log10(pinchDistance / distance) * 2.2));
          pinchDistance = distance;
          flight = null;
          return;
        }
        azimuth -= (current.x - previous.x) * 0.005;
        elevation = Math.max(-1.45, Math.min(1.45, elevation + (current.y - previous.y) * 0.005));
      };
      const onPointerUp = (event: PointerEvent) => {
        pointers.delete(event.pointerId);
        if (pointers.size < 2) pinchDistance = 0;
      };
      canvas.addEventListener("pointerdown", onPointerDown);
      canvas.addEventListener("pointermove", onPointerMove);
      canvas.addEventListener("pointerup", onPointerUp);
      canvas.addEventListener("pointercancel", onPointerUp);
      cleanups.push(() => {
        canvas.removeEventListener("wheel", onWheel);
        canvas.removeEventListener("pointerdown", onPointerDown);
        canvas.removeEventListener("pointermove", onPointerMove);
        canvas.removeEventListener("pointerup", onPointerUp);
        canvas.removeEventListener("pointercancel", onPointerUp);
      });

      // ── 帧循环 ──
      const earth = scene.earthPosition;
      const state: FrameState = { logDistance: log, focus: [0, 0, 0], direction, up, time: 0 };
      let lastStage = "";
      let lastScale = "";
      let previousTime = performance.now();
      const labelNodes = () => Array.from(labelsRef.current?.children ?? []) as HTMLElement[];

      const tick = (now: number) => {
        frame = requestAnimationFrame(tick);
        const dt = Math.min(0.05, (now - previousTime) / 1000);
        previousTime = now;

        if (flight) {
          const t = Math.min(1, Math.max(0, (now - flight.start) / flight.duration));
          const eased = easeInOutCubic(t);
          log = flight.from + (flight.to - flight.from) * eased;
          targetLog = log;
          if (flight.dirFrom && flight.dirTo && flight.upFrom) {
            // 方向在后半程才转到俯视银河，近地时保持首屏的视角
            const turn = easeInOutCubic(Math.min(1, Math.max(0, (t - 0.35) / 0.55)));
            const rotation = new THREE.Quaternion().setFromUnitVectors(flight.dirFrom, flight.dirTo);
            direction.copy(flight.dirFrom).applyQuaternion(new THREE.Quaternion().slerp(rotation, turn));
            const upRotation = new THREE.Quaternion().setFromUnitVectors(flight.upFrom.clone().normalize(), Z_UP);
            up.copy(flight.upFrom).applyQuaternion(new THREE.Quaternion().slerp(upRotation, turn)).normalize();
          }
          if (t >= 1) {
            const finished = flight;
            flight = null;
            if (finished.dirTo) {
              azimuth = Math.atan2(finished.dirTo.y, finished.dirTo.x);
              elevation = Math.asin(finished.dirTo.z);
              manual = true;
            }
          }
        } else {
          log += (targetLog - log) * (1 - Math.exp(-dt * 7));
        }
        if (manual && !flight?.dirFrom) {
          directionFromAngles(azimuth, elevation, direction);
          up.copy(Z_UP);
        }

        state.logDistance = log;
        state.focus = focusAt(log, earth);
        state.time = now / 1000;
        scene.render(state);

        // HUD：阶段、比例尺、标注
        const stage = stageAt(log).key;
        if (stage !== lastStage) {
          lastStage = stage;
          setStageKey(stage);
        }
        const { width, height } = size();
        const lyPerPixel = (2 * Math.tan((scene.camera.fov * DEG) / 2) * 10 ** log) / height;
        const length = niceLength(lyPerPixel * 140);
        const text = formatDistance(length);
        if (scaleBarRef.current) scaleBarRef.current.style.width = `${length / lyPerPixel}px`;
        if (text !== lastScale && scaleTextRef.current) {
          lastScale = text;
          scaleTextRef.current.textContent = text;
        }
        const nodes = labelNodes();
        scene.labels.forEach((label, index) => {
          const node = nodes[index];
          if (!node) return;
          const opacity = label.visibility(log);
          const point = opacity > 0.01 ? scene.project(label.position as Vec3, state.focus, log, width, height) : null;
          if (!point) {
            node.style.opacity = "0";
            return;
          }
          node.style.opacity = String(opacity);
          node.style.transform = `translate3d(${point.x}px, ${point.y}px, 0)`;
        });
      };
      frame = requestAnimationFrame(tick);
    }

    return () => {
      disposed = true;
      cancelAnimationFrame(frame);
      cleanups.forEach((cleanup) => cleanup());
      flyToRef.current = null;
      cosmos?.dispose();
      renderer?.dispose();
      renderer?.forceContextLoss();
      renderer?.domElement.remove();
    };
  }, [zoneLongitude]);

  const stage = STAGES.find((item) => item.key === stageKey) ?? STAGES[0]!;

  return createPortal(
    <motion.div
      className="fixed inset-0 z-[200] bg-black text-white select-none"
      initial={{ opacity: 0 }}
      animate={{ opacity: closing ? 0 : 1 }}
      transition={{ duration: 0.45, ease: [0.22, 1, 0.36, 1] }}
      role="dialog"
      aria-label="宇宙"
    >
      <div ref={hostRef} className="absolute inset-0" />

      {/* 天体标注：位置每帧由画布投影更新 */}
      <div ref={labelsRef} className="pointer-events-none absolute inset-0 overflow-hidden" aria-hidden>
        {labels.map((label) => (
          <span key={label.key} className="absolute top-0 left-0 opacity-0 will-change-transform">
            <span
              className={cn(
                "flex -translate-y-1/2 items-center gap-1.5 whitespace-nowrap",
                label.kind === "major" ? "text-xs font-medium text-white/85" : "text-[11px] text-white/60"
              )}
            >
              <span className={cn("rounded-full bg-white/70", label.kind === "major" ? "size-1.5" : "size-1")} />
              {label.text}
            </span>
          </span>
        ))}
      </div>

      {/* 阶段标题 */}
      <div className="pointer-events-none absolute top-6 left-6 max-w-[min(32rem,70vw)] md:top-8 md:left-10">
        <AnimatePresence mode="wait">
          <motion.div
            key={stage.key}
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: ready ? 1 : 0, y: 0 }}
            exit={{ opacity: 0, y: -6 }}
            transition={{ duration: 0.35, ease: [0.22, 1, 0.36, 1] }}
            className="space-y-1.5"
          >
            <h2 className="text-2xl font-semibold tracking-tight md:text-3xl">{stage.title}</h2>
            <p className="text-sm text-white/60">{stage.subtitle}</p>
          </motion.div>
        </AnimatePresence>
      </div>

      {/* 尺度阶梯 */}
      <nav aria-label="尺度" className={cn("absolute top-1/2 right-4 -translate-y-1/2 transition-opacity md:right-8", ready ? "opacity-100" : "opacity-0")}>
        <ol className="flex flex-col gap-1">
          {STAGES.map((item) => (
            <li key={item.key}>
              <button
                type="button"
                onClick={() => flyToRef.current?.(item.target)}
                className={cn(
                  "group flex w-full items-center justify-end gap-2.5 rounded-md px-2 py-1 text-right text-xs transition-colors",
                  item.key === stage.key ? "text-white" : "text-white/45 hover:text-white/80"
                )}
              >
                {item.title}
                <span className={cn("h-px transition-all", item.key === stage.key ? "w-6 bg-white" : "w-3 bg-white/30 group-hover:w-4")} />
              </button>
            </li>
          ))}
        </ol>
      </nav>

      {/* 比例尺 */}
      <div className={cn("pointer-events-none absolute bottom-8 left-1/2 flex -translate-x-1/2 flex-col items-center gap-1.5 transition-opacity", ready ? "opacity-100" : "opacity-0")}>
        <span ref={scaleTextRef} className="font-data text-[11px] text-white/70 tabular-nums" />
        <div ref={scaleBarRef} className="h-1.5 border-x border-b border-white/60" />
      </div>

      <p className="pointer-events-none absolute bottom-3 left-6 max-w-[60vw] text-[10px] leading-relaxed text-white/35 md:left-10">
        影像与数据：NASA Blue Marble、Black Marble、NASA SVS 全天星图、NASA/JPL-Caltech 银河系结构图、2MASS 红移巡天（Huchra 等 2012）、WMAP 九年数据
      </p>
      <p className={cn("pointer-events-none absolute right-6 bottom-3 text-[10px] text-white/40 transition-opacity md:right-10", ready ? "opacity-100" : "opacity-0")}>
        滚轮缩放　拖动旋转　Esc 返回
      </p>

      <button
        type="button"
        onClick={close}
        aria-label="返回控制台"
        className="absolute top-5 right-5 flex size-9 items-center justify-center rounded-full border border-white/15 bg-white/5 text-white/80 transition-colors hover:bg-white/15 hover:text-white md:top-7 md:right-8"
      >
        <X className="size-4" />
      </button>

      {/* 载入 */}
      <AnimatePresence>
        {!ready ? (
          <motion.div
            className="absolute inset-0 flex flex-col items-center justify-center gap-4"
            initial={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.6 }}
          >
            {failed ? (
              <p className="text-sm text-white/70">星图载入失败</p>
            ) : (
              <>
                <div className="h-px w-48 overflow-hidden bg-white/15">
                  <motion.div className="h-full bg-white" animate={{ width: `${Math.round(progress * 100)}%` }} transition={{ duration: 0.3 }} />
                </div>
                <p className="font-data text-[11px] text-white/50 tabular-nums">{Math.round(progress * 100)}%</p>
              </>
            )}
          </motion.div>
        ) : null}
      </AnimatePresence>
    </motion.div>,
    document.body
  );
}
