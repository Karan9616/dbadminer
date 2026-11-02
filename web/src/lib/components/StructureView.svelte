<script lang="ts">
  import { tableKey } from "$lib/api";
  import { Badge } from "$lib/components/ui/badge";
  import { Button } from "$lib/components/ui/button";
  import { getApp } from "$lib/state.svelte";

  const app = getApp();
  const key = $derived(app.selected ? tableKey(app.selected) : "");
  const schema = $derived(key ? app.schemas.get(key) : undefined);
  const detail = $derived(schema ?? (key ? app.details.get(key) : undefined));
  const error = $derived(key ? app.detailErrors.get(key) : undefined);
  const isView = $derived(!!app.selected && app.selected.type.toLowerCase().includes("view"));
  const indexes = $derived(schema?.indexes ?? []);
  const foreignKeys = $derived(schema?.foreignKeys ?? []);
  const ddl = $derived(schema?.ddl ?? "");

  async function dropIndex(name: string): Promise<void> {
    if (!app.selected) return;
    if (!window.confirm(`Drop index ${name}?`)) return;
    try {
      await app.dropIndex(app.selected, name);
    } catch (err) {
      app.toast("error", err instanceof Error ? err.message : String(err));
    }
  }

  async function dropFk(name: string): Promise<void> {
    if (!app.selected) return;
    if (!window.confirm(`Drop foreign key ${name}?`)) return;
    try {
      await app.dropForeignKey(app.selected, name);
    } catch (err) {
      app.toast("error", err instanceof Error ? err.message : String(err));
    }
  }
</script>

<div class="flex min-h-0 flex-1 flex-col">
  {#if !app.selected}
    <div class="grid flex-1 place-items-center text-muted-foreground">Choose a table to see its columns.</div>
  {:else if error && !detail}
    <p class="m-3 rounded-md bg-danger-soft px-3 py-2 text-danger">{error}</p>
  {:else if !detail}
    <div class="grid flex-1 place-items-center text-muted-foreground">Loading structure…</div>
  {:else}
    <div class="flex flex-wrap items-center gap-2 border-b border-border px-3 py-2">
      <h2 class="font-mono text-[13px] font-medium">{tableKey(detail)}</h2>
      {#if detail.primaryKey.length > 0}
        <span class="text-[12px] text-muted-foreground">Primary key {detail.primaryKey.join(", ")}</span>
      {/if}
      {#if !app.readOnly}
        <div class="ml-auto flex flex-wrap items-center gap-1.5">
          {#if !isView}
            <Button
              variant="outline"
              size="sm"
              onclick={() => app.openWrite({ kind: "add-column", table: app.selected! })}
            >
              Add column
            </Button>
            <Button
              variant="outline"
              size="sm"
              onclick={() => app.openWrite({ kind: "create-index", table: app.selected! })}
            >
              Create index
            </Button>
            <Button
              variant="outline"
              size="sm"
              onclick={() => app.openWrite({ kind: "create-fk", table: app.selected! })}
            >
              Add FK
            </Button>
          {/if}
          <Button
            variant="outline"
            size="sm"
            class="text-danger"
            onclick={() => app.openWrite({ kind: "drop", table: app.selected! })}
          >
            Drop {isView ? "view" : "table"}
          </Button>
        </div>
      {/if}
    </div>
    <div class="min-h-0 flex-1 overflow-auto">
      <section class="border-b border-border">
        <h3 class="px-3 py-2 text-[11px] font-semibold tracking-wide text-muted-foreground uppercase">Columns</h3>
        <table class="w-full border-separate border-spacing-0 text-[12.5px]">
          <thead>
            <tr class="text-left text-[11px] tracking-wide text-muted-foreground uppercase">
              <th class="sticky top-0 border-b border-border bg-surface px-3 py-1.5 font-medium">#</th>
              <th class="sticky top-0 border-b border-border bg-surface px-3 py-1.5 font-medium">Column</th>
              <th class="sticky top-0 border-b border-border bg-surface px-3 py-1.5 font-medium">Type</th>
              <th class="sticky top-0 border-b border-border bg-surface px-3 py-1.5 font-medium">Nullable</th>
              <th class="sticky top-0 border-b border-border bg-surface px-3 py-1.5 font-medium">Default</th>
            </tr>
          </thead>
          <tbody>
            {#each detail.columns as column (column.position)}
              <tr class="hover:bg-surface-2/70">
                <td class="border-b border-border px-3 py-1.5 font-mono text-muted-foreground">{column.position}</td>
                <td class="border-b border-border px-3 py-1.5 font-mono">
                  <span class="inline-flex items-center gap-2">
                    {column.name}
                    {#if column.primaryKey}
                      <Badge variant="outline" class="h-4 border-primary/30 px-1 text-[10px] text-primary">PK</Badge>
                    {/if}
                  </span>
                </td>
                <td class="border-b border-border px-3 py-1.5 font-mono text-muted-foreground">{column.type}</td>
                <td class="border-b border-border px-3 py-1.5">{column.nullable ? "yes" : "no"}</td>
                <td class="border-b border-border px-3 py-1.5 font-mono text-muted-foreground">{column.default ?? ""}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </section>

      <section class="border-b border-border">
        <h3 class="px-3 py-2 text-[11px] font-semibold tracking-wide text-muted-foreground uppercase">Indexes</h3>
        {#if indexes.length === 0}
          <p class="px-3 pb-3 text-[12px] text-muted-foreground">No indexes.</p>
        {:else}
          <table class="w-full border-separate border-spacing-0 text-[12.5px]">
            <thead>
              <tr class="text-left text-[11px] tracking-wide text-muted-foreground uppercase">
                <th class="border-b border-border px-3 py-1.5 font-medium">Name</th>
                <th class="border-b border-border px-3 py-1.5 font-medium">Columns</th>
                <th class="border-b border-border px-3 py-1.5 font-medium">Flags</th>
                {#if !app.readOnly}
                  <th class="border-b border-border px-3 py-1.5 font-medium"></th>
                {/if}
              </tr>
            </thead>
            <tbody>
              {#each indexes as idx (idx.name)}
                <tr class="hover:bg-surface-2/70">
                  <td class="border-b border-border px-3 py-1.5 font-mono">{idx.name}</td>
                  <td class="border-b border-border px-3 py-1.5 font-mono text-muted-foreground">
                    {idx.columns.join(", ")}
                  </td>
                  <td class="border-b border-border px-3 py-1.5">
                    <span class="inline-flex gap-1">
                      {#if idx.unique}
                        <Badge variant="outline" class="h-4 px-1 text-[10px]">unique</Badge>
                      {/if}
                      {#if idx.primary}
                        <Badge variant="outline" class="h-4 border-primary/30 px-1 text-[10px] text-primary">PK</Badge>
                      {/if}
                    </span>
                  </td>
                  {#if !app.readOnly}
                    <td class="border-b border-border px-3 py-1.5 text-right">
                      {#if !idx.primary}
                        <Button variant="ghost" size="sm" class="text-danger" onclick={() => dropIndex(idx.name)}>
                          Drop
                        </Button>
                      {/if}
                    </td>
                  {/if}
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
      </section>

      <section class="border-b border-border">
        <h3 class="px-3 py-2 text-[11px] font-semibold tracking-wide text-muted-foreground uppercase">
          Foreign keys
        </h3>
        {#if foreignKeys.length === 0}
          <p class="px-3 pb-3 text-[12px] text-muted-foreground">No foreign keys.</p>
        {:else}
          <table class="w-full border-separate border-spacing-0 text-[12.5px]">
            <thead>
              <tr class="text-left text-[11px] tracking-wide text-muted-foreground uppercase">
                <th class="border-b border-border px-3 py-1.5 font-medium">Name</th>
                <th class="border-b border-border px-3 py-1.5 font-medium">Columns</th>
                <th class="border-b border-border px-3 py-1.5 font-medium">References</th>
                {#if !app.readOnly}
                  <th class="border-b border-border px-3 py-1.5 font-medium"></th>
                {/if}
              </tr>
            </thead>
            <tbody>
              {#each foreignKeys as fk (fk.name)}
                <tr class="hover:bg-surface-2/70">
                  <td class="border-b border-border px-3 py-1.5 font-mono">{fk.name}</td>
                  <td class="border-b border-border px-3 py-1.5 font-mono text-muted-foreground">
                    {fk.columns.join(", ")}
                  </td>
                  <td class="border-b border-border px-3 py-1.5 font-mono text-muted-foreground">
                    {fk.refSchema ? `${fk.refSchema}.` : ""}{fk.refTable}({fk.refColumns.join(", ")})
                  </td>
                  {#if !app.readOnly}
                    <td class="border-b border-border px-3 py-1.5 text-right">
                      <Button variant="ghost" size="sm" class="text-danger" onclick={() => dropFk(fk.name)}>
                        Drop
                      </Button>
                    </td>
                  {/if}
                </tr>
              {/each}
            </tbody>
          </table>
        {/if}
      </section>

      <section>
        <h3 class="px-3 py-2 text-[11px] font-semibold tracking-wide text-muted-foreground uppercase">DDL</h3>
        {#if ddl}
          <pre
            class="overflow-auto px-3 pb-4 font-mono text-[12px] leading-relaxed whitespace-pre-wrap text-foreground/90"
          >{ddl}</pre>
        {:else}
          <p class="px-3 pb-3 text-[12px] text-muted-foreground">No DDL available.</p>
        {/if}
      </section>
    </div>
  {/if}
</div>
