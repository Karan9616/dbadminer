<script lang="ts">
  import {
    ArrowDown,
    ArrowUp,
    Braces,
    ChevronsUpDown,
    Copy,
    Pin,
    PinOff,
  } from "@lucide/svelte";
  import { tableKey } from "$lib/api";
  import { Button } from "$lib/components/ui/button";
  import * as Popover from "$lib/components/ui/popover";
  import { cellKind, cellPretty, cellText, formatCount } from "$lib/format";
  import {
    editInputText,
    parseCellInput,
    type DisplayRow,
  } from "$lib/row-changes.svelte";
  import { PAGE_SIZES, getApp } from "$lib/state.svelte";
  import { cn } from "$lib/utils";
  import ExportMenu from "./ui/ExportMenu.svelte";
  import RowChangesSheet from "./RowChangesSheet.svelte";

  const PIN_PK_KEY = "dbadminer:pin-pk";
  const CHECKBOX_W = 32;
  const PINNED_PK_W = 120;

  function loadPinPk(): boolean {
    try {
      const raw = localStorage.getItem(PIN_PK_KEY);
      if (raw === null) return true;
      return raw === "1" || raw === "true";
    } catch {
      return true;
    }
  }

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
  const pk = $derived(detail?.primaryKey ?? []);
  const canWriteRows = $derived(
    !app.readOnly && !!app.selected && !isView && pk.length > 0,
  );

  let pinPk = $state(loadPinPk());
  const pinPkEnabled = $derived(pinPk && pk.length > 0);

  const displayRows = $derived.by((): DisplayRow[] => {
    if (!data) return [];
    // Touch ops so fold recomputes when the queue changes
    void app.rowChanges.ops;
    return app.rowChanges.fold(data.rows, data.columns, pk);
  });

  /** Column order: PK first when pinned, otherwise table order. */
  const visibleColumns = $derived.by(() => {
    const cols = data?.columns ?? [];
    if (!pinPkEnabled) return cols.map((name, index) => ({ name, index, pinned: false, pinIndex: -1 }));
    const pkSet = new Set(pk);
    const pinned = pk.filter((name) => cols.includes(name));
    const rest = cols.filter((name) => !pkSet.has(name));
    return [...pinned, ...rest].map((name) => {
      const index = cols.indexOf(name);
      const pinIndex = pinned.indexOf(name);
      return { name, index, pinned: pinIndex >= 0, pinIndex };
    });
  });

  /** Rows eligible for bulk selection (pending deletes are locked out). */
  const selectableRows = $derived(displayRows.filter((r) => !r.deleted));
  const selectableIds = $derived(selectableRows.map((r) => r.rowId));
  const selectedCount = $derived(
    selectableIds.filter((id) => app.rowChanges.selectedRowIds.has(id)).length,
  );
  const allPageSelected = $derived(
    selectableIds.length > 0 && selectableIds.every((id) => app.rowChanges.selectedRowIds.has(id)),
  );
  const somePageSelected = $derived(selectedCount > 0 && !allPageSelected);

  let editing = $state<{ rowId: string; column: string } | null>(null);
  let editText = $state("");

  let bulkOpen = $state(false);
  let bulkColumn = $state("");
  let bulkValue = $state("");
  let bulkNull = $state(false);

  const colCount = $derived((data?.columns.length ?? 0) + (canWriteRows ? 2 : 0));

  function togglePinPk() {
    pinPk = !pinPk;
    try {
      localStorage.setItem(PIN_PK_KEY, pinPk ? "1" : "0");
    } catch {
      /* ignore */
    }
  }

  function pinnedLeft(pinIndex: number): string {
    const base = canWriteRows ? CHECKBOX_W : 0;
    return `${base + pinIndex * PINNED_PK_W}px`;
  }

  function copyCell(value: unknown, event?: MouseEvent) {
    event?.stopPropagation();
    if (value === undefined) {
      void app.copy("", "Copied empty");
      return;
    }
    void app.copy(cellText(value), "Copied cell");
  }

  function align(value: unknown): string {
    // Keep numbers left-aligned with headers; tabular-nums still lines digits up.
    if (cellKind(value) === "number") return "text-left tabular-nums";
    return "text-left";
  }

  function startEdit(row: DisplayRow, column: string, value: unknown) {
    if (!canWriteRows || row.deleted) return;
    editing = { rowId: row.rowId, column };
    editText = editInputText(value);
  }

  function focusEditInput(node: HTMLInputElement) {
    node.focus();
    node.select();
  }

  function cancelEdit() {
    editing = null;
    editText = "";
  }

  function commitEdit(asNull = false) {
    if (!editing || !data) return;
    const { rowId, column } = editing;
    const from = app.rowChanges.originalValue(rowId, column, data.columns, data.rows, pk);
    let to: unknown;
    if (asNull) {
      to = null;
    } else if (editText === "" && from === null) {
      to = null;
    } else {
      to = parseCellInput(editText, false);
    }
    app.rowChanges.stageEdit(rowId, column, from, to);
    cancelEdit();
  }

  function onEditKeydown(event: KeyboardEvent) {
    if (event.key === "Enter") {
      event.preventDefault();
      commitEdit();
    } else if (event.key === "Escape") {
      event.preventDefault();
      cancelEdit();
    } else if (event.key === "Backspace" && (event.ctrlKey || event.metaKey)) {
      event.preventDefault();
      commitEdit(true);
    }
  }

  function toggleSelectAll(checked: boolean) {
    app.rowChanges.setSelectAll(selectableIds, checked);
  }

  function columnInfos() {
    return (
      detail?.columns ??
      (data?.columns.map((name, position) => ({
        name,
        type: "TEXT",
        nullable: true,
        primaryKey: pk.includes(name),
        position,
      })) ??
        [])
    );
  }

  function stageDeleteRow(row: DisplayRow) {
    if (row.rowId.startsWith("draft:")) {
      app.rowChanges.stageDelete(row.rowId, {}, row.values);
      return;
    }
    if (!row.key) {
      app.toast("error", "Cannot delete without primary key values");
      return;
    }
    app.rowChanges.stageDelete(row.rowId, row.key, row.values);
  }

  function stageCloneRow(row: DisplayRow) {
    if (!data) return;
    app.rowChanges.stageClone(columnInfos(), data.columns, row.values, data.rows);
  }

  function stageBulkDeleteSelected() {
    const rows = selectableRows
      .filter((r) => app.rowChanges.selectedRowIds.has(r.rowId))
      .map((r) => ({
        rowId: r.rowId,
        key: r.key ?? {},
        snapshot: r.values,
      }));
    if (rows.length === 0) return;
    app.rowChanges.stageBulkDelete(rows);
  }

  function resetBulkSet() {
    bulkColumn = data?.columns[0] ?? "";
    bulkValue = "";
    bulkNull = false;
  }

  function applyBulkSet() {
    if (!data || !bulkColumn) return;
    const colIdx = data.columns.indexOf(bulkColumn);
    if (colIdx < 0) return;
    const to = parseCellInput(bulkValue, bulkNull);
    const rows = selectableRows
      .filter((r) => app.rowChanges.selectedRowIds.has(r.rowId))
      .map((r) => ({ rowId: r.rowId, from: r.values[colIdx] }));
    if (rows.length === 0) return;
    app.rowChanges.stageBulkEdit(bulkColumn, to, rows);
    bulkOpen = false;
  }
</script>

{#snippet cell(value: unknown, auto = false)}
  {@const kind = cellKind(value)}
  <span class="group/cell inline-flex max-w-full items-start gap-1">
    <span class="min-w-0 flex-1">
      {#if auto || value === undefined}
        <span class="text-muted-foreground italic">(auto)</span>
      {:else if kind === "null"}
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
    </span>
    {#if !auto && value !== undefined}
      <button
        type="button"
        class="mt-0.5 shrink-0 cursor-pointer rounded p-0.5 text-muted-foreground opacity-0 transition-opacity hover:bg-surface-2 hover:text-foreground group-hover/cell:opacity-100 focus-visible:opacity-100"
        aria-label="Copy cell"
        title="Copy"
        onclick={(event) => copyCell(value, event)}
      >
        <Copy class="size-3.5" />
      </button>
    {/if}
  </span>
{/snippet}

<div class="flex min-h-0 flex-1 flex-col">
  <div class="flex flex-wrap items-center gap-2 border-b border-border px-3 py-2">
    <h2 class="min-w-0 truncate font-mono text-[13px] font-medium">
      {app.selected ? tableKey(app.selected) : "No table selected"}
    </h2>
    <div class="ml-auto flex flex-wrap items-center gap-2">
      {#if app.selected && pk.length > 0}
        <Button
          variant="outline"
          size="sm"
          aria-pressed={pinPk}
          title={pinPk ? "Unpin primary key columns" : "Pin primary key columns"}
          onclick={togglePinPk}
        >
          {#if pinPk}
            <Pin class="size-3.5" />
          {:else}
            <PinOff class="size-3.5" />
          {/if}
          Pin PK
        </Button>
      {/if}
      {#if canWriteRows && app.selected && data}
        <Button
          variant="outline"
          size="sm"
          onclick={() =>
            app.rowChanges.stageInsert(columnInfos(), {
              columnNames: data.columns,
              baseRows: data.rows,
            })}
        >
          Insert
        </Button>
        {#if selectedCount > 0}
          <Button variant="outline" size="sm" class="text-danger" onclick={stageBulkDeleteSelected}>
            Bulk Delete
          </Button>
          <Popover.Root
            bind:open={bulkOpen}
            onOpenChange={(open) => {
              if (open) resetBulkSet();
            }}
          >
            <Popover.Trigger>
              {#snippet child({ props })}
                <Button {...props} variant="outline" size="sm">
                  Bulk Set column…
                </Button>
              {/snippet}
            </Popover.Trigger>
            <Popover.Content class="w-72 space-y-2 p-3" align="end">
              <label class="block space-y-1 text-[12px]">
                <span class="text-muted-foreground">Column</span>
                <select
                  class="h-8 w-full rounded-md border border-border bg-background px-2 font-mono text-[12px]"
                  bind:value={bulkColumn}
                >
                  {#each data.columns as column (column)}
                    <option value={column}>{column}</option>
                  {/each}
                </select>
              </label>
              <label class="block space-y-1 text-[12px]">
                <span class="text-muted-foreground">Value</span>
                <input
                  class="h-8 w-full rounded-md border border-border bg-background px-2 font-mono text-[12px] disabled:opacity-50"
                  bind:value={bulkValue}
                  disabled={bulkNull}
                />
              </label>
              <label class="flex items-center gap-2 text-[12px]">
                <input type="checkbox" bind:checked={bulkNull} />
                <span>NULL</span>
              </label>
              <div class="flex justify-end gap-2 pt-1">
                <Button variant="outline" size="sm" onclick={() => (bulkOpen = false)}>Cancel</Button>
                <Button size="sm" onclick={applyBulkSet}>Set</Button>
              </div>
            </Popover.Content>
          </Popover.Root>
        {/if}
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
            {#if canWriteRows}
              <th
                class="sticky top-0 left-0 z-30 w-8 border-b border-r border-border bg-surface px-1.5 py-1.5 text-center shadow-[2px_0_4px_-2px_rgba(0,0,0,0.12)]"
              >
                <input
                  type="checkbox"
                  class="cursor-pointer disabled:cursor-not-allowed disabled:opacity-40"
                  checked={allPageSelected}
                  bind:indeterminate={
                    () => somePageSelected,
                    (v) => {
                      void v;
                    }
                  }
                  disabled={selectableIds.length === 0}
                  onchange={(event) => toggleSelectAll(event.currentTarget.checked)}
                  aria-label="Select all rows on this page"
                />
              </th>
            {/if}
            {#each visibleColumns as col (col.name)}
              <th
                class={cn(
                  "sticky top-0 border-b border-border bg-surface px-2 py-1 text-left font-medium",
                  col.pinned ? "z-30 border-r shadow-[2px_0_4px_-2px_rgba(0,0,0,0.12)]" : "z-10",
                )}
                style={col.pinned ? `left: ${pinnedLeft(col.pinIndex)}; min-width: ${PINNED_PK_W}px;` : undefined}
              >
                <button
                  type="button"
                  class="inline-flex cursor-pointer items-center gap-1 rounded px-1 py-0.5 hover:bg-surface-2"
                  onclick={() => app.sortBy(col.name)}
                >
                  <span>{col.name}</span>
                  {#if col.pinned}
                    <Pin class="size-3 text-primary" />
                  {/if}
                  {#if app.grid.order === col.name && app.grid.dir === "asc"}
                    <ArrowUp class="size-3 text-primary" />
                  {:else if app.grid.order === col.name && app.grid.dir === "desc"}
                    <ArrowDown class="size-3 text-primary" />
                  {:else}
                    <ChevronsUpDown class="size-3 text-muted-foreground" />
                  {/if}
                </button>
              </th>
            {/each}
            {#if canWriteRows}
              <th
                class="sticky top-0 right-0 z-30 border-b border-l border-border bg-surface px-2 py-1.5 text-right font-sans text-[11px] font-medium text-muted-foreground shadow-[-2px_0_4px_-2px_rgba(0,0,0,0.12)]"
              >
                Actions
              </th>
            {/if}
          </tr>
        </thead>
        <tbody>
          {#if data && displayRows.length === 0}
            <tr>
              <td
                colspan={colCount}
                class="px-3 py-8 text-center font-sans text-muted-foreground"
              >
                No rows on this page.
              </td>
            </tr>
          {/if}
          {#each displayRows as row (row.rowId)}
            {@const isEditingRow = editing?.rowId === row.rowId}
            {@const stickyBg = cn(
              "bg-surface group-hover:bg-surface-2",
              row.source === "insert" && "bg-emerald-500/10 group-hover:bg-emerald-500/15",
            )}
            <tr
              class={cn(
                "group hover:bg-surface-2/70",
                row.source === "insert" && "bg-emerald-500/10",
                row.deleted && "text-muted-foreground line-through opacity-70",
              )}
            >
              {#if canWriteRows}
                <td
                  class={cn(
                    "relative sticky left-0 z-20 border-b border-r border-border px-1.5 py-1 text-center align-middle shadow-[2px_0_4px_-2px_rgba(0,0,0,0.12)]",
                    stickyBg,
                  )}
                >
                  <input
                    type="checkbox"
                    class="cursor-pointer disabled:cursor-not-allowed disabled:opacity-40"
                    checked={!row.deleted && app.rowChanges.selectedRowIds.has(row.rowId)}
                    disabled={row.deleted}
                    title={row.deleted ? "Row staged for delete — remove that change to select again" : undefined}
                    onchange={() => app.rowChanges.toggleSelect(row.rowId)}
                    aria-label={row.deleted ? "Row staged for delete" : "Select row"}
                  />
                  {#if row.insertOpIndex !== null}
                    <span
                      class="absolute top-0 right-0 rounded-bl bg-emerald-600/90 px-1 text-[9px] leading-tight font-sans text-white"
                      title="Insert op"
                    >
                      {row.insertOpIndex}
                    </span>
                  {/if}
                  {#if row.deleteOpIndex !== null}
                    <span
                      class="absolute top-0 right-0 rounded-bl bg-destructive/90 px-1 text-[9px] leading-tight font-sans text-white"
                      title="Delete op"
                    >
                      {row.deleteOpIndex}
                    </span>
                  {/if}
                </td>
              {/if}
              {#each visibleColumns as col (`${row.rowId}-${col.name}`)}
                {@const value = row.values[col.index]}
                {@const opIndex = row.cellOpIndex[col.name]}
                {@const isEditing = isEditingRow && editing?.column === col.name}
                <td
                  class={cn(
                    "relative max-w-96 border-b border-border px-2 py-1 align-top",
                    align(value),
                    opIndex != null && "bg-amber-500/15",
                    !isEditing && !row.deleted && canWriteRows && "cursor-cell",
                    col.pinned && "sticky z-20 border-r shadow-[2px_0_4px_-2px_rgba(0,0,0,0.12)]",
                    col.pinned && stickyBg,
                  )}
                  style={col.pinned ? `left: ${pinnedLeft(col.pinIndex)}; min-width: ${PINNED_PK_W}px;` : undefined}
                  title={isEditing ? undefined : canWriteRows && !row.deleted ? "Double-click to edit" : undefined}
                  ondblclick={() => startEdit(row, col.name, value)}
                >
                  {#if isEditing}
                    <div class="flex min-w-40 items-center gap-1">
                      <input
                        {@attach focusEditInput}
                        class="h-7 min-w-0 flex-1 rounded border border-border bg-background px-1.5 font-mono text-[12px] outline-none focus-visible:border-ring"
                        bind:value={editText}
                        onkeydown={onEditKeydown}
                        onblur={() => commitEdit()}
                        onclick={(e) => e.stopPropagation()}
                      />
                      <button
                        type="button"
                        class="h-7 shrink-0 cursor-pointer rounded border border-border px-1.5 text-[10px] font-sans font-semibold text-muted-foreground hover:bg-surface-2 hover:text-foreground"
                        title="Set NULL (Ctrl/Cmd+Backspace)"
                        onmousedown={(e) => e.preventDefault()}
                        onclick={(e) => {
                          e.stopPropagation();
                          commitEdit(true);
                        }}
                      >
                        N
                      </button>
                    </div>
                  {:else}
                    {@render cell(value, row.source === "insert" && value === undefined)}
                  {/if}
                  {#if opIndex != null && !isEditing}
                    <span
                      class="absolute top-0 right-0 rounded-bl bg-amber-600/90 px-1 text-[9px] leading-tight font-sans text-white"
                      title="Edit op"
                    >
                      {opIndex}
                    </span>
                  {/if}
                </td>
              {/each}
              {#if canWriteRows}
                <td
                  class={cn(
                    "sticky right-0 z-20 border-b border-l border-border px-2 py-1 text-right font-sans whitespace-nowrap shadow-[-2px_0_4px_-2px_rgba(0,0,0,0.12)]",
                    stickyBg,
                  )}
                >
                  {#if !row.deleted}
                    <Button variant="ghost" size="sm" onclick={() => stageCloneRow(row)}>
                      Clone
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      class="text-danger"
                      onclick={() => stageDeleteRow(row)}
                    >
                      Delete
                    </Button>
                  {/if}
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
    {#if canWriteRows}
      <RowChangesSheet />
    {/if}
  {/if}
</div>
