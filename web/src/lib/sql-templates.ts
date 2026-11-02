import type { Dialect } from "./api";

export interface SqlTemplate {
  id: string;
  label: string;
  body: (dialect: Dialect, schema?: string) => string;
}

function q(dialect: Dialect, ident: string): string {
  if (dialect === "mysql" || dialect === "mariadb") {
    return "`" + ident.replaceAll("`", "``") + "`";
  }
  return `"${ident.replaceAll(`"`, `""`)}"`;
}

function qualify(dialect: Dialect, schema: string | undefined, table: string): string {
  if (!schema) return q(dialect, table);
  if (dialect === "sqlite" && (schema === "main" || schema === "temp")) return q(dialect, table);
  return `${q(dialect, schema)}.${q(dialect, table)}`;
}

export const SQL_TEMPLATES: SqlTemplate[] = [
  {
    id: "create-table",
    label: "Create table",
    body: (d, schema) =>
      `CREATE TABLE ${qualify(d, schema, "new_table")} (\n  ${q(d, "id")} INTEGER PRIMARY KEY,\n  ${q(d, "name")} TEXT NOT NULL\n);`,
  },
  {
    id: "create-view",
    label: "Create view",
    body: (d, schema) =>
      `CREATE VIEW ${qualify(d, schema, "new_view")} AS\nSELECT * FROM ${qualify(d, schema, "some_table")};`,
  },
  {
    id: "create-index",
    label: "Create index",
    body: (d, schema) =>
      `CREATE INDEX ${q(d, "idx_name")} ON ${qualify(d, schema, "some_table")} (${q(d, "column")});`,
  },
  {
    id: "insert",
    label: "Insert",
    body: (d, schema) =>
      `INSERT INTO ${qualify(d, schema, "some_table")} (${q(d, "id")}, ${q(d, "name")})\nVALUES (1, 'example');`,
  },
  {
    id: "update",
    label: "Update",
    body: (d, schema) =>
      `UPDATE ${qualify(d, schema, "some_table")}\nSET ${q(d, "name")} = 'example'\nWHERE ${q(d, "id")} = 1;`,
  },
  {
    id: "delete",
    label: "Delete",
    body: (d, schema) => `DELETE FROM ${qualify(d, schema, "some_table")}\nWHERE ${q(d, "id")} = 1;`,
  },
];
