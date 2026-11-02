<script lang="ts">
  import { ArrowDown, ArrowUp, ChevronsUpDown, Braces } from "@lucide/svelte";
  import { tableKey } from "$lib/api";
  import { Button } from "$lib/components/ui/button";
  import * as Popover from "$lib/components/ui/popover";
  import { cellKind, cellPretty, cellText, formatCount } from "$lib/format";
  import { PAGE_SIZES, getApp } from "$lib/state.svelte";
  import { cn } from "$lib/utils";
  import ExportMenu from "./ui/ExportMenu.svelte";

  const app = getApp();

  const data = $derived(app.grid.data);
  const from = $derived(data && data.rows.length > 0 ? app.grid.offset + 1 : app.grid.offset);
  const to = $derived(app.grid.offset + (data?.rows.length ?? 0));
  const capped = $derived(data?.limit !== undefined && data.limit < app.grid.limit);
  const basename = $derived(app.selected ? tableKey(app.selected) : "rows");
  const isView = $derived(!!app.selected && app.selected.type.toLowerCase().includes("view"));
  const detail = $derived.by(() => {
    if (!app.selected) return undefined;
    const key = tableKey(app.selected);
    return app.schemas.get(key) ?? app.details.get(key);
  });
  const canWriteRows = $derived(
    !app.readOnly && !!app.selected && !isView && (detail?.primaryKey.length ?? 0) > 0,
  );

  function copyCell(value: unknown) {
    if (window.getSelection()?.toString()) return;
    void app.copy(cellText(value), "Copied cell");
  }

  function align(value: unknown): string {
    const kind = cellKind(value);
    if (kind === "number") return "text-right tabular-nums";
    return "text-left";
  }

  function rowKey(row: unknown[], columns: string[]): Record<string, unknown> | null {
    const pk = detail?.primaryKey ?? [];
    if (pk.length === 0) return null;
    const key: Record<string, unknown> = {};
    for (const col of pk) {
      const idx = columns.indexOf(col);
      if (idx < 0) return null;
      key[col] = row[idx];
    }
    return key;
  }

  async function deleteRow(row: unknown[], columns: string[]): Promise<void> {
    if (!app.selected) return;
    const key = rowKey(row, columns);
    if (!key) {
      app.toast("error", "Cannot delete without primary key values");
      return;
    }
    if (!window.confirm("Delete this row?")) return;
    try {
      await app.deleteRow(app.selected, key);
    } catch (err) {
      app.toast("error", err instanceof Error ? err.message : String(err));
    }
  }
</script>

{#snippet cell(value: unknown)}
  {@const kind = cellKind(value)}
  {#if kind === "null"}
    <span class="text-muted-foreground italic">NULL</span>
  {:else if kind === "json"}
    <span class="inline-flex max-w-80 items-start gap-1">
      <span class="truncate font-mono text-[12px]">{cellText(value)}</span>
      <Popover.Root>
        <Popover.Trigger>
          {#snippet child({ props })}
            <button
              {...props}
              type="button"
              aria-label="Expand value"
              class="mt-0.5 shrink-0 cursor-pointer rounded text-muted-foreground hover:text-foreground"
              onclick={(event) => event.stopPropagation()}
            >
              <Braces class="size-3.5" />
            </button>
          {/snippet}
        </Popover.Trigger>
        <Popover.Content class="w-96 max-w-[80vw] p-0" align="start">
          <pre class="max-h-80 overflow-auto p-3 font-mono text-[11.5px] leading-relaxed whitespace-pre-wrap">{cellPretty(value)}</pre>
        </Popover.Content>
      </Popover.Root>
    </span>
  {:else if kind === "boolean"}
    <span class="font-mono text-primary">{cellText(value)}</span>
  {:else}
    <span class={cn("break-all", kind === "number" && "font-mono")}>{cellText(value)}</span>
  {/if}
{/snippet}

<div class="flex min-h-0 flex-1 flex-col">
  <div class="flex flex-wrap items-center gap-2 border-b border-border px-3 py-2">
    <h2 class="min-w-0 truncate font-mono text-[13px] font-medium">
      {app.selected ? tableKey(app.selected) : "No table selected"}
    </h2>
    <div class="ml-auto flex items-center gap-2">
      {#if canWriteRows && app.selected}
        <Button
          variant="outline"
          size="sm"
          onclick={() => app.openWrite({ kind: "insert", table: app.selected! })}
        >
          Insert
        </Button>
      {/if}
      <label class="flex items-center gap-1.5 text-[12px] text-muted-foreground">
        Rows
        <select
          class="h-7 rounded-md border border-border bg-background px-1.5 font-mono text-[12px]"
          value={app.grid.limit}
          onchange={(event) => app.setPageSize(Number(event.currentTarget.value))}
        >
          {#each PAGE_SIZES as size (size)}
            <option value={size}>{size}</option>
          {/each}
        </select>
      </label>
      <Button variant="outline" size="sm" aria-pressed={app.grid.wantCount} onclick={() => app.toggleCount()}>
        {#if app.grid.total !== null}
          {formatCount(app.grid.total)} rows
        {:else}
          Count
        {/if}
      </Button>
      <ExportMenu data={data} {basename} />
    </div>
  </div>

  {#if !app.selected}
    <div class="grid flex-1 place-items-center px-6 text-center text-muted-foreground">
      <p>Choose a table to browse its rows.</p>
    </div>
  {:else if app.grid.error && !data}
    <p class="m-3 rounded-md bg-danger-soft px-3 py-2 text-danger">{app.grid.error}</p>
  {:else}
    {#if data?.truncated}
      <p class="border-b border-amber-500/30 bg-amber-500/10 px-3 py-1.5 text-[12px] text-amber-800 dark:text-amber-200">
        This page was truncated because the response reached the size limit.
      </p>
    {/if}
    {#if capped}
      <p class="border-b border-border px-3 py-1.5 text-[12px] text-muted-foreground">
        The server returned {data?.limit} rows. The page size is capped by the server.
      </p>
    {/if}
    <div class={cn("min-h-0 flex-1 overflow-auto", app.grid.loading && "opacity-60")}>
      <table class="w-max min-w-full border-separate border-spacing-0 font-mono text-[12px]">
        <thead>
          <tr>
            <th class="sticky top-0 z-10 w-12 border-b border-border bg-surface px-2 py-1.5 text-right font-sans text-[11px] font-medium text-muted-foreground">#</th>
            {#each data?.columns ?? [] as column (column)}
              <th class="sticky top-0 z-10 border-b border-border bg-surface px-2 py-1 text-left font-medium">
                <button
                  type="button"
                  class="inline-flex cursor-pointer items-center gap-1 rounded px-1 py-0.5 hover:bg-surface-2"
                  onclick={() => app.sortBy(column)}
                >
                  <span>{column}</span>
                  {#if app.grid.order === column && app.grid.dir === "asc"}
                    <ArrowUp class="size-3 text-primary" />
                  {:else if app.grid.order === column && app.grid.dir === "desc"}
                    <ArrowDown class="size-3 text-primary" />
                  {:else}
                    <ChevronsUpDown class="size-3 text-muted-foreground" />
                  {/if}
                </button>
              </th>
            {/each}
            {#if canWriteRows}
              <th class="sticky top-0 z-10 border-b border-border bg-surface px-2 py-1.5 text-right font-sans text-[11px] font-medium text-muted-foreground">
                Actions
              </th>
            {/if}
          </tr>
        </thead>
        <tbody>
          {#if data && data.rows.length === 0}
            <tr>
              <td
                colspan={(data.columns.length || 0) + 1 + (canWriteRows ? 1 : 0)}
                class="px-3 py-8 text-center font-sans text-muted-foreground"
              >
                No rows on this page.
              </td>
            </tr>
          {/if}
          {#each data?.rows ?? [] as row, index (index)}
            <tr class="hover:bg-surface-2/70">
              <td class="border-b border-border px-2 py-1 text-right text-muted-foreground">{from + index}</td>
              {#each row as value, cellIndex (`${index}-${cellIndex}`)}
                <td
                  class={cn("max-w-96 cursor-copy border-b border-border px-2 py-1 align-top", align(value))}
                  title="Click to copy"
                  onclick={() => copyCell(value)}
                >
                  {@render cell(value)}
                </td>
              {/each}
              {#if canWriteRows && app.selected && data}
                <td class="border-b border-border px-2 py-1 text-right font-sans whitespace-nowrap">
                  <Button
                    variant="ghost"
                    size="sm"
                    onclick={() =>
                      app.openWrite({
                        kind: "edit",
                        table: app.selected!,
                        row,
                        columns: data.columns,
                      })}
                  >
                    Edit
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    class="text-danger"
                    onclick={() => deleteRow(row, data.columns)}
                  >
                    Delete
                  </Button>
                </td>
              {/if}
            </tr>
          {/each}
        </tbody>
      </table>
    </div>
    <div class="flex items-center gap-2 border-t border-border px-3 py-2">
      <Button variant="outline" size="sm" disabled={app.grid.offset === 0 || app.grid.loading} onclick={() => app.setPage(app.grid.offset - app.grid.limit)}>
        Previous
      </Button>
      <span class="font-mono text-[12px] text-muted-foreground">
        {data && data.rows.length ? `${formatCount(from)}–${formatCount(to)}` : "0"}
        {#if app.grid.total !== null}
          of {formatCount(app.grid.total)}
        {/if}
      </span>
      <Button
        variant="outline"
        size="sm"
        disabled={!data || data.rows.length < (data.limit || app.grid.limit) || app.grid.loading}
        onclick={() => app.setPage(app.grid.offset + app.grid.limit)}
      >
        Next
      </Button>
    </div>
  {/if}
</div>
