import type { ResultSet } from "./api";

function csvField(value: unknown): string {
  if (value === null || value === undefined) return "";
  const s = typeof value === "object" ? JSON.stringify(value) : String(value);
  return /[",\r\n]/.test(s) ? `"${s.replaceAll('"', '""')}"` : s;
}

export function toCSV(data: ResultSet): string {
  const lines = [data.columns.map(csvField).join(",")];
  for (const row of data.rows) lines.push(row.map(csvField).join(","));
  return lines.join("\r\n") + "\r\n";
}

export function toJSON(data: ResultSet): string {
  const objects = data.rows.map((row) => Object.fromEntries(data.columns.map((c, i) => [c, row[i]])));
  return JSON.stringify(objects, null, 2) + "\n";
}

export function download(filename: string, content: string | Blob | ArrayBuffer | Uint8Array, type: string): void {
  const blob =
    content instanceof Blob
      ? content
      : new Blob([content as BlobPart], { type });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

/** Download a binary cell stored as base64 (API form for BYTEA/BLOB). */
export function downloadBase64Blob(base64: string, filename: string): void {
  const clean = base64.replace(/\s+/g, "");
  const bin = atob(clean);
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
  const safe = filename.replace(/[^\w.-]+/g, "_") || "blob";
  download(safe, bytes, "application/octet-stream");
}

export function exportResult(data: ResultSet, basename: string, format: "csv" | "json"): void {
  const safe = basename.replace(/[^\w.-]+/g, "_") || "result";
  if (format === "csv") download(`${safe}.csv`, toCSV(data), "text/csv;charset=utf-8");
  else download(`${safe}.json`, toJSON(data), "application/json");
}
