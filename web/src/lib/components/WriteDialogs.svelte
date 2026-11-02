<script lang="ts">
  import { untrack } from "svelte";
  import { tableKey, type ColumnDef, type TableInfo } from "$lib/api";
  import { Badge } from "$lib/components/ui/badge";
  import { Button } from "$lib/components/ui/button";
  import * as Dialog from "$lib/components/ui/dialog";
  import { Input } from "$lib/components/ui/input";
  import { Textarea } from "$lib/components/ui/textarea";
  import { getApp, type WriteDialog } from "$lib/state.svelte";

  const COLUMN_TYPES = [
    "INTEGER",
    "BIGINT",
    "REAL",
    "TEXT",
    "VARCHAR(255)",
    "BOOLEAN",
    "TIMESTAMP",
    "BLOB",
  ] as const;

  const FK_ACTIONS = ["", "NO ACTION", "RESTRICT", "CASCADE", "SET NULL", "SET DEFAULT"] as const;

  interface ColumnRow {
    id: number;
    name: string;
    type: string;
    nullable: boolean;
    primaryKey: boolean;
    default: string;
  }

  interface FieldRow {
    name: string;
    type: string;
    text: string;
    isNull: boolean;
  }

  const app = getApp();

  let busy = $state(false);
  let error = $state<string | null>(null);

  let schema = $state("");
  let name = $state("");
  let columns = $state<ColumnRow[]>([]);
  let selectSql = $state("SELECT 1");
  let colName = $state("");
  let colType = $state("TEXT");
  let colNullable = $state(true);
  let colDefault = $state("");
  let indexName = $state("");
  let indexUnique = $state(false);
  let indexColumns = $state<string[]>([]);
  let fkName = $state("");
  let fkColumns = $state("");
  let refSchema = $state("");
  let refTable = $state("");
  let refColumns = $state("");
  let onDelete = $state("");
  let onUpdate = $state("");
  let fields = $state<FieldRow[]>([]);
  let nextColId = 0;

  const dialog = $derived(app.writeDialog);
  const open = $derived(dialog !== null);
  const title = $derived(dialogTitle(dialog));
  const tableColumns = $derived.by(() => {
    if (!dialog || !("table" in dialog)) return [];
    const key = tableKey(dialog.table);
    return app.schemas.get(key)?.columns ?? app.details.get(key)?.columns ?? [];
  });

  function dialogTitle(d: WriteDialog): string {
    if (!d) return "";
    switch (d.kind) {
      case "create-table":
        return "Create table";
      case "create-view":
        return "Create view";
      case "drop":
        return `Drop ${d.table.type.toLowerCase().includes("view") ? "view" : "table"}`;
      case "add-column":
        return `Add column · ${tableKey(d.table)}`;
      case "create-index":
        return `Create index · ${tableKey(d.table)}`;
      case "create-fk":
        return `Add foreign key · ${tableKey(d.table)}`;
      case "insert":
        return `Insert row · ${tableKey(d.table)}`;
      case "edit":
        return `Edit row · ${tableKey(d.table)}`;
    }
  }

  function newColumn(partial?: Partial<ColumnRow>): ColumnRow {
    return {
      id: ++nextColId,
      name: partial?.name ?? "",
      type: partial?.type ?? "TEXT",
      nullable: partial?.nullable ?? true,
      primaryKey: partial?.primaryKey ?? false,
      default: partial?.default ?? "",
    };
  }

  function looksNumeric(type: string): boolean {
    return /int|decimal|numeric|real|double|float|money|serial|number/i.test(type);
  }

  function parseCell(value: unknown, type: string): { text: string; isNull: boolean } {
    if (value === null || value === undefined) return { text: "", isNull: true };
    if (typeof value === "object") return { text: JSON.stringify(value), isNull: false };
    if (typeof value === "boolean" || typeof value === "number" || typeof value === "bigint") {
      return { text: String(value), isNull: false };
    }
    const text = String(value);
    if (looksNumeric(type) && text !== "" && !Number.isNaN(Number(text))) {
      return { text, isNull: false };
    }
    return { text, isNull: false };
  }

  function parseInput(text: string, type: string, isNull: boolean): unknown {
    if (isNull) return null;
    const trimmed = text.trim();
    if (trimmed.startsWith("{") || trimmed.startsWith("[")) {
      try {
        return JSON.parse(trimmed);
      } catch {
        /* keep as string */
      }
    }
    if (looksNumeric(type) && trimmed !== "" && !Number.isNaN(Number(trimmed))) {
      return Number(trimmed);
    }
    if (/^(true|false)$/i.test(trimmed) && /bool|bit/i.test(type)) {
      return trimmed.toLowerCase() === "true";
    }
    // empty string stays empty for text columns
    return text;
  }

  function resetForm(d: NonNullable<WriteDialog>): void {
    busy = false;
    error = null;
    schema = app.defaultSchema;
    name = "";
    selectSql = "SELECT 1";
    colName = "";
    colType = "TEXT";
    colNullable = true;
    colDefault = "";
    indexName = "";
    indexUnique = false;
    indexColumns = [];
    fkName = "";
    fkColumns = "";
    refSchema = app.defaultSchema;
    refTable = "";
    refColumns = "";
    onDelete = "";
    onUpdate = "";
    fields = [];
    columns = [newColumn({ name: "id", type: "INTEGER", nullable: false, primaryKey: true })];

    if (d.kind === "create-view") {
      schema = app.defaultSchema;
    }
    if (d.kind === "add-column" || d.kind === "create-index" || d.kind === "create-fk") {
      schema = d.table.schema;
    }
    if (d.kind === "create-fk") {
      refSchema = d.table.schema || app.defaultSchema;
    }
    if (d.kind === "insert" || d.kind === "edit") {
      const key = tableKey(d.table);
      const detail = app.schemas.get(key) ?? app.details.get(key);
      const cols = detail?.columns ?? [];
      if (d.kind === "edit") {
        fields = cols.map((c) => {
          const idx = d.columns.indexOf(c.name);
          const raw = idx >= 0 ? d.row[idx] : null;
          const parsed = parseCell(raw, c.type);
          return { name: c.name, type: c.type, text: parsed.text, isNull: parsed.isNull };
        });
      } else {
        fields = cols.map((c) => ({
          name: c.name,
          type: c.type,
          text: "",
          isNull: c.nullable || c.primaryKey,
        }));
      }
    }
  }

  $effect(() => {
    const d = app.writeDialog;
    if (!d) return;
    untrack(() => resetForm(d));
  });

  function onOpenChange(next: boolean): void {
    if (!next) app.closeWrite();
  }

  function addColumnRow(): void {
    columns = [...columns, newColumn()];
  }

  function removeColumnRow(id: number): void {
    if (columns.length <= 1) return;
    columns = columns.filter((c) => c.id !== id);
  }

  function toggleIndexColumn(col: string): void {
    if (indexColumns.includes(col)) indexColumns = indexColumns.filter((c) => c !== col);
    else indexColumns = [...indexColumns, col];
  }

  function splitList(value: string): string[] {
    return value
      .split(/[\s,]+/)
      .map((s) => s.trim())
      .filter(Boolean);
  }

  async function submit(): Promise<void> {
    const d = app.writeDialog;
    if (!d || busy) return;
    busy = true;
    error = null;
    try {
      switch (d.kind) {
        case "create-table": {
          if (!name.trim()) throw new Error("Table name is required");
          const defs: ColumnDef[] = columns
            .filter((c) => c.name.trim())
            .map((c) => ({
              name: c.name.trim(),
              type: c.type.trim() || "TEXT",
              nullable: c.nullable,
              primaryKey: c.primaryKey,
              default: c.default.trim() === "" ? null : c.default.trim(),
            }));
          if (defs.length === 0) throw new Error("Add at least one column");
          await app.createTable(name.trim(), defs, schema.trim() || undefined);
          break;
        }
        case "create-view": {
          if (!name.trim()) throw new Error("View name is required");
          if (!selectSql.trim()) throw new Error("SELECT statement is required");
          await app.createView(name.trim(), selectSql.trim(), schema.trim() || undefined);
          break;
        }
        case "drop":
          await app.dropRelation(d.table);
          break;
        case "add-column": {
          if (!colName.trim()) throw new Error("Column name is required");
          await app.alterTable(d.table, [
            {
              op: "add_column",
              name: colName.trim(),
              type: colType.trim() || "TEXT",
              nullable: colNullable,
              default: colDefault.trim() === "" ? null : colDefault.trim(),
            },
          ]);
          break;
        }
        case "create-index": {
          if (!indexName.trim()) throw new Error("Index name is required");
          if (indexColumns.length === 0) throw new Error("Select at least one column");
          await app.createIndex(d.table, indexName.trim(), indexColumns, indexUnique);
          break;
        }
        case "create-fk": {
          if (!fkName.trim()) throw new Error("Constraint name is required");
          const cols = splitList(fkColumns);
          const refs = splitList(refColumns);
          if (cols.length === 0 || refs.length === 0) throw new Error("Columns and ref columns are required");
          if (!refTable.trim()) throw new Error("Referenced table is required");
          await app.createForeignKey(d.table, {
            name: fkName.trim(),
            columns: cols,
            refSchema: refSchema.trim() || undefined,
            refTable: refTable.trim(),
            refColumns: refs,
            onDelete: onDelete || undefined,
            onUpdate: onUpdate || undefined,
          });
          break;
        }
        case "insert": {
          const values: Record<string, unknown> = {};
          for (const f of fields) {
            if (f.isNull) values[f.name] = null;
            else values[f.name] = parseInput(f.text, f.type, false);
          }
          await app.insertRow(d.table, values);
          break;
        }
        case "edit": {
          const key = tableKey(d.table);
          const detail = app.schemas.get(key) ?? app.details.get(key);
          const pk = detail?.primaryKey ?? [];
          if (pk.length === 0) throw new Error("Table has no primary key");
          const keyMap: Record<string, unknown> = {};
          const values: Record<string, unknown> = {};
          for (const f of fields) {
            const parsed = parseInput(f.text, f.type, f.isNull);
            values[f.name] = parsed;
            if (pk.includes(f.name)) keyMap[f.name] = parsed;
          }
          // Prefer original PK values from the row so edits to PK still match
          for (const pkCol of pk) {
            const idx = d.columns.indexOf(pkCol);
            if (idx >= 0) keyMap[pkCol] = d.row[idx];
          }
          await app.updateRow(d.table, keyMap, values);
          break;
        }
      }
      app.closeWrite();
    } catch (err) {
      error = err instanceof Error ? err.message : String(err);
    } finally {
      busy = false;
    }
  }

  function isView(t: TableInfo): boolean {
    return t.type.toLowerCase().includes("view");
  }

  const fieldClass =
    "h-8 w-full rounded-md border border-border bg-background px-2 font-mono text-[12px] outline-none focus:border-primary";
  const labelClass = "mb-1 block text-[11px] font-medium text-muted-foreground";
</script>

<Dialog.Root {open} onOpenChange={onOpenChange}>
  <Dialog.Content class="sm:max-w-lg" showCloseButton={!busy}>
    <Dialog.Header>
      <Dialog.Title class="font-medium">{title}</Dialog.Title>
      {#if dialog && "table" in dialog && dialog.kind !== "drop"}
        <Dialog.Description class="font-mono text-[12px] text-muted-foreground">
          {tableKey(dialog.table)}
        </Dialog.Description>
      {/if}
    </Dialog.Header>

    {#if dialog}
      <form
        class="grid gap-3"
        onsubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        {#if dialog.kind === "create-table"}
          <div class="grid grid-cols-2 gap-2">
            <label>
              <span class={labelClass}>Schema</span>
              <Input class="font-mono text-[12px]" bind:value={schema} spellcheck={false} autocomplete="off" />
            </label>
            <label>
              <span class={labelClass}>Name</span>
              <Input class="font-mono text-[12px]" bind:value={name} spellcheck={false} autocomplete="off" required />
            </label>
          </div>
          <div class="grid gap-2">
            <div class="flex items-center justify-between">
              <span class={labelClass + " mb-0"}>Columns</span>
              <Button type="button" variant="outline" size="sm" onclick={addColumnRow}>Add column</Button>
            </div>
            {#each columns as col (col.id)}
              <div class="grid gap-1.5 rounded-md border border-border p-2">
                <div class="grid grid-cols-[1fr_1fr_auto] gap-1.5">
                  <Input
                    class="font-mono text-[12px]"
                    placeholder="name"
                    bind:value={col.name}
                    spellcheck={false}
                    autocomplete="off"
                  />
                  <select class={fieldClass} bind:value={col.type}>
                    {#each COLUMN_TYPES as t (t)}
                      <option value={t}>{t}</option>
                    {/each}
                  </select>
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    disabled={columns.length <= 1}
                    onclick={() => removeColumnRow(col.id)}
                  >
                    Remove
                  </Button>
                </div>
                <div class="flex flex-wrap items-center gap-3 text-[12px]">
                  <label class="inline-flex items-center gap-1.5">
                    <input type="checkbox" bind:checked={col.nullable} />
                    Nullable
                  </label>
                  <label class="inline-flex items-center gap-1.5">
                    <input type="checkbox" bind:checked={col.primaryKey} />
                    PK
                  </label>
                  <Input
                    class="h-7 max-w-40 font-mono text-[12px]"
                    placeholder="default"
                    bind:value={col.default}
                    spellcheck={false}
                  />
                </div>
              </div>
            {/each}
          </div>
        {:else if dialog.kind === "create-view"}
          <div class="grid grid-cols-2 gap-2">
            <label>
              <span class={labelClass}>Schema</span>
              <Input class="font-mono text-[12px]" bind:value={schema} spellcheck={false} autocomplete="off" />
            </label>
            <label>
              <span class={labelClass}>Name</span>
              <Input class="font-mono text-[12px]" bind:value={name} spellcheck={false} autocomplete="off" required />
            </label>
          </div>
          <label>
            <span class={labelClass}>SELECT</span>
            <Textarea class="min-h-28 font-mono text-[12px]" bind:value={selectSql} spellcheck={false} />
          </label>
        {:else if dialog.kind === "drop"}
          <p class="text-[13px]">
            Drop {isView(dialog.table) ? "view" : "table"}
            <span class="font-mono font-medium">{tableKey(dialog.table)}</span>? This cannot be undone.
          </p>
        {:else if dialog.kind === "add-column"}
          <label>
            <span class={labelClass}>Name</span>
            <Input class="font-mono text-[12px]" bind:value={colName} spellcheck={false} autocomplete="off" required />
          </label>
          <label>
            <span class={labelClass}>Type</span>
            <select class={fieldClass} bind:value={colType}>
              {#each COLUMN_TYPES as t (t)}
                <option value={t}>{t}</option>
              {/each}
            </select>
          </label>
          <label class="inline-flex items-center gap-1.5 text-[12px]">
            <input type="checkbox" bind:checked={colNullable} />
            Nullable
          </label>
          <label>
            <span class={labelClass}>Default</span>
            <Input class="font-mono text-[12px]" bind:value={colDefault} spellcheck={false} />
          </label>
        {:else if dialog.kind === "create-index"}
          <label>
            <span class={labelClass}>Name</span>
            <Input class="font-mono text-[12px]" bind:value={indexName} spellcheck={false} autocomplete="off" required />
          </label>
          <label class="inline-flex items-center gap-1.5 text-[12px]">
            <input type="checkbox" bind:checked={indexUnique} />
            Unique
          </label>
          <div>
            <span class={labelClass}>Columns</span>
            <div class="flex max-h-40 flex-col gap-1 overflow-auto rounded-md border border-border p-2">
              {#each tableColumns as c (c.name)}
                <label class="inline-flex items-center gap-1.5 font-mono text-[12px]">
                  <input
                    type="checkbox"
                    checked={indexColumns.includes(c.name)}
                    onchange={() => toggleIndexColumn(c.name)}
                  />
                  {c.name}
                  <span class="text-muted-foreground">{c.type}</span>
                </label>
              {:else}
                <span class="text-[12px] text-muted-foreground">No columns loaded.</span>
              {/each}
            </div>
          </div>
        {:else if dialog.kind === "create-fk"}
          <label>
            <span class={labelClass}>Name</span>
            <Input class="font-mono text-[12px]" bind:value={fkName} spellcheck={false} autocomplete="off" required />
          </label>
          <label>
            <span class={labelClass}>Columns</span>
            <Input
              class="font-mono text-[12px]"
              placeholder="col_a, col_b"
              bind:value={fkColumns}
              spellcheck={false}
              list="fk-local-cols"
            />
            <datalist id="fk-local-cols">
              {#each tableColumns as c (c.name)}
                <option value={c.name}></option>
              {/each}
            </datalist>
          </label>
          <div class="grid grid-cols-2 gap-2">
            <label>
              <span class={labelClass}>Ref schema</span>
              <Input class="font-mono text-[12px]" bind:value={refSchema} spellcheck={false} />
            </label>
            <label>
              <span class={labelClass}>Ref table</span>
              <Input class="font-mono text-[12px]" bind:value={refTable} spellcheck={false} required />
            </label>
          </div>
          <label>
            <span class={labelClass}>Ref columns</span>
            <Input
              class="font-mono text-[12px]"
              placeholder="id"
              bind:value={refColumns}
              spellcheck={false}
            />
          </label>
          <div class="grid grid-cols-2 gap-2">
            <label>
              <span class={labelClass}>ON DELETE</span>
              <select class={fieldClass} bind:value={onDelete}>
                {#each FK_ACTIONS as a (a || "none")}
                  <option value={a}>{a || "(default)"}</option>
                {/each}
              </select>
            </label>
            <label>
              <span class={labelClass}>ON UPDATE</span>
              <select class={fieldClass} bind:value={onUpdate}>
                {#each FK_ACTIONS as a (a || "none")}
                  <option value={a}>{a || "(default)"}</option>
                {/each}
              </select>
            </label>
          </div>
        {:else if dialog.kind === "insert" || dialog.kind === "edit"}
          <div class="grid max-h-80 gap-2 overflow-auto pr-1">
            {#each fields as field (field.name)}
              <div class="grid gap-1">
                <div class="flex items-center gap-2">
                  <span class="font-mono text-[12px] font-medium">{field.name}</span>
                  <Badge variant="outline" class="h-4 px-1 font-mono text-[10px] text-muted-foreground">
                    {field.type}
                  </Badge>
                  <label class="ml-auto inline-flex items-center gap-1 text-[11px] text-muted-foreground">
                    <input type="checkbox" bind:checked={field.isNull} />
                    NULL
                  </label>
                </div>
                <Input
                  class="font-mono text-[12px]"
                  bind:value={field.text}
                  disabled={field.isNull}
                  spellcheck={false}
                  autocomplete="off"
                />
              </div>
            {:else}
              <p class="text-[12px] text-muted-foreground">No columns loaded for this table.</p>
            {/each}
          </div>
        {/if}

        {#if error}
          <p class="rounded-md bg-danger-soft px-2 py-1.5 text-[12px] text-danger">{error}</p>
        {/if}

        <Dialog.Footer class="gap-2 sm:justify-end">
          <Button type="button" variant="outline" size="sm" disabled={busy} onclick={() => app.closeWrite()}>
            Cancel
          </Button>
          <Button
            type="submit"
            size="sm"
            variant={dialog.kind === "drop" ? "destructive" : "default"}
            disabled={busy}
          >
            {#if busy}
              Working…
            {:else if dialog.kind === "drop"}
              Drop
            {:else if dialog.kind === "edit"}
              Save
            {:else if dialog.kind === "insert"}
              Insert
            {:else if dialog.kind === "add-column"}
              Add
            {:else}
              Create
            {/if}
          </Button>
        </Dialog.Footer>
      </form>
    {/if}
  </Dialog.Content>
</Dialog.Root>
