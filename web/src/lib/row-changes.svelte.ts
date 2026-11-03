import { SvelteSet } from "svelte/reactivity";
import type { ColumnInfo } from "./api";
import { cellText } from "./format";
import {
  cloneInsertValues,
  formatTemporalForEdit,
  nextAutoValues,
  suggestInsertValues,
} from "./insert-suggest";

export type PendingOp =
  | { id: string; kind: "edit"; rowId: string; column: string; from: unknown; to: unknown }
  | { id: string; kind: "insert"; rowId: string; values: Record<string, unknown> }
  | { id: string; kind: "delete"; rowId: string; key: Record<string, unknown>; snapshot: unknown[] }
  | {
      id: string;
      kind: "bulk-edit";
      column: string;
      to: unknown;
      rows: { rowId: string; from: unknown }[];
    }
  | {
      id: string;
      kind: "bulk-delete";
      rows: { rowId: string; key: Record<string, unknown>; snapshot: unknown[] }[];
    };

export type DisplayRow = {
  rowId: string;
  values: unknown[];
  source: "existing" | "insert";
  deleted: boolean;
  key: Record<string, unknown> | null;
  /** 1-based op index for insert badge */
  insertOpIndex: number | null;
  /** 1-based op index for delete badge */
  deleteOpIndex: number | null;
  /** column → 1-based op index for cell edit badge */
  cellOpIndex: Record<string, number>;
};

export type ApplyAction =
  | { kind: "insert"; values: Record<string, unknown> }
  | { kind: "update"; key: Record<string, unknown>; values: Record<string, unknown> }
  | { kind: "delete"; key: Record<string, unknown> };

let opSeq = 0;

function nextId(prefix: string): string {
  opSeq += 1;
  return `${prefix}-${opSeq}-${Math.random().toString(36).slice(2, 8)}`;
}

export function draftRowId(): string {
  return `draft:${nextId("r")}`;
}

/** Stable id from primary-key map. */
export function rowIdFromKey(key: Record<string, unknown>): string {
  const parts = Object.keys(key)
    .sort()
    .map((k) => `${k}=${JSON.stringify(key[k] ?? null)}`);
  return parts.join("|");
}

export function buildRowKey(
  row: unknown[],
  columns: string[],
  primaryKey: string[],
): Record<string, unknown> | null {
  if (primaryKey.length === 0) return null;
  const key: Record<string, unknown> = {};
  for (const col of primaryKey) {
    const idx = columns.indexOf(col);
    if (idx < 0) return null;
    key[col] = row[idx];
  }
  return key;
}

export function parseCellInput(text: string, isNull: boolean): unknown {
  if (isNull) return null;
  const trimmed = text.trim();
  if (trimmed.startsWith("{") || trimmed.startsWith("[")) {
    try {
      return JSON.parse(trimmed);
    } catch {
      /* keep string */
    }
  }
  if (trimmed !== "" && /^-?\d+(\.\d+)?([eE][+-]?\d+)?$/.test(trimmed)) {
    return Number(trimmed);
  }
  if (/^(true|false)$/i.test(trimmed)) {
    return trimmed.toLowerCase() === "true";
  }
  return text;
}

export function editInputText(value: unknown): string {
  if (value === null || value === undefined) return "";
  const temporal = formatTemporalForEdit(value);
  if (temporal !== null) return temporal;
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}

export function valuesEqual(a: unknown, b: unknown): boolean {
  if (Object.is(a, b)) return true;
  if (a === null || b === null || a === undefined || b === undefined) return a === b;
  if (typeof a === "object" || typeof b === "object") {
    try {
      return JSON.stringify(a) === JSON.stringify(b);
    } catch {
      return false;
    }
  }
  return false;
}

function summarizeValue(value: unknown, max = 36): string {
  const t = cellText(value);
  return t.length > max ? `${t.slice(0, Math.max(1, max - 1))}…` : t;
}

/** Human-readable PK / row identity from a key map. */
export function formatRowKey(key: Record<string, unknown>, maxParts = 4): string {
  const entries = Object.entries(key);
  if (entries.length === 0) return "row";
  const parts = entries.slice(0, maxParts).map(([k, v]) => `${k}=${summarizeValue(v, 24)}`);
  if (entries.length > maxParts) parts.push(`+${entries.length - maxParts}`);
  return parts.join(", ");
}

function formatRowId(rowId: string): string {
  if (rowId.startsWith("draft:")) return "new row";
  if (rowId.startsWith("idx:")) return `row ${rowId.slice(4)}`;
  try {
    const key: Record<string, unknown> = {};
    for (const part of rowId.split("|")) {
      const eq = part.indexOf("=");
      if (eq < 0) return rowId;
      key[part.slice(0, eq)] = JSON.parse(part.slice(eq + 1));
    }
    return formatRowKey(key);
  } catch {
    return rowId;
  }
}

function formatChange(from: unknown, to: unknown): string {
  return `${summarizeValue(from)} → ${summarizeValue(to)}`;
}

function previewRecord(values: Record<string, unknown>, limit = 4): string {
  const entries = Object.entries(values).filter(([, v]) => v !== undefined);
  if (entries.length === 0) return "(defaults / auto)";
  const parts = entries.slice(0, limit).map(([k, v]) => `${k}=${summarizeValue(v, 20)}`);
  if (entries.length > limit) parts.push(`+${entries.length - limit} more`);
  return parts.join(", ");
}

function listKeys(rows: { key: Record<string, unknown> }[], limit = 3): string {
  const parts = rows.slice(0, limit).map((r) => formatRowKey(r.key, 2));
  if (rows.length > limit) parts.push(`+${rows.length - limit} more`);
  return parts.join("; ");
}

export type OpSummary = {
  verb: string;
  title: string;
  detail: string;
  tone: "edit" | "insert" | "delete";
};

/** Rich one/two-line summary for the pending-ops sheet. */
export function opSummary(op: PendingOp): OpSummary {
  switch (op.kind) {
    case "edit":
      return {
        verb: "UPDATE",
        tone: "edit",
        title: `${op.column}: ${formatChange(op.from, op.to)}`,
        detail: formatRowId(op.rowId),
      };
    case "insert":
      return {
        verb: "INSERT",
        tone: "insert",
        title: previewRecord(op.values),
        detail: "new row",
      };
    case "delete":
      return {
        verb: "DELETE",
        tone: "delete",
        title: formatRowKey(op.key),
        detail: "remove row",
      };
    case "bulk-edit": {
      const samples = op.rows
        .slice(0, 3)
        .map((r) => `${formatRowId(r.rowId)} (${summarizeValue(r.from, 16)} → ${summarizeValue(op.to, 16)})`);
      if (op.rows.length > 3) samples.push(`+${op.rows.length - 3} more`);
      return {
        verb: "UPDATE",
        tone: "edit",
        title: `${op.column} → ${summarizeValue(op.to)} · ${op.rows.length} rows`,
        detail: samples.join("; "),
      };
    }
    case "bulk-delete":
      return {
        verb: "DELETE",
        tone: "delete",
        title: `${op.rows.length} row${op.rows.length === 1 ? "" : "s"}`,
        detail: listKeys(op.rows),
      };
  }
}

/** Short single-line label (toolbar / a11y). */
export function opLabel(op: PendingOp): string {
  const s = opSummary(op);
  return s.detail ? `${s.verb} ${s.title} · ${s.detail}` : `${s.verb} ${s.title}`;
}

const ROW_CHANGES_KEY = "dbadminer:row-changes";

export function rowChangesStorageKey(database: string, table: string): string {
  return `${ROW_CHANGES_KEY}:${database || "_"}:${table}`;
}

function isPendingOp(value: unknown): value is PendingOp {
  if (!value || typeof value !== "object") return false;
  const kind = (value as PendingOp).kind;
  return (
    kind === "edit" ||
    kind === "insert" ||
    kind === "delete" ||
    kind === "bulk-edit" ||
    kind === "bulk-delete"
  );
}

export class RowChanges {
  ops = $state.raw<PendingOp[]>([]);
  selectedRowIds = new SvelteSet<string>();
  sheetExpanded = $state(false);
  applying = $state(false);
  #storageKey = "";

  get pending(): boolean {
    return this.ops.length > 0;
  }

  /** Switch active table scope and load any saved ops for it. */
  bindScope(database: string, table: string): void {
    this.#storageKey = rowChangesStorageKey(database, table);
    this.selectedRowIds.clear();
    this.load();
  }

  #setOps(next: PendingOp[], expandIfPending = true): void {
    this.ops = next;
    if (next.length === 0) this.sheetExpanded = false;
    else if (expandIfPending) this.sheetExpanded = true;
    this.persist();
  }

  persist(): void {
    if (!this.#storageKey || typeof localStorage === "undefined") return;
    try {
      if (this.ops.length === 0) {
        localStorage.removeItem(this.#storageKey);
        return;
      }
      localStorage.setItem(this.#storageKey, JSON.stringify({ ops: this.ops, at: Date.now() }));
    } catch {
      /* quota / private mode */
    }
  }

  load(): void {
    if (!this.#storageKey || typeof localStorage === "undefined") {
      this.ops = [];
      this.sheetExpanded = false;
      return;
    }
    try {
      const raw = JSON.parse(localStorage.getItem(this.#storageKey) ?? "null");
      const ops = Array.isArray(raw?.ops) ? raw.ops.filter(isPendingOp) : [];
      this.ops = ops;
      this.sheetExpanded = ops.length > 0;
    } catch {
      this.ops = [];
      this.sheetExpanded = false;
    }
  }

  clear(): void {
    this.#setOps([]);
    this.selectedRowIds.clear();
  }

  discard(): void {
    this.clear();
  }

  toggleSelect(rowId: string): void {
    if (this.isPendingDelete(rowId)) {
      this.selectedRowIds.delete(rowId);
      return;
    }
    if (this.selectedRowIds.has(rowId)) this.selectedRowIds.delete(rowId);
    else this.selectedRowIds.add(rowId);
  }

  setSelectAll(rowIds: string[], checked: boolean): void {
    if (checked) {
      for (const id of rowIds) {
        if (!this.isPendingDelete(id)) this.selectedRowIds.add(id);
      }
    } else {
      for (const id of rowIds) this.selectedRowIds.delete(id);
    }
  }

  /** Row already staged for delete (single or bulk) — not bulk-selectable. */
  isPendingDelete(rowId: string): boolean {
    return this.ops.some((o) => {
      if (o.kind === "delete") return o.rowId === rowId;
      if (o.kind === "bulk-delete") return o.rows.some((r) => r.rowId === rowId);
      return false;
    });
  }

  deselect(...rowIds: string[]): void {
    for (const id of rowIds) this.selectedRowIds.delete(id);
  }

  removeOp(id: string): void {
    const target = this.ops.find((o) => o.id === id);
    if (!target) return;
    let next = this.ops.filter((o) => o.id !== id);
    if (target.kind === "insert") {
      next = next.filter((o) => !(o.kind === "edit" && o.rowId === target.rowId));
      this.selectedRowIds.delete(target.rowId);
    }
    this.#setOps(next, false);
  }

  stageEdit(rowId: string, column: string, from: unknown, to: unknown): void {
    if (valuesEqual(from, to)) {
      const existing = this.ops.find((o) => o.kind === "edit" && o.rowId === rowId && o.column === column);
      if (existing?.kind === "edit" && valuesEqual(existing.from, to)) {
        this.removeOp(existing.id);
      }
      return;
    }
    const insert = this.ops.find((o) => o.kind === "insert" && o.rowId === rowId);
    if (insert && insert.kind === "insert") {
      this.#setOps(
        this.ops.map((o) => {
          if (o.id !== insert.id || o.kind !== "insert") return o;
          return { ...o, values: { ...o.values, [column]: to } };
        }),
      );
      return;
    }
    const existingIdx = this.ops.findIndex(
      (o) => o.kind === "edit" && o.rowId === rowId && o.column === column,
    );
    if (existingIdx >= 0) {
      const prev = this.ops[existingIdx];
      if (prev.kind !== "edit") return;
      if (valuesEqual(prev.from, to)) {
        this.removeOp(prev.id);
        return;
      }
      const copy = this.ops.slice();
      copy[existingIdx] = { ...prev, to };
      this.#setOps(copy);
      return;
    }
    this.#setOps([...this.ops, { id: nextId("edit"), kind: "edit", rowId, column, from, to }]);
  }

  #pendingInsertValues(): Record<string, unknown>[] {
    return this.ops.filter((o): o is Extract<PendingOp, { kind: "insert" }> => o.kind === "insert").map((o) => o.values);
  }

  stageInsert(
    columns: string[] | ColumnInfo[],
    opts?: { columnNames?: string[]; baseRows?: unknown[][] },
  ): string {
    const rowId = draftRowId();
    const infos: ColumnInfo[] = columns.map((c) =>
      typeof c === "string"
        ? { name: c, type: "TEXT", nullable: true, primaryKey: false, position: 0 }
        : c,
    );
    const columnNames = opts?.columnNames ?? infos.map((c) => c.name);
    const nextAuto = nextAutoValues(infos, columnNames, opts?.baseRows ?? [], this.#pendingInsertValues());
    const values = suggestInsertValues(infos, { nextAuto });
    this.#setOps([...this.ops, { id: nextId("ins"), kind: "insert", rowId, values }]);
    return rowId;
  }

  /** Stage an insert cloned from an existing (or draft) display row. */
  stageClone(columns: ColumnInfo[], columnNames: string[], sourceValues: unknown[], baseRows: unknown[][] = []): string {
    const rowId = draftRowId();
    const source: Record<string, unknown> = {};
    for (let i = 0; i < columnNames.length; i++) {
      const name = columnNames[i];
      if (!name) continue;
      const v = sourceValues[i];
      if (v !== undefined) source[name] = v;
    }
    const nextAuto = nextAutoValues(
      columns,
      columnNames,
      baseRows,
      this.#pendingInsertValues(),
      source, // clone: start from source id + 1
    );
    const values = cloneInsertValues(columns, source, { nextAuto });
    this.#setOps([...this.ops, { id: nextId("ins"), kind: "insert", rowId, values }]);
    return rowId;
  }

  stageDelete(rowId: string, key: Record<string, unknown>, snapshot: unknown[]): void {
    if (rowId.startsWith("draft:")) {
      const insert = this.ops.find((o) => o.kind === "insert" && o.rowId === rowId);
      if (insert) this.removeOp(insert.id);
      return;
    }
    if (this.ops.some((o) => o.kind === "delete" && o.rowId === rowId)) return;
    if (
      this.ops.some(
        (o) => o.kind === "bulk-delete" && o.rows.some((r) => r.rowId === rowId),
      )
    ) {
      return;
    }
    this.#setOps([
      ...this.ops,
      { id: nextId("del"), kind: "delete", rowId, key, snapshot: snapshot.slice() },
    ]);
    this.deselect(rowId);
  }

  stageBulkDelete(
    rows: { rowId: string; key: Record<string, unknown>; snapshot: unknown[] }[],
  ): void {
    const existing = new Set(
      this.ops.flatMap((o) => {
        if (o.kind === "delete") return [o.rowId];
        if (o.kind === "bulk-delete") return o.rows.map((r) => r.rowId);
        return [];
      }),
    );
    const fresh = rows.filter((r) => !r.rowId.startsWith("draft:") && !existing.has(r.rowId));
    for (const r of rows) {
      if (r.rowId.startsWith("draft:")) {
        const insert = this.ops.find((o) => o.kind === "insert" && o.rowId === r.rowId);
        if (insert) this.removeOp(insert.id);
      }
    }
    // Always drop selection for targeted rows so they can't be bulk-acted again.
    this.deselect(...rows.map((r) => r.rowId));
    if (fresh.length === 0) return;
    this.#setOps([
      ...this.ops,
      {
        id: nextId("bdel"),
        kind: "bulk-delete",
        rows: fresh.map((r) => ({ ...r, snapshot: r.snapshot.slice() })),
      },
    ]);
  }

  stageBulkEdit(column: string, to: unknown, rows: { rowId: string; from: unknown }[]): void {
    const usable = rows.filter(
      (r) =>
        !r.rowId.startsWith("draft:") &&
        !this.isPendingDelete(r.rowId) &&
        !valuesEqual(r.from, to),
    );
    for (const r of rows) {
      if (r.rowId.startsWith("draft:")) {
        this.stageEdit(r.rowId, column, r.from, to);
      }
    }
    this.deselect(...rows.map((r) => r.rowId));
    if (usable.length === 0) return;
    this.#setOps([
      ...this.ops,
      { id: nextId("bedit"), kind: "bulk-edit", column, to, rows: usable },
    ]);
  }

  fold(
    baseRows: unknown[][],
    columns: string[],
    primaryKey: string[],
  ): DisplayRow[] {
    const out: DisplayRow[] = [];
    const byId = new Map<string, DisplayRow>();

    for (const row of baseRows) {
      const key = buildRowKey(row, columns, primaryKey);
      const rowId = key ? rowIdFromKey(key) : `idx:${out.length}`;
      const display: DisplayRow = {
        rowId,
        values: row.slice(),
        source: "existing",
        deleted: false,
        key,
        insertOpIndex: null,
        deleteOpIndex: null,
        cellOpIndex: {},
      };
      out.push(display);
      byId.set(rowId, display);
    }

    this.ops.forEach((op, i) => {
      const index = i + 1;
      switch (op.kind) {
        case "edit": {
          const row = byId.get(op.rowId);
          if (!row || row.deleted) break;
          const colIdx = columns.indexOf(op.column);
          if (colIdx >= 0) row.values[colIdx] = op.to;
          row.cellOpIndex[op.column] = index;
          break;
        }
        case "bulk-edit": {
          for (const r of op.rows) {
            const row = byId.get(r.rowId);
            if (!row || row.deleted) continue;
            const colIdx = columns.indexOf(op.column);
            if (colIdx >= 0) row.values[colIdx] = op.to;
            row.cellOpIndex[op.column] = index;
          }
          break;
        }
        case "insert": {
          // Missing keys = DB-generated (auto) columns; keep undefined for UI to show "(auto)"
          const values = columns.map((c) => (c in op.values ? op.values[c] : undefined));
          const display: DisplayRow = {
            rowId: op.rowId,
            values,
            source: "insert",
            deleted: false,
            key: null,
            insertOpIndex: index,
            deleteOpIndex: null,
            cellOpIndex: {},
          };
          out.push(display);
          byId.set(op.rowId, display);
          break;
        }
        case "delete": {
          const row = byId.get(op.rowId);
          if (row) {
            row.deleted = true;
            row.deleteOpIndex = index;
          }
          break;
        }
        case "bulk-delete": {
          for (const r of op.rows) {
            const row = byId.get(r.rowId);
            if (row) {
              row.deleted = true;
              row.deleteOpIndex = index;
            }
          }
          break;
        }
      }
    });

    return out;
  }

  /** Original cell value before pending edits (for edit from=). */
  originalValue(
    rowId: string,
    column: string,
    columns: string[],
    baseRows: unknown[][],
    primaryKey: string[],
  ): unknown {
    const insert = this.ops.find((o) => o.kind === "insert" && o.rowId === rowId);
    if (insert && insert.kind === "insert") return insert.values[column] ?? null;

    for (const row of baseRows) {
      const key = buildRowKey(row, columns, primaryKey);
      if (!key || rowIdFromKey(key) !== rowId) continue;
      const idx = columns.indexOf(column);
      return idx >= 0 ? row[idx] : null;
    }
    return null;
  }

  buildApplyActions(columns: string[], primaryKey: string[]): ApplyAction[] {
    type RowPlan = {
      insert?: Record<string, unknown>;
      updates: Record<string, unknown>;
      key?: Record<string, unknown>;
      delete?: boolean;
    };
    const plans = new Map<string, RowPlan>();

    const plan = (rowId: string): RowPlan => {
      let p = plans.get(rowId);
      if (!p) {
        p = { updates: {} };
        plans.set(rowId, p);
      }
      return p;
    };

    for (const op of this.ops) {
      switch (op.kind) {
        case "insert":
          plan(op.rowId).insert = { ...op.values };
          break;
        case "edit": {
          const p = plan(op.rowId);
          if (p.insert) p.insert[op.column] = op.to;
          else p.updates[op.column] = op.to;
          break;
        }
        case "bulk-edit": {
          for (const r of op.rows) {
            const p = plan(r.rowId);
            if (p.insert) p.insert[op.column] = op.to;
            else p.updates[op.column] = op.to;
          }
          break;
        }
        case "delete": {
          const p = plan(op.rowId);
          p.delete = true;
          p.key = op.key;
          break;
        }
        case "bulk-delete": {
          for (const r of op.rows) {
            const p = plan(r.rowId);
            p.delete = true;
            p.key = r.key;
          }
          break;
        }
      }
    }

    // Resolve keys for updates from PK columns in updates/insert or existing key
    const actions: ApplyAction[] = [];
    for (const [rowId, p] of plans) {
      if (p.insert && p.delete) continue; // cancelled draft
      if (p.insert) {
        // Only send keys present on the insert (auto columns stay omitted)
        const values: Record<string, unknown> = { ...p.insert };
        actions.push({ kind: "insert", values });
        continue;
      }
      if (p.delete && p.key) {
        actions.push({ kind: "delete", key: p.key });
        continue;
      }
      if (Object.keys(p.updates).length > 0) {
        const key = p.key ?? keyFromRowId(rowId, primaryKey) ?? undefined;
        if (!key) continue;
        actions.push({ kind: "update", key, values: p.updates });
      }
    }
    return actions;
  }
}

function keyFromRowId(rowId: string, primaryKey: string[]): Record<string, unknown> | null {
  if (rowId.startsWith("draft:") || rowId.startsWith("idx:")) return null;
  try {
    const key: Record<string, unknown> = {};
    for (const part of rowId.split("|")) {
      const eq = part.indexOf("=");
      if (eq < 0) return null;
      const col = part.slice(0, eq);
      key[col] = JSON.parse(part.slice(eq + 1));
    }
    for (const pk of primaryKey) {
      if (!(pk in key)) return null;
    }
    return key;
  } catch {
    return null;
  }
}
