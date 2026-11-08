"use client";

import { useMemo, useState } from "react";
import { Area, AreaChart, Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts";
import { Archive, Boxes, Camera, FileText, HardDrive, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { buildChartConfig, ChartCard } from "@/components/commerce/commerce-charts";
import { StatTile } from "@/components/content/content-shared";
import { Button } from "@/components/ui/button";
import { ChartContainer, ChartTooltip, ChartTooltipContent } from "@/components/ui/chart";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { ApiError } from "@/lib/api/client";
import type { StorageSource } from "@/lib/api/storage-center";
import { formatDate } from "@/lib/datetime";
import { useStorageUsageHistoryQuery, useStorageUsageQuery } from "@/lib/admin-hooks";
import { useCollectStorageUsageMutation } from "@/lib/storage-center-hooks";
import { formatSize } from "./storage-shared";

const TREND_CHART = buildChartConfig([
  { key: "size", label: "大小", color: { light: "#0ea5e9", dark: "#38bdf8" } },
  { key: "files", label: "文件", color: { light: "#10b981", dark: "#34d399" } }
]);
const TYPE_CHART = buildChartConfig([{ key: "size", label: "大小", color: { light: "#6366f1", dark: "#818cf8" } }]);
const RANGES = [7, 30, 90] as const;

export function StorageUsagePanel({ sources }: { sources: StorageSource[] }) {
  const [selected, setSelected] = useState<string>("all");
  const [days, setDays] = useState<number>(30);
  const configId = selected === "all" ? undefined : Number(selected);
  const historyConfig = configId ?? sources[0]?.id ?? null;
  const usage = useStorageUsageQuery(configId);
  const history = useStorageUsageHistoryQuery(historyConfig, days);
  const collect = useCollectStorageUsageMutation();
  const stats = usage.data;

  const trend = useMemo(
    () =>
      [...(history.data ?? [])]
        .sort((a, b) => a.snapshotAt.localeCompare(b.snapshotAt))
        .map((item) => ({ label: formatDate(item.snapshotAt).slice(5), size: item.totalSize, files: item.totalFiles })),
    [history.data]
  );
  const types = useMemo(
    () =>
      [...(stats?.topTypes ?? [])]
        .sort((a, b) => b.size - a.size)
        .slice(0, 8)
        .map((item) => ({ type: item.contentType || "未知", size: item.size, count: item.count })),
    [stats]
  );

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Select value={selected} onValueChange={setSelected}>
          <SelectTrigger size="sm" className="w-48 text-xs">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部存储</SelectItem>
            {sources.map((source) => (
              <SelectItem key={source.id} value={String(source.id)}>
                {source.config_name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button
          size="sm"
          variant="outline"
          className="ml-auto"
          disabled={collect.isPending}
          onClick={async () => {
            try {
              await collect.mutateAsync();
              toast.success("已采集");
            } catch (error) {
              toast.error(error instanceof ApiError ? error.message : "采集失败");
            }
          }}
        >
          {collect.isPending ? <Loader2 className="size-3.5 animate-spin" /> : <Camera className="size-3.5" />}
          立即采集
        </Button>
      </div>

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <StatTile label="总大小" icon={HardDrive} value={stats ? formatSize(stats.totalSize) : "—"} />
        <StatTile label="文件" icon={Boxes} value={stats?.totalFiles.toLocaleString() ?? "—"} />
        <StatTile label="活跃" icon={FileText} value={stats?.activeFiles.toLocaleString() ?? "—"} tone="positive" />
        <StatTile label="回收站" icon={Archive} value={stats?.deletedFiles.toLocaleString() ?? "—"} tone="muted" />
      </div>

      <div className="grid gap-4 xl:grid-cols-5">
        <div className="xl:col-span-3">
          <ChartCard
            title={`用量趋势 · ${sources.find((item) => item.id === historyConfig)?.config_name ?? ""}`}
            loading={history.isLoading}
            empty={trend.length === 0}
            height={260}
            action={
              <ToggleGroup type="single" size="sm" value={String(days)} onValueChange={(value) => value && setDays(Number(value))}>
                {RANGES.map((range) => (
                  <ToggleGroupItem key={range} value={String(range)} className="h-7 px-2.5 text-xs">
                    {range} 天
                  </ToggleGroupItem>
                ))}
              </ToggleGroup>
            }
          >
            <ChartContainer config={TREND_CHART} className="h-[260px] w-full">
              <AreaChart data={trend} margin={{ left: 0, right: 8, top: 8 }}>
                <CartesianGrid vertical={false} strokeDasharray="3 3" />
                <XAxis dataKey="label" tickLine={false} axisLine={false} fontSize={11} />
                <YAxis
                  yAxisId="size"
                  tickLine={false}
                  axisLine={false}
                  fontSize={11}
                  width={56}
                  tickFormatter={(value) => formatSize(Number(value))}
                />
                <YAxis yAxisId="files" orientation="right" tickLine={false} axisLine={false} fontSize={11} width={40} />
                <ChartTooltip
                  content={
                    <ChartTooltipContent
                      formatter={(value, name) => (name === "size" ? formatSize(Number(value)) : Number(value).toLocaleString())}
                    />
                  }
                />
                <Area yAxisId="size" dataKey="size" type="monotone" stroke="var(--color-size)" fill="var(--color-size)" fillOpacity={0.15} />
                <Area yAxisId="files" dataKey="files" type="monotone" stroke="var(--color-files)" fill="var(--color-files)" fillOpacity={0.08} />
              </AreaChart>
            </ChartContainer>
          </ChartCard>
        </div>
        <div className="xl:col-span-2">
          <ChartCard title="文件类型" loading={usage.isLoading} empty={types.length === 0} height={260}>
            <ChartContainer config={TYPE_CHART} className="h-[260px] w-full">
              <BarChart data={types} layout="vertical" margin={{ left: 8, right: 16, top: 4 }}>
                <CartesianGrid horizontal={false} strokeDasharray="3 3" />
                <XAxis type="number" tickLine={false} axisLine={false} fontSize={11} tickFormatter={(value) => formatSize(Number(value))} />
                <YAxis type="category" dataKey="type" tickLine={false} axisLine={false} fontSize={10} width={110} />
                <ChartTooltip content={<ChartTooltipContent formatter={(value) => formatSize(Number(value))} />} />
                <Bar dataKey="size" fill="var(--color-size)" radius={[0, 4, 4, 0]} maxBarSize={18} />
              </BarChart>
            </ChartContainer>
          </ChartCard>
        </div>
      </div>
    </div>
  );
}
