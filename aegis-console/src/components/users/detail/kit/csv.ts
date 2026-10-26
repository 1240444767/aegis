/**
 * 导出 CSV。带 BOM，Excel 直接打开不乱码；单元格里的引号、逗号、换行按 RFC 4180 转义，
 * 以 = + - @ 开头的文本前面加单引号，防止被表格软件当成公式执行。
 */
export type CsvColumn<T> = { header: string; value: (row: T) => string | number | boolean | null | undefined };

function escapeCell(value: string | number | boolean | null | undefined) {
  if (value === null || value === undefined) return "";
  let text = String(value);
  if (typeof value === "string" && /^[=+\-@\t\r]/.test(text)) text = `'${text}`;
  return /[",\r\n]/.test(text) ? `"${text.replace(/"/g, '""')}"` : text;
}

export function toCsv<T>(columns: Array<CsvColumn<T>>, rows: T[]) {
  const lines = [columns.map((column) => escapeCell(column.header)).join(",")];
  for (const row of rows) lines.push(columns.map((column) => escapeCell(column.value(row))).join(","));
  return lines.join("\r\n");
}

export function downloadCsv<T>(filename: string, columns: Array<CsvColumn<T>>, rows: T[]) {
  const blob = new Blob(["﻿" + toCsv(columns, rows)], { type: "text/csv;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename.endsWith(".csv") ? filename : `${filename}.csv`;
  document.body.appendChild(link);
  link.click();
  link.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 1000);
}

/**
 * 逐页拉取后导出。`fetchPage` 返回该页条目与总数；最多拉 `maxRows` 条，避免一次导出拖垮接口。
 */
export async function collectPages<T>(
  fetchPage: (page: number, limit: number) => Promise<{ items: T[] | null; total: number }>,
  { limit = 100, maxRows = 5000 }: { limit?: number; maxRows?: number } = {}
) {
  const rows: T[] = [];
  let page = 1;
  let total = 0;
  do {
    const result = await fetchPage(page, limit);
    total = result.total;
    rows.push(...(result.items ?? []));
    if (!result.items?.length) break;
    page += 1;
  } while (rows.length < Math.min(total, maxRows));
  return { rows: rows.slice(0, maxRows), total, truncated: total > maxRows };
}
