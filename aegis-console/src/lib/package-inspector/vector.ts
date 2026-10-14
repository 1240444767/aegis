/**
 * Android VectorDrawable（编译后的 XML）→ SVG。
 *
 * 现在的应用图标大多是自适应图标，前景、背景都是矢量图；只认位图的话，
 * 拿到的往往是工程模板里没人改过的旧版 PNG。这里把矢量图翻成 SVG 交给浏览器画。
 *
 * 支持 group（平移、缩放、旋转、轴心）、path（填充、描边、透明度、填充规则）、
 * clip-path，以及填充色引用的渐变资源（aapt:attr 内联渐变编译后是单独的 <gradient> 文件）。
 */

import type { ResourceTable, XmlElement, XmlValue } from "./android";
import { parseBinaryXml } from "./android";
import type { ZipReader } from "./zip";

type Ctx = {
  zip: ZipReader;
  table: ResourceTable;
  defs: string[];
  seq: number;
};

function num(v: XmlValue | undefined, fallback = 0) {
  if (!v) return fallback;
  if (v.kind === "int" || v.kind === "other") return Number(v.value);
  if (v.kind === "string") {
    const n = parseFloat(v.value);
    return Number.isFinite(n) ? n : fallback;
  }
  return fallback;
}

function argb(value: number) {
  const a = ((value >>> 24) & 0xff) / 255;
  return { css: `rgb(${(value >>> 16) & 0xff}, ${(value >>> 8) & 0xff}, ${value & 0xff})`, alpha: a };
}

function escapeAttr(s: string) {
  return s.replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;");
}

/** 颜色属性：直接色值、颜色资源，或渐变 drawable（返回 url(#id)） */
async function paint(ctx: Ctx, v: XmlValue | undefined): Promise<{ css: string; alpha: number } | null> {
  if (!v) return null;
  if (v.kind === "color") return argb(v.value);
  if (v.kind === "string" && /^#[0-9a-f]{3,8}$/i.test(v.value)) return { css: v.value, alpha: 1 };
  if (v.kind !== "ref") return null;
  const color = ctx.table.resolveColor(v.value);
  if (color !== undefined) return argb(color);
  const file = ctx.table.resolveFiles(v.value).find((f) => f.path.endsWith(".xml") && ctx.zip.has(f.path));
  if (!file) return null;
  const bytes = await ctx.zip.read(file.path, 1024 * 1024);
  const gradient = bytes ? parseBinaryXml(bytes) : null;
  if (gradient?.name !== "gradient") return null;
  return { css: `url(#${gradientDef(ctx, gradient)})`, alpha: 1 };
}

function colorAttr(v: XmlValue | undefined) {
  return v?.kind === "color" ? argb(v.value) : null;
}

function gradientDef(ctx: Ctx, el: XmlElement): string {
  const id = `g${++ctx.seq}`;
  const a = el.attrs;
  const type = num(a.type, 0); // 0 linear, 1 radial, 2 sweep
  const stops: string[] = [];
  const items = el.children.filter((c) => c.name === "item");
  if (items.length) {
    for (const item of items) {
      const c = colorAttr(item.attrs.color);
      if (c) stops.push(`<stop offset="${num(item.attrs.offset)}" stop-color="${c.css}" stop-opacity="${c.alpha}"/>`);
    }
  } else {
    const start = colorAttr(a.startColor);
    const center = colorAttr(a.centerColor);
    const end = colorAttr(a.endColor);
    if (start) stops.push(`<stop offset="0" stop-color="${start.css}" stop-opacity="${start.alpha}"/>`);
    if (center) stops.push(`<stop offset="0.5" stop-color="${center.css}" stop-opacity="${center.alpha}"/>`);
    if (end) stops.push(`<stop offset="1" stop-color="${end.css}" stop-opacity="${end.alpha}"/>`);
  }
  if (type === 1) {
    ctx.defs.push(
      `<radialGradient id="${id}" gradientUnits="userSpaceOnUse" cx="${num(a.centerX)}" cy="${num(a.centerY)}" r="${num(a.gradientRadius, 1)}">${stops.join("")}</radialGradient>`
    );
  } else {
    // 扫描渐变 SVG 没有对应物，退化为线性渐变
    ctx.defs.push(
      `<linearGradient id="${id}" gradientUnits="userSpaceOnUse" x1="${num(a.startX)}" y1="${num(a.startY)}" x2="${num(a.endX)}" y2="${num(a.endY)}">${stops.join("")}</linearGradient>`
    );
  }
  return id;
}

async function renderNode(ctx: Ctx, el: XmlElement): Promise<string> {
  if (el.name === "path") {
    const d = el.attrs.pathData?.kind === "string" ? el.attrs.pathData.value : "";
    if (!d) return "";
    const fill = await paint(ctx, el.attrs.fillColor);
    const stroke = await paint(ctx, el.attrs.strokeColor);
    const fillAlpha = num(el.attrs.fillAlpha, 1) * (fill?.alpha ?? 1);
    const strokeAlpha = num(el.attrs.strokeAlpha, 1) * (stroke?.alpha ?? 1);
    const parts = [
      `d="${escapeAttr(d)}"`,
      `fill="${fill ? fill.css : "none"}"`,
      fill && fillAlpha < 1 ? `fill-opacity="${fillAlpha}"` : "",
      num(el.attrs.fillType, 0) === 1 ? `fill-rule="evenodd"` : "",
      stroke ? `stroke="${stroke.css}" stroke-width="${num(el.attrs.strokeWidth, 0)}"` : "",
      stroke && strokeAlpha < 1 ? `stroke-opacity="${strokeAlpha}"` : "",
      stroke ? `stroke-linecap="${["butt", "round", "square"][num(el.attrs.strokeLineCap, 0)] ?? "butt"}" stroke-linejoin="${["miter", "round", "bevel"][num(el.attrs.strokeLineJoin, 0)] ?? "miter"}"` : ""
    ];
    return `<path ${parts.filter(Boolean).join(" ")}/>`;
  }
  if (el.name === "group") {
    const a = el.attrs;
    const px = num(a.pivotX);
    const py = num(a.pivotY);
    const transform = [
      `translate(${num(a.translateX) + px} ${num(a.translateY) + py})`,
      `rotate(${num(a.rotation)})`,
      `scale(${num(a.scaleX, 1)} ${num(a.scaleY, 1)})`,
      `translate(${-px} ${-py})`
    ].join(" ");
    return `<g transform="${transform}">${await renderChildren(ctx, el)}</g>`;
  }
  return "";
}

/** clip-path 作用于其后的兄弟节点 */
async function renderChildren(ctx: Ctx, el: XmlElement): Promise<string> {
  let out = "";
  let clip = "";
  for (const child of el.children) {
    if (child.name === "clip-path") {
      const d = child.attrs.pathData?.kind === "string" ? child.attrs.pathData.value : "";
      if (d) {
        const id = `c${++ctx.seq}`;
        ctx.defs.push(`<clipPath id="${id}"><path d="${escapeAttr(d)}"/></clipPath>`);
        clip = id;
      }
      continue;
    }
    const rendered = await renderNode(ctx, child);
    out += clip ? `<g clip-path="url(#${clip})">${rendered}</g>` : rendered;
  }
  return out;
}

/** 把一个 <vector> 渲染成 SVG 字符串；不是矢量图时返回 null。 */
export async function vectorToSvg(zip: ZipReader, table: ResourceTable, root: XmlElement): Promise<string | null> {
  if (root.name !== "vector") return null;
  const ctx: Ctx = { zip, table, defs: [], seq: 0 };
  const w = num(root.attrs.viewportWidth, 24);
  const h = num(root.attrs.viewportHeight, 24);
  const body = await renderChildren(ctx, root);
  const alpha = num(root.attrs.alpha, 1);
  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${w} ${h}" width="${w * 4}" height="${h * 4}"${alpha < 1 ? ` opacity="${alpha}"` : ""}><defs>${ctx.defs.join("")}</defs>${body}</svg>`;
}
