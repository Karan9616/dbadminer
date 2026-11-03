export type Dialect = "postgres" | "mysql" | "mariadb" | "sqlite" | (string & {});

export interface Info {
  name: string;
  version: string;
  dialect: Dialect;
  access: "readonly" | "full" | (string & {});
  readOnly: boolean;
  schema?: string;
}

export interface DatabaseInfo {
  name: string;
  dialect: Dialect;
  schema?: string;
  access: Info["access"];
  readOnly: boolean;
}

export interface TableInfo {
  schema: string;
  name: string;
  type: string;
}

export interface RoutineInfo {
  schema: string;
  name: string;
  type: "procedure" | "function" | (string & {});
  language?: string;
  returnType?: string;
  identityArgs?: string;
}

export interface RoutineParam {
  name: string;
  mode?: string;
  dataType: string;
  position: number;
}

export interface RoutineDetail extends RoutineInfo {
  parameters: RoutineParam[];
  definition: string;
}

export interface ColumnInfo {
  name: string;
  type: string;
  nullable: boolean;
  primaryKey: boolean;
  position: number;
  default?: string;
}

export interface TableDetail {
  schema: string;
  name: string;
  columns: ColumnInfo[];
  primaryKey: string[];
}

export interface IndexInfo {
  name: string;
  unique: boolean;
  columns: string[];
  primary?: boolean;
}

export interface ForeignKeyInfo {
  name: string;
  columns: string[];
  refSchema?: string;
  refTable: string;
  refColumns: string[];
  onUpdate?: string;
  onDelete?: string;
}

export interface TableSchema extends TableDetail {
  indexes: IndexInfo[];
  foreignKeys: ForeignKeyInfo[];
  ddl?: string;
}

export interface ColumnDef {
  name: string;
  type: string;
  nullable?: boolean;
  primaryKey?: boolean;
  default?: string | null;
}

export interface AlterOp {
  op: "add_column" | "drop_column" | "rename_column";
  name: string;
  newName?: string;
  type?: string;
  nullable?: boolean;
  default?: string | null;
}

export interface MutateResult {
  rowsAffected: number;
  sql?: string;
}

export interface ResultSet {
  columns: string[];
  rows: unknown[][];
  limit?: number;
  offset?: number;
  total?: number;
  truncated?: boolean;
  rowsAffected?: number;
}

export interface RowsQuery {
  limit: number;
  offset: number;
  order?: string;
  dir?: "asc" | "desc";
  count?: boolean;
}

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

let database = "";

export function setDatabase(name: string): void {
  database = name;
}

function root(): string {
  return database ? `api/databases/${encodeURIComponent(database)}` : "api";
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, init);
  const text = await res.text();
  let body: unknown = null;
  if (text) {
    try {
      body = JSON.parse(text);
    } catch {
      body = { error: text };
    }
  }
  if (!res.ok) {
    const msg = (body as { error?: string } | null)?.error ?? res.statusText;
    throw new ApiError(msg, res.status);
  }
  return body as T;
}

function tablePath(schema: string, table: string): string {
  return `${root()}/tables/${encodeURIComponent(schema)}/${encodeURIComponent(table)}`;
}

function json(method: string, body: unknown): RequestInit {
  return {
    method,
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  };
}

export const api = {
  databases: async (): Promise<DatabaseInfo[]> => {
    const res = await fetch("api/databases");
    if (res.status === 404) return [];
    const text = await res.text();
    let body: unknown = null;
    if (text) {
      try {
        body = JSON.parse(text);
      } catch {
        body = { error: text };
      }
    }
    if (!res.ok) {
      const msg = (body as { error?: string } | null)?.error ?? res.statusText;
      throw new ApiError(msg, res.status);
    }
    return (body as { databases?: DatabaseInfo[] } | null)?.databases ?? [];
  },

  info: () => request<Info>(`${root()}/info`),

  tables: async () => (await request<{ tables: TableInfo[] }>(`${root()}/tables`)).tables ?? [],

  routines: async () => (await request<{ routines: RoutineInfo[] }>(`${root()}/routines`)).routines ?? [],

  routine: (schema: string, name: string, type: string, args?: string) => {
    const params = new URLSearchParams({ type });
    if (args) params.set("args", args);
    return request<RoutineDetail>(
      `${root()}/routines/${encodeURIComponent(schema)}/${encodeURIComponent(name)}?${params}`,
    );
  },

  table: (schema: string, table: string) => request<TableDetail>(tablePath(schema, table)),

  schema: (schema: string, table: string) => request<TableSchema>(`${tablePath(schema, table)}/schema`),

  rows(schema: string, table: string, q: RowsQuery) {
    const params = new URLSearchParams({ limit: String(q.limit), offset: String(q.offset) });
    if (q.order) {
      params.set("order", q.order);
      params.set("dir", q.dir ?? "asc");
    }
    if (q.count) params.set("count", "1");
    return request<ResultSet>(`${tablePath(schema, table)}/rows?${params}`);
  },

  query: (sql: string) =>
    request<ResultSet>(`${root()}/query`, json("POST", { sql })),

  createTable: (body: { schema?: string; name: string; columns: ColumnDef[] }) =>
    request<MutateResult>(`${root()}/tables`, json("POST", body)),

  createView: (body: { schema?: string; name: string; select: string }) =>
    request<MutateResult>(`${root()}/views`, json("POST", body)),

  alterTable: (schema: string, table: string, ops: AlterOp[]) =>
    request<MutateResult>(tablePath(schema, table), json("PATCH", { ops })),

  dropTable: (schema: string, table: string, type: "table" | "view" = "table") =>
    request<MutateResult>(`${tablePath(schema, table)}?type=${type}`, { method: "DELETE" }),

  createIndex: (schema: string, table: string, body: { name: string; unique?: boolean; columns: string[] }) =>
    request<MutateResult>(`${tablePath(schema, table)}/indexes`, json("POST", body)),

  dropIndex: (schema: string, table: string, name: string) =>
    request<MutateResult>(`${tablePath(schema, table)}/indexes/${encodeURIComponent(name)}`, { method: "DELETE" }),

  createForeignKey: (
    schema: string,
    table: string,
    body: {
      name: string;
      columns: string[];
      refSchema?: string;
      refTable: string;
      refColumns: string[];
      onUpdate?: string;
      onDelete?: string;
    },
  ) => request<MutateResult>(`${tablePath(schema, table)}/foreign-keys`, json("POST", body)),

  dropForeignKey: (schema: string, table: string, name: string) =>
    request<MutateResult>(`${tablePath(schema, table)}/foreign-keys/${encodeURIComponent(name)}`, {
      method: "DELETE",
    }),

  insertRow: (schema: string, table: string, values: Record<string, unknown>) =>
    request<MutateResult>(`${tablePath(schema, table)}/rows`, json("POST", { values })),

  updateRow: (schema: string, table: string, key: Record<string, unknown>, values: Record<string, unknown>) =>
    request<MutateResult>(`${tablePath(schema, table)}/rows`, json("PATCH", { key, values })),

  deleteRow: (schema: string, table: string, key: Record<string, unknown>) =>
    request<MutateResult>(`${tablePath(schema, table)}/rows`, json("DELETE", { key })),
};

export function tableKey(t: { schema: string; name: string }): string {
  return `${t.schema}.${t.name}`;
}

export function routineKey(r: RoutineInfo): string {
  const args = r.identityArgs ? `(${r.identityArgs})` : "";
  return `${r.schema}.${r.name}${args}:${r.type}`;
}
