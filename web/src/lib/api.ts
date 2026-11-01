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

  table: (schema: string, table: string) => request<TableDetail>(tablePath(schema, table)),

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
    request<ResultSet>(`${root()}/query`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ sql }),
    }),
};

export function tableKey(t: { schema: string; name: string }): string {
  return `${t.schema}.${t.name}`;
}
