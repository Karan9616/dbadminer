import { createContext } from "svelte";
import { SvelteMap } from "svelte/reactivity";
import { toast as notify } from "svelte-sonner";
import {
  api,
  setDatabase,
  tableKey,
  type AlterOp,
  type ColumnDef,
  type DatabaseInfo,
  type Info,
  type ResultSet,
  type TableDetail,
  type TableInfo,
  type TableSchema,
} from "./api";
import { normalizeWriteValues } from "./insert-suggest";
import { RowChanges } from "./row-changes.svelte";
import { splitStatements } from "./sql-split";
import { applyTheme, loadThemePreference, saveThemePreference, type ThemePreference } from "./theme";

export type View = "data" | "structure" | "sql";
export type WriteDialog =
  | null
  | { kind: "create-table" }
  | { kind: "create-view" }
  | { kind: "drop"; table: TableInfo }
  | { kind: "add-column"; table: TableInfo }
  | { kind: "create-index"; table: TableInfo }
  | { kind: "create-fk"; table: TableInfo }
  | { kind: "insert"; table: TableInfo }
  | { kind: "edit"; table: TableInfo; row: unknown[]; columns: string[] };

export interface HistoryEntry {
  sql: string;
  at: number;
  ok: boolean;
  ms: number;
  rowsAffected?: number;
  mutated?: boolean;
}

const HISTORY_KEY = "dbadminer:history";
const DATABASE_KEY = "dbadminer:database";
const TABLE_KEY = "dbadminer:table";
const PAGE_SIZE_KEY = "dbadminer:page-size";
const HISTORY_MAX = 50;
export const PAGE_SIZES = [25, 50, 100, 200] as const;

function historyKey(name: string): string {
  return name ? `${HISTORY_KEY}:${name}` : HISTORY_KEY;
}

function tableStorageKey(database: string): string {
  return database ? `${TABLE_KEY}:${database}` : TABLE_KEY;
}

function loadSavedTable(database: string): { schema: string; name: string } | null {
  try {
    const raw = JSON.parse(localStorage.getItem(tableStorageKey(database)) ?? "null");
    if (raw && typeof raw.schema === "string" && typeof raw.name === "string") {
      return { schema: raw.schema, name: raw.name };
    }
  } catch {
    /* ignore */
  }
  return null;
}

function saveSelectedTable(database: string, table: TableInfo | null): void {
  const key = tableStorageKey(database);
  if (!table) {
    localStorage.removeItem(key);
    return;
  }
  localStorage.setItem(key, JSON.stringify({ schema: table.schema, name: table.name }));
}

function loadHistory(name = ""): HistoryEntry[] {
  try {
    const raw = JSON.parse(localStorage.getItem(historyKey(name)) ?? "[]");
    return Array.isArray(raw) ? raw.filter((e) => typeof e?.sql === "string").slice(0, HISTORY_MAX) : [];
  } catch {
    return [];
  }
}

function loadPageSize(): number {
  const n = Number(localStorage.getItem(PAGE_SIZE_KEY));
  return (PAGE_SIZES as readonly number[]).includes(n) ? n : 50;
}

function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

export class GridState {
  data = $state.raw<ResultSet | null>(null);
  loading = $state(false);
  error = $state<string | null>(null);
  limit = $state(loadPageSize());
  offset = $state(0);
  order = $state("");
  dir = $state<"asc" | "desc">("asc");
  total = $state<number | null>(null);
  wantCount = $state(false);
  durationMs = $state<number | null>(null);

  reset(): void {
    this.data = null;
    this.error = null;
    this.offset = 0;
    this.order = "";
    this.dir = "asc";
    this.total = null;
    this.durationMs = null;
  }
}

export class SqlState {
  text = $state("");
  result = $state.raw<ResultSet | null>(null);
  results = $state.raw<{ sql: string; result?: ResultSet; error?: string }[]>([]);
  running = $state(false);
  error = $state<string | null>(null);
  durationMs = $state<number | null>(null);
  lastSql = $state("");
}

export class AppState {
  databases = $state.raw<DatabaseInfo[]>([]);
  database = $state("");
  info = $state.raw<Info | null>(null);
  tables = $state.raw<TableInfo[]>([]);
  tablesLoading = $state(true);
  tablesError = $state<string | null>(null);
  selected = $state.raw<TableInfo | null>(null);
  view = $state<View>("data");
  details = new SvelteMap<string, TableDetail>();
  schemas = new SvelteMap<string, TableSchema>();
  detailErrors = new SvelteMap<string, string>();
  grid = new GridState();
  sql = new SqlState();
  rowChanges = new RowChanges();
  history = $state.raw<HistoryEntry[]>(loadHistory());
  theme = $state<ThemePreference>(loadThemePreference());
  paletteOpen = $state(false);
  writeDialog = $state.raw<WriteDialog>(null);

  #rowsSeq = 0;

  readOnly = $derived(this.info?.access !== "full");

  defaultSchema = $derived.by(() => {
    if (this.info?.schema) return this.info.schema;
    if (this.selected?.schema) return this.selected.schema;
    const first = this.tables[0]?.schema;
    if (first) return first;
    switch (this.info?.dialect) {
      case "postgres":
        return "public";
      case "sqlite":
        return "main";
      default:
        return "";
    }
  });

  async init(): Promise<void> {
    try {
      this.databases = await api.databases();
    } catch (err) {
      this.databases = [];
      this.toast("error", `Could not load databases: ${message(err)}`);
    }
    const saved = localStorage.getItem(DATABASE_KEY) ?? "";
    const chosen = this.databases.some((db) => db.name === saved) ? saved : (this.databases[0]?.name ?? "");
    await this.useDatabase(chosen, false);
  }

  async selectDatabase(name: string): Promise<void> {
    if (!name || name === this.database) return;
    await this.useDatabase(name, true);
  }

  async useDatabase(name: string, reset: boolean): Promise<void> {
    this.database = name;
    setDatabase(name);
    if (name) localStorage.setItem(DATABASE_KEY, name);
    this.history = loadHistory(name);
    if (reset) {
      this.selected = null;
      this.grid.reset();
      this.rowChanges.bindScope(name, "");
      this.rowChanges.clear();
      this.sql.result = null;
      this.sql.results = [];
      this.sql.error = null;
      this.sql.durationMs = null;
      this.details.clear();
      this.schemas.clear();
      this.detailErrors.clear();
      this.tables = [];
    }
    try {
      this.info = await api.info();
    } catch (err) {
      this.info = null;
      this.toast("error", `Could not load server info: ${message(err)}`);
    }
    await this.loadTables();
  }

  async loadTables(): Promise<void> {
    this.tablesLoading = true;
    this.tablesError = null;
    try {
      this.tables = await api.tables();
      await this.#restoreSelectedTable();
    } catch (err) {
      this.tablesError = message(err);
      this.toast("error", this.tablesError);
    } finally {
      this.tablesLoading = false;
    }
  }

  async #restoreSelectedTable(): Promise<void> {
    if (this.selected) {
      const stillThere = this.tables.some(
        (t) => t.schema === this.selected!.schema && t.name === this.selected!.name,
      );
      if (stillThere) return;
      this.selected = null;
      saveSelectedTable(this.database, null);
    }
    const saved = loadSavedTable(this.database);
    if (!saved) return;
    const match = this.tables.find((t) => t.schema === saved.schema && t.name === saved.name);
    if (match) await this.selectTable(match, this.view === "sql" ? "data" : this.view);
    else saveSelectedTable(this.database, null);
  }

  async selectTable(table: TableInfo, view: View = this.view === "sql" ? "data" : this.view): Promise<void> {
    this.selected = table;
    this.view = view;
    saveSelectedTable(this.database, table);
    this.grid.reset();
    this.rowChanges.bindScope(this.database, tableKey(table));
    await Promise.all([this.loadRows(), this.loadDetail(table), this.loadSchema(table)]);
  }

  async loadRows(): Promise<void> {
    const t = this.selected;
    if (!t) return;
    const g = this.grid;
    const seq = ++this.#rowsSeq;
    g.loading = true;
    g.error = null;
    const started = performance.now();
    try {
      const data = await api.rows(t.schema, t.name, {
        limit: g.limit,
        offset: g.offset,
        order: g.order || undefined,
        dir: g.dir,
        count: g.wantCount,
      });
      if (seq !== this.#rowsSeq) return;
      g.data = data;
      g.durationMs = performance.now() - started;
      if (typeof data.total === "number") g.total = data.total;
      this.rowChanges.selectedRowIds.clear();
    } catch (err) {
      if (seq !== this.#rowsSeq) return;
      g.error = message(err);
      this.toast("error", g.error);
    } finally {
      if (seq === this.#rowsSeq) g.loading = false;
    }
  }

  async navigateRows(action: () => void | Promise<void>): Promise<void> {
    // Pending ops are persisted per table and keyed by PK; keep them across page/sort.
    this.rowChanges.selectedRowIds.clear();
    await action();
  }

  async applyRowChanges(): Promise<void> {
    const table = this.selected;
    if (!table || this.rowChanges.applying) return;
    const key = tableKey(table);
    const detail = this.schemas.get(key) ?? this.details.get(key);
    const columns = this.grid.data?.columns ?? detail?.columns.map((c) => c.name) ?? [];
    const pk = detail?.primaryKey ?? [];
    const actions = this.rowChanges.buildApplyActions(columns, pk);
    if (actions.length === 0) {
      this.rowChanges.clear();
      return;
    }
    const colInfos = detail?.columns ?? [];
    this.rowChanges.applying = true;
    try {
      for (const action of actions) {
        if (action.kind === "insert") {
          await api.insertRow(table.schema, table.name, normalizeWriteValues(action.values, colInfos));
        } else if (action.kind === "update") {
          await api.updateRow(
            table.schema,
            table.name,
            action.key,
            normalizeWriteValues(action.values, colInfos),
          );
        } else {
          await api.deleteRow(table.schema, table.name, action.key);
        }
      }
      this.toast("success", `Applied ${actions.length} change${actions.length === 1 ? "" : "s"}`);
      this.rowChanges.clear();
      await this.loadRows();
    } catch (err) {
      this.toast("error", message(err));
      await this.loadRows();
    } finally {
      this.rowChanges.applying = false;
    }
  }

  async loadDetail(table: TableInfo): Promise<TableDetail | undefined> {
    const key = tableKey(table);
    const cached = this.details.get(key);
    if (cached) return cached;
    try {
      const detail = await api.table(table.schema, table.name);
      this.details.set(key, detail);
      this.detailErrors.delete(key);
      return detail;
    } catch (err) {
      this.detailErrors.set(key, message(err));
      return undefined;
    }
  }

  async loadSchema(table: TableInfo): Promise<TableSchema | undefined> {
    const key = tableKey(table);
    try {
      const schema = await api.schema(table.schema, table.name);
      this.schemas.set(key, schema);
      this.details.set(key, schema);
      this.detailErrors.delete(key);
      return schema;
    } catch (err) {
      this.detailErrors.set(key, message(err));
      return undefined;
    }
  }

  invalidateTable(table?: { schema: string; name: string } | null): void {
    if (!table) return;
    const key = tableKey(table);
    this.details.delete(key);
    this.schemas.delete(key);
  }

  sortBy(column: string): void {
    void this.navigateRows(() => {
      const g = this.grid;
      if (g.order === column) {
        if (g.dir === "asc") g.dir = "desc";
        else {
          g.order = "";
          g.dir = "asc";
        }
      } else {
        g.order = column;
        g.dir = "asc";
      }
      g.offset = 0;
      return this.loadRows();
    });
  }

  setPage(offset: number): void {
    void this.navigateRows(() => {
      this.grid.offset = Math.max(0, offset);
      return this.loadRows();
    });
  }

  setPageSize(limit: number): void {
    void this.navigateRows(() => {
      this.grid.limit = limit;
      this.grid.offset = 0;
      localStorage.setItem(PAGE_SIZE_KEY, String(limit));
      return this.loadRows();
    });
  }

  toggleCount(): void {
    void this.navigateRows(() => {
      this.grid.wantCount = !this.grid.wantCount;
      if (!this.grid.wantCount) this.grid.total = null;
      return this.loadRows();
    });
  }

  async runSql(sql: string = this.sql.text): Promise<void> {
    const text = sql.trim();
    if (!text || this.sql.running) return;
    const statements = this.readOnly ? [text] : splitStatements(text);
    if (statements.length === 0) return;
    if (this.readOnly && statements.length > 1) {
      this.sql.error = "Multiple statements are not allowed in read-only mode";
      this.toast("error", this.sql.error);
      return;
    }

    const s = this.sql;
    s.running = true;
    s.error = null;
    s.results = [];
    s.result = null;
    s.lastSql = text;
    const started = performance.now();
    let ok = true;
    let lastAffected: number | undefined;
    let mutated = false;
    try {
      for (const stmt of statements) {
        try {
          const result = await api.query(stmt);
          s.results = [...s.results, { sql: stmt, result }];
          s.result = result;
          if (typeof result.rowsAffected === "number") {
            lastAffected = result.rowsAffected;
            mutated = true;
          }
        } catch (err) {
          ok = false;
          const msg = message(err);
          s.results = [...s.results, { sql: stmt, error: msg }];
          s.error = msg;
          this.toast("error", msg);
          break;
        }
      }
      if (ok && mutated) {
        await this.loadTables();
        if (this.selected) {
          this.invalidateTable(this.selected);
          await Promise.all([this.loadRows(), this.loadSchema(this.selected)]);
        }
      }
    } finally {
      s.durationMs = performance.now() - started;
      s.running = false;
      this.#remember({
        sql: text,
        at: Date.now(),
        ok,
        ms: s.durationMs,
        rowsAffected: lastAffected,
        mutated,
      });
    }
  }

  async createTable(name: string, columns: ColumnDef[], schema = this.defaultSchema): Promise<void> {
    await api.createTable({ schema, name, columns });
    this.toast("success", `Created table ${schema}.${name}`);
    await this.loadTables();
    const created = this.tables.find((t) => t.schema === schema && t.name === name);
    if (created) await this.selectTable(created, "structure");
  }

  async createView(name: string, selectSql: string, schema = this.defaultSchema): Promise<void> {
    await api.createView({ schema, name, select: selectSql });
    this.toast("success", `Created view ${schema}.${name}`);
    await this.loadTables();
    const created = this.tables.find((t) => t.schema === schema && t.name === name);
    if (created) await this.selectTable(created, "structure");
  }

  async dropRelation(table: TableInfo): Promise<void> {
    const type = table.type.toLowerCase().includes("view") ? "view" : "table";
    await api.dropTable(table.schema, table.name, type);
    this.toast("success", `Dropped ${type} ${tableKey(table)}`);
    if (this.selected && tableKey(this.selected) === tableKey(table)) {
      this.selected = null;
      this.grid.reset();
      this.rowChanges.clear();
      saveSelectedTable(this.database, null);
    }
    this.invalidateTable(table);
    await this.loadTables();
  }

  async alterTable(table: TableInfo, ops: AlterOp[]): Promise<void> {
    await api.alterTable(table.schema, table.name, ops);
    this.toast("success", `Altered ${tableKey(table)}`);
    this.invalidateTable(table);
    await Promise.all([this.loadTables(), this.loadSchema(table), this.loadRows()]);
  }

  async createIndex(table: TableInfo, name: string, columns: string[], unique = false): Promise<void> {
    await api.createIndex(table.schema, table.name, { name, columns, unique });
    this.toast("success", `Created index ${name}`);
    this.invalidateTable(table);
    await this.loadSchema(table);
  }

  async dropIndex(table: TableInfo, name: string): Promise<void> {
    await api.dropIndex(table.schema, table.name, name);
    this.toast("success", `Dropped index ${name}`);
    this.invalidateTable(table);
    await this.loadSchema(table);
  }

  async createForeignKey(
    table: TableInfo,
    body: {
      name: string;
      columns: string[];
      refSchema?: string;
      refTable: string;
      refColumns: string[];
      onUpdate?: string;
      onDelete?: string;
    },
  ): Promise<void> {
    await api.createForeignKey(table.schema, table.name, body);
    this.toast("success", `Created foreign key ${body.name}`);
    this.invalidateTable(table);
    await this.loadSchema(table);
  }

  async dropForeignKey(table: TableInfo, name: string): Promise<void> {
    await api.dropForeignKey(table.schema, table.name, name);
    this.toast("success", `Dropped foreign key ${name}`);
    this.invalidateTable(table);
    await this.loadSchema(table);
  }

  async insertRow(table: TableInfo, values: Record<string, unknown>): Promise<void> {
    await api.insertRow(table.schema, table.name, values);
    this.toast("success", "Row inserted");
    await this.loadRows();
  }

  async updateRow(table: TableInfo, key: Record<string, unknown>, values: Record<string, unknown>): Promise<void> {
    await api.updateRow(table.schema, table.name, key, values);
    this.toast("success", "Row updated");
    await this.loadRows();
  }

  async deleteRow(table: TableInfo, key: Record<string, unknown>): Promise<void> {
    await api.deleteRow(table.schema, table.name, key);
    this.toast("success", "Row deleted");
    await this.loadRows();
  }

  #remember(entry: HistoryEntry): void {
    const rest = this.history.filter((h) => h.sql !== entry.sql);
    this.history = [entry, ...rest].slice(0, HISTORY_MAX);
    localStorage.setItem(historyKey(this.database), JSON.stringify(this.history));
  }

  clearHistory(): void {
    this.history = [];
    localStorage.removeItem(historyKey(this.database));
  }

  openSql(text?: string): void {
    if (text !== undefined) this.sql.text = text;
    this.view = "sql";
  }

  openWrite(dialog: WriteDialog): void {
    if (this.readOnly) {
      this.toast("error", "Writes require full access");
      return;
    }
    this.writeDialog = dialog;
  }

  closeWrite(): void {
    this.writeDialog = null;
  }

  setTheme(pref: ThemePreference): void {
    this.theme = pref;
    saveThemePreference(pref);
    applyTheme(pref);
  }

  cycleTheme(): void {
    const order: ThemePreference[] = ["system", "light", "dark"];
    this.setTheme(order[(order.indexOf(this.theme) + 1) % order.length]);
  }

  toast(kind: "error" | "info" | "success", msg: string): void {
    if (kind === "error") notify.error(msg);
    else if (kind === "success") notify.success(msg);
    else notify.message(msg);
  }

  async copy(text: string, label = "Copied to clipboard"): Promise<void> {
    try {
      await navigator.clipboard.writeText(text);
      this.toast("success", label);
    } catch {
      this.toast("error", "Clipboard is not available");
    }
  }
}

export const [getApp, setApp] = createContext<AppState>();
