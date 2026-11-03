import type { ColumnInfo } from "./api";

/** Integer-ish SQL types used for serial / auto-increment PKs (information_schema often returns just "bigint"). */
export function isIntegerLikeType(type: string): boolean {
  const t = type.toLowerCase().trim();
  if (/\b(big|small|medium|tiny)?serial\b/.test(t)) return true;
  if (/^(big|small|medium|tiny)?int\b/.test(t)) return true;
  if (t === "integer" || t === "int8" || t === "int4" || t === "int2") return true;
  return false;
}

/** Columns the DB (or we) can auto-fill: serial / identity / autoincrement / integer PK. */
export function isAutoColumn(col: ColumnInfo): boolean {
  const type = col.type.toLowerCase();
  const def = (col.default ?? "").toLowerCase();
  if (/\bserial\b/.test(type) || type.includes("auto_increment") || type.includes("autoincrement")) {
    return true;
  }
  if (/\bidentity\b/.test(type) || /generated\s+.*\s+as\s+identity/i.test(type)) {
    return true;
  }
  if (def.includes("nextval(")) return true;
  // MySQL AUTO_INCREMENT and Postgres BIGSERIAL often show as bigint/int PK with no useful type flag
  if (col.primaryKey && isIntegerLikeType(col.type)) {
    return true;
  }
  return false;
}

function looksUlid(col: ColumnInfo): boolean {
  const type = col.type.toLowerCase();
  const def = (col.default ?? "").toLowerCase();
  const name = col.name.toLowerCase();
  if (type.includes("ulid") || def.includes("ulid") || /(^|_)ulid$/.test(name) || name === "ulid") {
    return true;
  }
  return false;
}

function looksUuid(col: ColumnInfo): boolean {
  if (looksUlid(col)) return false;
  const type = col.type.toLowerCase();
  const def = (col.default ?? "").toLowerCase();
  const name = col.name.toLowerCase();
  if (type.includes("uuid") || type.includes("uniqueidentifier")) return true;
  if (def.includes("gen_random_uuid") || def.includes("uuid_generate") || def.includes("newid(")) {
    return true;
  }
  if (/(^|_)uuid$/.test(name) || name === "uuid") return true;
  return false;
}

export type TemporalKind = "timestamp" | "date" | "time";

export function looksDateTime(col: ColumnInfo): TemporalKind | null {
  const type = col.type.toLowerCase();
  const def = (col.default ?? "").toLowerCase();
  if (
    type.includes("timestamptz") ||
    type.includes("timestamp") ||
    type.includes("datetime") ||
    def.includes("current_timestamp") ||
    def.includes("now()")
  ) {
    return "timestamp";
  }
  if (/\bdate\b/.test(type) || def.includes("current_date")) return "date";
  if ((/\btime\b/.test(type) && !type.includes("timestamp")) || def.includes("current_time")) {
    return "time";
  }
  return null;
}

function pad(n: number): string {
  return String(n).padStart(2, "0");
}

function hasExplicitZone(value: string): boolean {
  return /(?:Z|[+-]\d{2}:?\d{2})$/i.test(value.trim());
}

/** Local timestamp without timezone suffix, e.g. 2026-11-03 09:15:30 */
export function suggestTimestamp(now = new Date()): string {
  return (
    `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())} ` +
    `${pad(now.getHours())}:${pad(now.getMinutes())}:${pad(now.getSeconds())}`
  );
}

export function suggestDate(now = new Date()): string {
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`;
}

export function suggestTime(now = new Date()): string {
  return `${pad(now.getHours())}:${pad(now.getMinutes())}:${pad(now.getSeconds())}`;
}

function formatUtcTimestamp(d: Date): string {
  return (
    `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())} ` +
    `${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}:${pad(d.getUTCSeconds())}`
  );
}

function formatUtcDate(d: Date): string {
  return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}`;
}

function formatUtcTime(d: Date): string {
  return `${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())}:${pad(d.getUTCSeconds())}`;
}

/**
 * Convert API/ISO values (e.g. 2026-11-03T08:47:34Z) into SQL-friendly literals
 * MySQL/Postgres accept as datetime/timestamp input.
 */
export function normalizeTemporalValue(value: unknown, kind: TemporalKind): unknown {
  if (value === null || value === undefined) return value;
  if (typeof value === "number" && Number.isFinite(value)) {
    const d = new Date(value);
    if (Number.isNaN(d.getTime())) return value;
    if (kind === "date") return formatUtcDate(d);
    if (kind === "time") return formatUtcTime(d);
    return formatUtcTimestamp(d);
  }
  if (value instanceof Date) {
    if (Number.isNaN(value.getTime())) return value;
    if (kind === "date") return formatUtcDate(value);
    if (kind === "time") return formatUtcTime(value);
    return formatUtcTimestamp(value);
  }
  if (typeof value !== "string") return value;
  const trimmed = value.trim();
  if (!trimmed) return value;

  if (kind === "date" && /^\d{4}-\d{2}-\d{2}$/.test(trimmed)) return trimmed;
  if (kind === "time" && /^\d{2}:\d{2}(:\d{2})?/.test(trimmed) && !/[TZtz]|[+-]\d{2}/.test(trimmed)) {
    return trimmed.length >= 8 ? trimmed.slice(0, 8) : trimmed;
  }
  if (
    kind === "timestamp" &&
    /^\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2}/.test(trimmed) &&
    !hasExplicitZone(trimmed) &&
    !trimmed.includes("T")
  ) {
    return trimmed.slice(0, 19);
  }

  // ISO / RFC3339 from the API (…T…Z)
  const d = new Date(trimmed);
  if (Number.isNaN(d.getTime())) return value;
  const useUtc = hasExplicitZone(trimmed) || trimmed.includes("T");
  if (kind === "date") return useUtc ? formatUtcDate(d) : suggestDate(d);
  if (kind === "time") return useUtc ? formatUtcTime(d) : suggestTime(d);
  return useUtc ? formatUtcTimestamp(d) : suggestTimestamp(d);
}

/** Normalize write payloads so ISO timestamps from the grid don't fail INSERT/UPDATE. */
export function normalizeWriteValues(
  values: Record<string, unknown>,
  columns: ColumnInfo[],
): Record<string, unknown> {
  const byName = new Map(columns.map((c) => [c.name, c]));
  const out: Record<string, unknown> = {};
  for (const [name, value] of Object.entries(values)) {
    const col = byName.get(name);
    const kind = col ? looksDateTime(col) : null;
    if (kind) {
      out[name] = normalizeTemporalValue(value, kind);
      continue;
    }
    if (typeof value === "string" && /^\d{4}-\d{2}-\d{2}T/.test(value.trim())) {
      out[name] = normalizeTemporalValue(value, "timestamp");
      continue;
    }
    out[name] = value;
  }
  return out;
}

/** Prefer SQL-friendly text in the inline editor when the cell holds an ISO timestamp. */
export function formatTemporalForEdit(value: unknown): string | null {
  if (value === null || value === undefined) return null;
  if (value instanceof Date) return formatUtcTimestamp(value);
  if (typeof value !== "string") return null;
  const trimmed = value.trim();
  if (!/^\d{4}-\d{2}-\d{2}T/.test(trimmed) && !hasExplicitZone(trimmed)) return null;
  const normalized = normalizeTemporalValue(trimmed, "timestamp");
  return typeof normalized === "string" ? normalized : null;
}

export function suggestUuid(): string {
  return crypto.randomUUID();
}

const ULID_ALPHABET = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";

/** Crockford Base32 ULID (26 chars). */
export function suggestUlid(now = Date.now()): string {
  let time = Math.max(0, Math.floor(now));
  let out = "";
  for (let i = 0; i < 10; i++) {
    out = ULID_ALPHABET[time % 32] + out;
    time = Math.floor(time / 32);
  }
  const rand = new Uint8Array(10);
  crypto.getRandomValues(rand);
  let carry = 0n;
  let bits = 0;
  for (const byte of rand) {
    carry = (carry << 8n) | BigInt(byte);
    bits += 8;
    while (bits >= 5) {
      bits -= 5;
      out += ULID_ALPHABET[Number((carry >> BigInt(bits)) & 31n)];
    }
  }
  return out;
}

function collectKnownInts(
  colName: string,
  colIdx: number,
  baseRows: unknown[][],
  pendingInserts: Record<string, unknown>[],
): Set<number> {
  const known = new Set<number>();
  if (colIdx >= 0) {
    for (const row of baseRows) {
      const n = Number(row[colIdx]);
      if (Number.isFinite(n)) known.add(Math.floor(n));
    }
  }
  for (const vals of pendingInserts) {
    const n = Number(vals[colName]);
    if (Number.isFinite(n)) known.add(Math.floor(n));
  }
  return known;
}

/**
 * Next auto-increment candidates from loaded rows + pending inserts.
 * When `sourceHints` is set (clone), start from that row's id + 1 and skip collisions
 * with ids already visible on the page / pending inserts.
 */
export function nextAutoValues(
  columns: ColumnInfo[],
  columnNames: string[],
  baseRows: unknown[][],
  pendingInserts: Record<string, unknown>[],
  sourceHints?: Record<string, unknown>,
): Record<string, number> {
  const out: Record<string, number> = {};
  for (const col of columns) {
    if (!isAutoColumn(col)) continue;
    const idx = columnNames.indexOf(col.name);
    const known = collectKnownInts(col.name, idx, baseRows, pendingInserts);

    let start = 1;
    if (sourceHints && col.name in sourceHints) {
      const src = Number(sourceHints[col.name]);
      if (Number.isFinite(src)) start = Math.floor(src) + 1;
    } else if (known.size > 0) {
      start = Math.max(...known) + 1;
    }

    let candidate = start;
    while (known.has(candidate)) candidate += 1;
    out[col.name] = candidate;
  }
  return out;
}

export type SuggestOptions = {
  now?: Date;
  /** Precomputed next values for auto-increment / serial columns */
  nextAuto?: Record<string, number>;
};

/**
 * Build initial insert values with auto-increment, UUID/ULID, and date-time suggestions.
 */
export function suggestInsertValues(columns: ColumnInfo[], opts: SuggestOptions = {}): Record<string, unknown> {
  const now = opts.now ?? new Date();
  const nextAuto = opts.nextAuto ?? {};
  const values: Record<string, unknown> = {};
  for (const col of columns) {
    if (isAutoColumn(col)) {
      if (col.name in nextAuto) values[col.name] = nextAuto[col.name];
      // else omit so the DB can generate
      continue;
    }
    if (looksUlid(col)) {
      values[col.name] = suggestUlid(now.getTime());
      continue;
    }
    if (looksUuid(col)) {
      values[col.name] = suggestUuid();
      continue;
    }
    const kind = looksDateTime(col);
    if (kind === "timestamp") {
      values[col.name] = suggestTimestamp(now);
      continue;
    }
    if (kind === "date") {
      values[col.name] = suggestDate(now);
      continue;
    }
    if (kind === "time") {
      values[col.name] = suggestTime(now);
      continue;
    }
    values[col.name] = null;
  }
  return values;
}

/**
 * Clone a row into insert values: copy fields, mint new auto/UUID/ULID, normalize timestamps.
 */
export function cloneInsertValues(
  columns: ColumnInfo[],
  source: Record<string, unknown>,
  opts: SuggestOptions = {},
): Record<string, unknown> {
  const now = opts.now ?? new Date();
  const nextAuto = opts.nextAuto ?? {};
  const values: Record<string, unknown> = {};
  for (const col of columns) {
    if (isAutoColumn(col)) {
      if (col.name in nextAuto) values[col.name] = nextAuto[col.name];
      continue;
    }
    if (looksUlid(col)) {
      values[col.name] = suggestUlid(now.getTime());
      continue;
    }
    if (looksUuid(col)) {
      values[col.name] = suggestUuid();
      continue;
    }
    const kind = looksDateTime(col);
    if (col.name in source) {
      const raw = source[col.name];
      values[col.name] = kind ? normalizeTemporalValue(raw, kind) : raw;
      continue;
    }
    if (kind === "timestamp") values[col.name] = suggestTimestamp(now);
    else if (kind === "date") values[col.name] = suggestDate(now);
    else if (kind === "time") values[col.name] = suggestTime(now);
    else values[col.name] = null;
  }
  return values;
}

export function isOmittedAuto(
  column: string,
  values: Record<string, unknown>,
  columns: ColumnInfo[],
): boolean {
  if (column in values) return false;
  const col = columns.find((c) => c.name === column);
  return !!col && isAutoColumn(col);
}
