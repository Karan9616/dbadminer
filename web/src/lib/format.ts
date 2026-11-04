export type CellKind = "null" | "number" | "boolean" | "json" | "array" | "blob" | "text";

const BINARY_TYPES = new Set([
  "BYTEA",
  "BLOB",
  "TINYBLOB",
  "MEDIUMBLOB",
  "LONGBLOB",
  "BINARY",
  "VARBINARY",
  "BIT",
  "RAW",
]);

/** True when the SQL column type is binary/blob-like. */
export function isBinaryColumnType(typeName: string | undefined | null): boolean {
  if (!typeName) return false;
  const u = typeName.trim().toUpperCase();
  if (BINARY_TYPES.has(u)) return true;
  // MySQL/MariaDB often report "blob" / "longblob"; Postgres "bytea".
  return BINARY_TYPES.has(u.split("(")[0]?.trim() ?? "");
}

export function cellKind(value: unknown, columnType?: string | null): CellKind {
  if (value === null || value === undefined) return "null";
  if (typeof value === "number" || typeof value === "bigint") return "number";
  if (typeof value === "boolean") return "boolean";
  if (Array.isArray(value)) return "array";
  if (typeof value === "object") return "json";
  if (typeof value === "string" && isBinaryColumnType(columnType)) return "blob";
  return "text";
}

export function cellText(value: unknown): string {
  if (value === null || value === undefined) return "NULL";
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}

/** Compact chip label for structured cells: `[n]` / `{n}`. */
export function cellSummary(value: unknown): string {
  if (Array.isArray(value)) {
    const n = value.length;
    return n === 1 ? "[1]" : `[${n} items]`;
  }
  if (value !== null && typeof value === "object") {
    return `{${Object.keys(value as object).length}}`;
  }
  return cellText(value);
}

/** Decoded byte length of a base64 blob string (API form for binary columns). */
export function blobByteLength(base64: string): number {
  const padded = base64.replace(/\s+/g, "");
  if (padded.length === 0) return 0;
  const pad = padded.endsWith("==") ? 2 : padded.endsWith("=") ? 1 : 0;
  return Math.max(0, Math.floor((padded.length * 3) / 4) - pad);
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(n < 10 * 1024 ? 1 : 0)} KB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

export function blobSummary(base64: string): string {
  return `BLOB · ${formatBytes(blobByteLength(base64))}`;
}

export function cellPretty(value: unknown): string {
  if (typeof value === "object" && value !== null) return JSON.stringify(value, null, 2);
  if (typeof value === "string") {
    const t = value.trim();
    if ((t.startsWith("{") && t.endsWith("}")) || (t.startsWith("[") && t.endsWith("]"))) {
      try {
        return JSON.stringify(JSON.parse(t), null, 2);
      } catch {
        return value;
      }
    }
  }
  return cellText(value);
}

export function formatDuration(ms: number): string {
  if (ms < 1) return "<1 ms";
  if (ms < 1000) return `${Math.round(ms)} ms`;
  return `${(ms / 1000).toFixed(2)} s`;
}

export function formatCount(n: number): string {
  return n.toLocaleString();
}

export function dialectLabel(d: string): string {
  switch (d) {
    case "postgres":
      return "PostgreSQL";
    case "mysql":
      return "MySQL";
    case "mariadb":
      return "MariaDB";
    case "sqlite":
      return "SQLite";
    default:
      return d;
  }
}
