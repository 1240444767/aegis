// 从控制台的中国地图资源生成后端内置的行政区表（省、市、区县的全称与中心点）。
// 用法：node scripts/gen-china-regions.mjs，输出到 ../internal/service/chinageo/regions.tsv
import fs from "node:fs";
import path from "node:path";

const rootDir = path.resolve(import.meta.dirname, "..");
const sourceDir = path.join(rootDir, "public", "geo", "china");
const outputPath = path.resolve(rootDir, "..", "internal", "service", "chinageo", "regions.tsv");

const rows = new Map();
for (const file of fs.readdirSync(sourceDir).sort()) {
  if (!/^\d{6}\.json$/.test(file)) continue;
  const geo = JSON.parse(fs.readFileSync(path.join(sourceDir, file), "utf8"));
  for (const feature of geo.features ?? []) {
    const p = feature.properties ?? {};
    const center = p.center ?? p.centroid;
    if (!p.adcode || !p.name || !Array.isArray(center) || !["province", "city", "district"].includes(p.level)) continue;
    // parent 在部分文件里是字符串化的 JSON，上级以 acroutes 末项为准
    const parent = Array.isArray(p.acroutes) && p.acroutes.length ? p.acroutes[p.acroutes.length - 1] : p.parent?.adcode ?? 0;
    rows.set(p.adcode, [p.adcode, parent, p.level, p.name, center[0].toFixed(5), center[1].toFixed(5)]);
  }
}

const lines = [...rows.values()].sort((a, b) => a[0] - b[0]).map((row) => row.join("\t"));
fs.writeFileSync(outputPath, `# adcode\tparent\tlevel\tname\tlng\tlat\n${lines.join("\n")}\n`);
console.log(`china regions: ${lines.length} -> ${path.relative(rootDir, outputPath)}`);
