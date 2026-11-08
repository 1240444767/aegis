"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useTheme } from "next-themes";
import * as echarts from "echarts/core";
import { MapChart } from "echarts/charts";
import { TooltipComponent, VisualMapComponent } from "echarts/components";
import { CanvasRenderer } from "echarts/renderers";
import { Skeleton } from "@/components/ui/skeleton";
import type { ReportSlice } from "@/lib/api/reports";
import { formatValue } from "./report-labels";

echarts.use([MapChart, TooltipComponent, VisualMapComponent, CanvasRenderer]);

/**
 * 省级分布图。GeoJSON 是控制台自带的 /geo/china/100000.json（约 650KB），只在打开地区页时加载一次。
 *
 * 归属地数据源不统一：纯真给「广东省」，GeoLite2 可能给「Guangdong」，旧数据还有「广东」。
 * 统一折算成简称再和地图上的行政区名对齐，对不上的不画（右侧排行里仍能看到）。
 */

const PINYIN: Record<string, string> = {
  beijing: "北京", tianjin: "天津", hebei: "河北", shanxi: "山西", "inner mongolia": "内蒙古", neimenggu: "内蒙古",
  liaoning: "辽宁", jilin: "吉林", heilongjiang: "黑龙江", shanghai: "上海", jiangsu: "江苏", zhejiang: "浙江",
  anhui: "安徽", fujian: "福建", jiangxi: "江西", shandong: "山东", henan: "河南", hubei: "湖北", hunan: "湖南",
  guangdong: "广东", guangxi: "广西", hainan: "海南", chongqing: "重庆", sichuan: "四川", guizhou: "贵州",
  yunnan: "云南", tibet: "西藏", xizang: "西藏", shaanxi: "陕西", gansu: "甘肃", qinghai: "青海", ningxia: "宁夏",
  xinjiang: "新疆", taiwan: "台湾", "hong kong": "香港", hongkong: "香港", macau: "澳门", macao: "澳门"
};

export function provinceShortName(name: string) {
  const trimmed = name.trim();
  const pinyin = PINYIN[trimmed.toLowerCase()];
  if (pinyin) return pinyin;
  return trimmed.replace(/(特别行政区|维吾尔自治区|壮族自治区|回族自治区|自治区|省|市)$/u, "");
}

type GeoJSON = { type: string; features: Array<{ properties: { name: string } }> };
let geoPromise: Promise<GeoJSON> | null = null;

function loadChina() {
  if (!geoPromise) {
    geoPromise = fetch("/geo/china/100000.json").then((r) => {
      if (!r.ok) throw new Error(`地图数据加载失败（HTTP ${r.status}）`);
      return r.json() as Promise<GeoJSON>;
    });
    geoPromise.catch(() => {
      geoPromise = null;
    });
  }
  return geoPromise;
}

function cssVar(el: Element, name: string, fallback: string) {
  const v = getComputedStyle(el).getPropertyValue(name).trim();
  return v || fallback;
}

export function ChinaMap({ items, metric = "users", height = 380 }: { items: ReportSlice[]; metric?: "users" | "count"; height?: number }) {
  const ref = useRef<HTMLDivElement>(null);
  const chartRef = useRef<echarts.ECharts | null>(null);
  const [geo, setGeo] = useState<GeoJSON | null>(null);
  const [error, setError] = useState<string | null>(null);
  const { resolvedTheme } = useTheme();

  useEffect(() => {
    let alive = true;
    loadChina()
      .then((json) => {
        if (!alive) return;
        if (!echarts.getMap("aegis-china")) {
          echarts.registerMap("aegis-china", json as never);
        }
        setGeo(json);
      })
      .catch((err: Error) => alive && setError(err.message));
    return () => {
      alive = false;
    };
  }, []);

  const data = useMemo(() => {
    if (!geo) return [];
    const byShort = new Map<string, number>();
    for (const it of items) {
      const short = provinceShortName(it.key);
      byShort.set(short, (byShort.get(short) ?? 0) + Number(it[metric] ?? 0));
    }
    return geo.features.map((f) => ({ name: f.properties.name, value: byShort.get(provinceShortName(f.properties.name)) ?? 0 }));
  }, [geo, items, metric]);

  useEffect(() => {
    const el = ref.current;
    if (!el || !geo) return;
    const chart = chartRef.current ?? echarts.init(el, undefined, { renderer: "canvas" });
    chartRef.current = chart;
    const accent = cssVar(el, "--rp-0", "#6366f1");
    const muted = cssVar(document.documentElement, "--muted", "#f4f4f5");
    const border = cssVar(document.documentElement, "--border", "#e4e4e7");
    const fg = cssVar(document.documentElement, "--muted-foreground", "#71717a");
    const max = Math.max(...data.map((d) => d.value), 1);
    chart.setOption(
      {
        tooltip: {
          trigger: "item",
          formatter: (p: { name: string; value?: number }) => `${p.name}<br/>${formatValue(Number(p.value) || 0)} ${metric === "users" ? "人" : "次"}`
        },
        visualMap: {
          min: 0,
          max,
          left: 8,
          bottom: 8,
          itemWidth: 10,
          itemHeight: 90,
          calculable: false,
          text: ["多", "少"],
          textStyle: { color: fg, fontSize: 11 },
          inRange: { color: [muted, accent] }
        },
        series: [
          {
            type: "map",
            map: "aegis-china",
            roam: false,
            layoutCenter: ["50%", "56%"],
            layoutSize: "118%",
            label: { show: false },
            itemStyle: { borderColor: border, borderWidth: 0.6, areaColor: muted },
            emphasis: { label: { show: true, color: fg, fontSize: 11 }, itemStyle: { areaColor: accent, opacity: 0.85 } },
            select: { disabled: true },
            data
          }
        ]
      },
      true
    );
    const resize = new ResizeObserver(() => chart.resize());
    resize.observe(el);
    return () => resize.disconnect();
  }, [geo, data, metric, resolvedTheme]);

  useEffect(
    () => () => {
      chartRef.current?.dispose();
      chartRef.current = null;
    },
    []
  );

  if (error) {
    return (
      <div className="flex items-center justify-center rounded-xl border border-dashed text-xs text-muted-foreground" style={{ height }}>
        {error}
      </div>
    );
  }
  return (
    <div className="relative w-full" style={{ height }}>
      {!geo ? <Skeleton className="absolute inset-0 rounded-xl" /> : null}
      <div ref={ref} className="size-full" />
    </div>
  );
}
