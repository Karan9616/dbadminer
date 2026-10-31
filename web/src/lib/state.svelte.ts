import { createContext } from "svelte";
import { SvelteMap } from "svelte/reactivity";
import { toast as notify } from "svelte-sonner";
import { api, tableKey, type Info, type ResultSet, type TableDetail, type TableInfo } from "./api";
import { applyTheme, loadThemePreference, saveThemePreference, type ThemePreference } from "./theme";

export type View = "data" | "structure" | "sql";

export interface HistoryEntry {
  sql: string;
  at: number;
  ok: boolean;
  ms: number;
}

const HISTORY_KEY = "dbadminer:history";
const PAGE_SIZE_KEY = "dbadminer:page-size";
const HISTORY_MAX = 50;
export const PAGE_SIZES = [25, 50, 100, 200] as const;

function loadHistory(): HistoryEntry[] {
  try {
    const raw = JSON.parse(localStorage.getItem(HISTORY_KEY) ?? "[]");
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
  running = $state(false);
  error = $state<string | null>(null);
  durationMs = $state<number | null>(null);
  lastSql = $state("");
}

export class AppState {
  info = $state.raw<Info | null>(null);
  tables = $state.raw<TableInfo[]>([]);
  tablesLoading = $state(true);
  tablesError = $state<string | null>(null);
  selected = $state.raw<TableInfo | null>(null);
  view = $state<View>("data");
  details = new SvelteMap<string, TableDetail>();
  detailErrors = new SvelteMap<string, string>();
  grid = new GridState();
  sql = new SqlState();
  history = $state.raw<HistoryEntry[]>(loadHistory());
  theme = $state<ThemePreference>(loadThemePreference());
  paletteOpen = $state(false);

  #rowsSeq = 0;

  readOnly = $derived(this.info?.access !== "full");

  async init(): Promise<void> {
    try {
      this.info = await api.info();
    } catch (err) {
      this.toast("error", `Could not load server info: ${message(err)}`);
    }
    await this.loadTables();
  }

  async loadTables(): Promise<void> {
    this.tablesLoading = true;
    this.tablesError = null;
    try {
      this.tables = await api.tables();
    } catch (err) {
      this.tablesError = message(err);
      this.toast("error", this.tablesError);
    } finally {
      this.tablesLoading = false;
    }
  }

  async selectTable(table: TableInfo, view: View = this.view === "sql" ? "data" : this.view): Promise<void> {
    this.selected = table;
    this.view = view;
    this.grid.reset();
    await Promise.all([this.loadRows(), this.loadDetail(table)]);
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
    } catch (err) {
      if (seq !== this.#rowsSeq) return;
      g.error = message(err);
      this.toast("error", g.error);
    } finally {
      if (seq === this.#rowsSeq) g.loading = false;
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

  sortBy(column: string): void {
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
    void this.loadRows();
  }

  setPage(offset: number): void {
    this.grid.offset = Math.max(0, offset);
    void this.loadRows();
  }

  setPageSize(limit: number): void {
    this.grid.limit = limit;
    this.grid.offset = 0;
    localStorage.setItem(PAGE_SIZE_KEY, String(limit));
    void this.loadRows();
  }

  toggleCount(): void {
    this.grid.wantCount = !this.grid.wantCount;
    if (!this.grid.wantCount) this.grid.total = null;
    void this.loadRows();
  }

  async runSql(sql: string = this.sql.text): Promise<void> {
    const text = sql.trim();
    if (!text || this.sql.running) return;
    const s = this.sql;
    s.running = true;
    s.error = null;
    s.lastSql = text;
    const started = performance.now();
    let ok = false;
    try {
      s.result = await api.query(text);
      ok = true;
    } catch (err) {
      s.error = message(err);
      s.result = null;
      this.toast("error", s.error);
    } finally {
      s.durationMs = performance.now() - started;
      s.running = false;
      this.#remember({ sql: text, at: Date.now(), ok, ms: s.durationMs });
    }
  }

  #remember(entry: HistoryEntry): void {
    const rest = this.history.filter((h) => h.sql !== entry.sql);
    this.history = [entry, ...rest].slice(0, HISTORY_MAX);
    localStorage.setItem(HISTORY_KEY, JSON.stringify(this.history));
  }

  clearHistory(): void {
    this.history = [];
    localStorage.removeItem(HISTORY_KEY);
  }

  openSql(text?: string): void {
    if (text !== undefined) this.sql.text = text;
    this.view = "sql";
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
