/*
 * 宇宙彩蛋的天文换算。
 *
 * 全场景统一使用银道直角坐标，单位光年，原点为太阳：
 *   x 指向银心（l=0, b=0），y 指向 l=90°（银河自转方向），z 指向北银极。
 * 尺度跨越二十个数量级，坐标一律用 JS 的双精度计算，进入着色器前再按当前视距缩放。
 */

export type Vec3 = [number, number, number];

export const DEG = Math.PI / 180;

export const LY_PER_KM = 1 / 9.4607e12;
export const EARTH_RADIUS_LY = 6371 * LY_PER_KM;
export const AU_LY = 1.495978707e8 * LY_PER_KM;
export const KLY = 1_000;
export const MPC_LY = 3.2616e6;
export const GLY = 1e9;

/** 太阳到银心的距离（GRAVITY 2019：8.178 kpc）。 */
export const GALACTIC_CENTER_LY = 26_670;
/** NASA/JPL-Caltech（R. Hurt）银河系俯视图的实际跨度，按图中太阳到银心的距离标定。 */
export const MILKY_WAY_IMAGE_LY = 130_000;
/** 可观测宇宙共动半径。 */
export const OBSERVABLE_RADIUS_LY = 46.5 * GLY;

/** J2000 赤道 → 银道的旋转矩阵（行为银道 x/y/z）。 */
const EQ_TO_GAL = [
  [-0.0548755604, -0.8734370902, -0.4838350155],
  [0.4941094279, -0.44482963, 0.7469822445],
  [-0.867666149, -0.1980763734, 0.4559837762]
];

export function equatorialToGalactic([x, y, z]: Vec3): Vec3 {
  return [
    EQ_TO_GAL[0]![0]! * x + EQ_TO_GAL[0]![1]! * y + EQ_TO_GAL[0]![2]! * z,
    EQ_TO_GAL[1]![0]! * x + EQ_TO_GAL[1]![1]! * y + EQ_TO_GAL[1]![2]! * z,
    EQ_TO_GAL[2]![0]! * x + EQ_TO_GAL[2]![1]! * y + EQ_TO_GAL[2]![2]! * z
  ];
}

export function radec(raDeg: number, decDeg: number, distance = 1): Vec3 {
  const ra = raDeg * DEG;
  const dec = decDeg * DEG;
  return [Math.cos(dec) * Math.cos(ra) * distance, Math.cos(dec) * Math.sin(ra) * distance, Math.sin(dec) * distance];
}

/** 赤经赤纬（度）与距离 → 银道直角坐标。 */
export function galacticFromRadec(raDeg: number, decDeg: number, distance: number): Vec3 {
  return equatorialToGalactic(radec(raDeg, decDeg, distance));
}

export function galacticFromLb(lDeg: number, bDeg: number, distance: number): Vec3 {
  const l = lDeg * DEG;
  const b = bDeg * DEG;
  return [Math.cos(b) * Math.cos(l) * distance, Math.cos(b) * Math.sin(l) * distance, Math.sin(b) * distance];
}

/** 北天极方向（地球自转轴）。 */
export const NORTH_CELESTIAL_POLE = galacticFromLb(122.932, 27.128, 1);

const OBLIQUITY = 23.4393 * DEG;

/** 黄道坐标（经度、距离）→ 银道直角坐标，行星都按黄道面上的圆轨道近似。 */
export function galacticFromEcliptic(lonRad: number, distance: number): Vec3 {
  const x = Math.cos(lonRad) * distance;
  const y = Math.sin(lonRad) * distance;
  // 黄道 → 赤道：绕 x 轴转黄赤交角
  return equatorialToGalactic([x, y * Math.cos(OBLIQUITY), y * Math.sin(OBLIQUITY)]);
}

/** 黄道面在银道坐标中的两个基向量，画行星轨道用。 */
export const ECLIPTIC_BASIS = {
  u: galacticFromEcliptic(0, 1),
  v: galacticFromEcliptic(Math.PI / 2, 1)
};

function daysSinceJ2000(date: Date) {
  return (date.getTime() - Date.UTC(2000, 0, 1, 12)) / 86_400_000;
}

export type Planet = { key: string; name: string; au: number; color: string; lonJ2000: number; rate: number };

/** 八大行星：半长轴与 J2000 平黄经、日运动（度），圆轨道近似足以画出正确的相对位置。 */
export const PLANETS: Planet[] = [
  { key: "mercury", name: "水星", au: 0.387, color: "#b9b2a8", lonJ2000: 252.25, rate: 4.09233 },
  { key: "venus", name: "金星", au: 0.723, color: "#e8d3a2", lonJ2000: 181.98, rate: 1.60213 },
  { key: "earth", name: "地球", au: 1.0, color: "#7cb7ff", lonJ2000: 100.46, rate: 0.98561 },
  { key: "mars", name: "火星", au: 1.524, color: "#e07a4f", lonJ2000: 355.43, rate: 0.52403 },
  { key: "jupiter", name: "木星", au: 5.203, color: "#d8b991", lonJ2000: 34.4, rate: 0.08309 },
  { key: "saturn", name: "土星", au: 9.537, color: "#e3cf9b", lonJ2000: 49.94, rate: 0.03346 },
  { key: "uranus", name: "天王星", au: 19.19, color: "#9fe0e8", lonJ2000: 313.23, rate: 0.01173 },
  { key: "neptune", name: "海王星", au: 30.07, color: "#6f8cff", lonJ2000: 304.88, rate: 0.00598 }
];

export function planetPosition(planet: Planet, date: Date): Vec3 {
  const lon = (planet.lonJ2000 + planet.rate * daysSinceJ2000(date)) * DEG;
  return galacticFromEcliptic(lon, planet.au * AU_LY);
}

/** 太阳直射点（弧度），与概览首屏的地球同一套近似。 */
export function subsolarPoint(date: Date) {
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

export type Galaxy = {
  key: string;
  name: string;
  position: Vec3;
  /** 盘面直径（光年）；不规则星系用点云表示 */
  diameter: number;
  /** 盘面法向（银道坐标），旋涡星系用 */
  normal?: Vec3;
  kind: "spiral" | "irregular" | "dwarf";
  tint: string;
};

/** 由位置角与倾角求盘面法向：主轴从北向东量 PA，再绕主轴倾斜 i。 */
function diskNormal(raDeg: number, decDeg: number, paDeg: number, inclinationDeg: number): Vec3 {
  const ra = raDeg * DEG;
  const dec = decDeg * DEG;
  const los: Vec3 = [Math.cos(dec) * Math.cos(ra), Math.cos(dec) * Math.sin(ra), Math.sin(dec)];
  const east: Vec3 = [-Math.sin(ra), Math.cos(ra), 0];
  const north: Vec3 = [-Math.sin(dec) * Math.cos(ra), -Math.sin(dec) * Math.sin(ra), Math.cos(dec)];
  const pa = paDeg * DEG;
  const inc = inclinationDeg * DEG;
  const minor: Vec3 = [
    -Math.sin(pa) * north[0] + Math.cos(pa) * east[0],
    -Math.sin(pa) * north[1] + Math.cos(pa) * east[1],
    -Math.sin(pa) * north[2] + Math.cos(pa) * east[2]
  ];
  const normal: Vec3 = [
    -Math.cos(inc) * los[0] + Math.sin(inc) * minor[0],
    -Math.cos(inc) * los[1] + Math.sin(inc) * minor[1],
    -Math.cos(inc) * los[2] + Math.sin(inc) * minor[2]
  ];
  return equatorialToGalactic(normal);
}

/** 本星系群的主要成员：坐标、距离来自 NED/McConnachie 2012。 */
export const LOCAL_GROUP: Galaxy[] = [
  {
    key: "m31",
    name: "仙女座星系",
    position: galacticFromRadec(10.6847, 41.269, 2_537_000),
    diameter: 152_000,
    normal: diskNormal(10.6847, 41.269, 38, 77),
    kind: "spiral",
    tint: "#ffe9d0"
  },
  {
    key: "m33",
    name: "三角座星系",
    position: galacticFromRadec(23.4621, 30.6599, 2_730_000),
    diameter: 60_000,
    normal: diskNormal(23.4621, 30.6599, 23, 54),
    kind: "spiral",
    tint: "#dfe8ff"
  },
  { key: "lmc", name: "大麦哲伦云", position: galacticFromRadec(80.894, -69.756, 163_000), diameter: 32_000, kind: "irregular", tint: "#dfe6ff" },
  { key: "smc", name: "小麦哲伦云", position: galacticFromRadec(13.187, -72.829, 200_000), diameter: 18_000, kind: "irregular", tint: "#e6ebff" },
  ...(
    [
      ["m32", 10.674, 40.865, 2_490_000],
      ["m110", 10.092, 41.685, 2_690_000],
      ["ngc185", 9.741, 48.337, 2_030_000],
      ["ngc147", 8.3, 48.509, 2_530_000],
      ["ic10", 5.072, 59.304, 2_200_000],
      ["ic1613", 16.199, 2.118, 2_380_000],
      ["wlm", 0.492, -15.461, 3_040_000],
      ["ngc6822", 296.24, -14.8, 1_630_000],
      ["leo1", 152.117, 12.307, 820_000],
      ["fornax", 39.997, -34.449, 460_000],
      ["sculptor", 15.039, -33.709, 290_000],
      ["draco", 260.06, 57.92, 260_000],
      ["sagittarius", 283.831, -30.545, 65_000]
    ] as const
  ).map(([key, ra, dec, distance]) => ({
    key,
    name: "",
    position: galacticFromRadec(ra, dec, distance),
    diameter: 4_000,
    kind: "dwarf" as const,
    tint: "#cfd8ff"
  }))
];

export const M31_POSITION = LOCAL_GROUP[0]!.position;

/** 拉尼亚凯亚尺度上的著名结构，位置由星系团中心的赤经赤纬与红移距离算出。 */
export const STRUCTURES: Array<{ key: string; name: string; position: Vec3 }> = [
  { key: "virgo", name: "室女座星系团", position: galacticFromRadec(187.7, 12.39, 16.5 * MPC_LY) },
  { key: "ga", name: "巨引源", position: galacticFromRadec(243.6, -60.9, 69.6 * MPC_LY) },
  { key: "coma", name: "后发座星系团", position: galacticFromRadec(194.95, 27.98, 99 * MPC_LY) },
  { key: "perseus", name: "英仙双鱼超星系团", position: galacticFromRadec(49.95, 41.51, 76.7 * MPC_LY) },
  { key: "shapley", name: "沙普利超星系团", position: galacticFromRadec(201.98, -31.5, 207 * MPC_LY) }
];

export type Stage = {
  key: string;
  title: string;
  subtitle: string;
  /** 进入该阶段的视距下限（log10 光年） */
  from: number;
  /** 点击阶段跳转时飞到的视距 */
  target: number;
};

export const STAGES: Stage[] = [
  { key: "earth", title: "地球", subtitle: "直径 12,742 公里，按当前时刻的日照", from: -Infinity, target: -8.62 },
  { key: "solar", title: "太阳系", subtitle: "八大行星按今天的位置排列，海王星轨道半径约 30 天文单位", from: -5.6, target: -3.55 },
  { key: "neighborhood", title: "太阳附近", subtitle: "NASA 全天星图，太阳系所见的银河", from: -0.6, target: 1.2 },
  { key: "milkyway", title: "银河系", subtitle: "直径约 10 万光年，太阳距银心约 2.6 万光年", from: 3.2, target: 5.05 },
  { key: "localgroup", title: "本星系群", subtitle: "五十多个星系，跨度约 1000 万光年", from: 5.65, target: 6.85 },
  { key: "laniakea", title: "拉尼亚凯亚超星系团", subtitle: "2MASS 红移巡天的 43,507 个真实星系，直径约 5.2 亿光年", from: 7.45, target: 8.85 },
  { key: "web", title: "宇宙网", subtitle: "泽尔多维奇近似模拟的纤维、墙与空洞", from: 9.35, target: 9.95 },
  { key: "universe", title: "可观测宇宙", subtitle: "直径约 930 亿光年，外壳为 WMAP 宇宙微波背景", from: 10.45, target: 11.1 }
];

export const MIN_LOG = -8.7;
export const MAX_LOG = 11.8;

export function stageAt(logDistance: number) {
  let current = STAGES[0]!;
  for (const stage of STAGES) if (logDistance >= stage.from) current = stage;
  return current;
}

export const smoothstep = (edge0: number, edge1: number, x: number) => {
  const t = Math.min(1, Math.max(0, (x - edge0) / (edge1 - edge0)));
  return t * t * (3 - 2 * t);
};

/** 淡入 a→b、淡出 c→d 的窗口。 */
export function windowed(x: number, a: number, b: number, c: number, d: number) {
  return smoothstep(a, b, x) * (1 - smoothstep(c, d, x));
}

const lerp3 = (a: Vec3, b: Vec3, t: number): Vec3 => [a[0] + (b[0] - a[0]) * t, a[1] + (b[1] - a[1]) * t, a[2] + (b[2] - a[2]) * t];

/** 视点沿尺度移动：地球 → 太阳 → 银心 → 本星系群质心 → 回到我们自己（巡天以我们为中心）。 */
export function focusAt(logDistance: number, earth: Vec3): Vec3 {
  const sun: Vec3 = [0, 0, 0];
  const galacticCenter: Vec3 = [GALACTIC_CENTER_LY, 0, 0];
  const localGroup = lerp3(galacticCenter, M31_POSITION, 0.45);
  if (logDistance < -4.8) return lerp3(earth, sun, smoothstep(-6.6, -4.8, logDistance));
  if (logDistance < 4.5) return lerp3(sun, galacticCenter, smoothstep(3.3, 4.5, logDistance));
  if (logDistance < 6.6) return lerp3(galacticCenter, localGroup, smoothstep(5.7, 6.6, logDistance));
  return lerp3(localGroup, sun, smoothstep(7.6, 8.4, logDistance));
}

/** 比例尺长度的中文写法。 */
export function formatDistance(ly: number) {
  if (ly < 0.0005 * AU_LY * 1000) {
    const km = ly / LY_PER_KM;
    return km >= 10_000 ? `${trim(km / 10_000)} 万公里` : `${trim(km)} 公里`;
  }
  if (ly < 0.05) return `${trim(ly / AU_LY)} 天文单位`;
  if (ly < 10_000) return `${trim(ly)} 光年`;
  if (ly < 1e8) return `${trim(ly / 10_000)} 万光年`;
  return `${trim(ly / 1e8)} 亿光年`;
}

function trim(value: number) {
  return Number(value.toPrecision(2)).toLocaleString("zh-CN");
}

/** 1、2、5 序列里不超过上限的最大值。 */
export function niceLength(max: number) {
  const exponent = Math.floor(Math.log10(max));
  const base = 10 ** exponent;
  for (const step of [5, 2, 1]) if (step * base <= max) return step * base;
  return base;
}
