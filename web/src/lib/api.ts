export type Dialect = "postgres" | "mysql" | "mariadb" | "sqlite" | (string & {});

export interface Info {
  name: string;
  version: string;
  dialect: Dialect;
  access: "readonly" | "full" | (string & {});
  readOnly: boolean;
  schema?: string;
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
  return `api/tables/${encodeURIComponent(schema)}/${encodeURIComponent(table)}`;
}

export const api = {
  info: () => request<Info>("api/info"),

  tables: async () => (await request<{ tables: TableInfo[] }>("api/tables")).tables ?? [],

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
    request<ResultSet>("api/query", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ sql }),
    }),
};

export function tableKey(t: { schema: string; name: string }): string {
  return `${t.schema}.${t.name}`;
}
