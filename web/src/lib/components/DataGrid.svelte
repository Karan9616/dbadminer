<script lang="ts">
  import {
    ArrowDown,
    ArrowUp,
    Braces,
    Brackets,
    ChevronsUpDown,
    Copy,
    Download,
    Maximize2,
    Pin,
    PinOff,
  } from "@lucide/svelte";
  import { tableKey } from "$lib/api";
  import { Button } from "$lib/components/ui/button";
  import * as Popover from "$lib/components/ui/popover";
  import {
    blobSummary,
    cellKind,
    cellPretty,
    cellSummary,
    cellText,
    formatCount,
  } from "$lib/format";
  import { downloadBase64Blob } from "$lib/export";
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
  /** Mono 12px ≈ 7.25px/char; clamps keep short ids tight and UUIDs from dominating. */
  const PINNED_CHAR_W = 7.25;
  const PINNED_PK_MIN_W = 52;
  const PINNED_PK_MAX_W = 300;
  /** Sort + pin icons + header `px-3` beyond the column name. */
  const PINNED_HEADER_CHROME = 48;
  /** Body cell `px-3` horizontal padding. */
  const PINNED_CELL_PAD = 24;

  function estimatePinnedPkWidth(headerLen: number, maxCellLen: number): number {
    const headerW = headerLen * PINNED_CHAR_W + PINNED_HEADER_CHROME;
    const cellW = maxCellLen * PINNED_CHAR_W + PINNED_CELL_PAD;
    const raw = Math.max(headerW, cellW);
    return Math.round(Math.min(PINNED_PK_MAX_W, Math.max(PINNED_PK_MIN_W, raw)));
  }

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
  /** Column name → SQL type (for blob detection, etc.). */
  const columnTypes = $derived.by(() => {
    const map: Record<string, string> = {};
    for (const c of detail?.columns ?? []) map[c.name] = c.type;
    return map;
  });

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

  /** Content-based widths for each pinned PK column (current page + header). */
  const pinnedPkWidths = $derived.by((): number[] => {
    if (!pinPkEnabled) return [];
    const cols = data?.columns ?? [];
    const pinned = pk.filter((name) => cols.includes(name));
    return pinned.map((name) => {
      const colIndex = cols.indexOf(name);
      let maxCellLen = 0;
      for (const row of displayRows) {
        const len = cellText(row.values[colIndex]).length;
        if (len > maxCellLen) maxCellLen = len;
      }
      return estimatePinnedPkWidth(name.length, maxCellLen);
    });
  });

  /** Sticky `left` offsets: checkbox (optional) + cumulative prior pinned widths. */
  const pinnedPkOffsets = $derived.by((): number[] => {
    const base = canWriteRows ? CHECKBOX_W : 0;
    const offsets: number[] = [];
    let left = base;
    for (const w of pinnedPkWidths) {
      offsets.push(left);
      left += w;
    }
    return offsets;
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
  /** True when editing a JSON object or array (strict JSON.parse on commit). */
  let editStructured = $state(false);
  /** True for textarea editor (text / JSON / blob); numbers & booleans stay single-line. */
  let editMultiline = $state(false);

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

  function pinnedStyle(pinIndex: number): string {
    const w = pinnedPkWidths[pinIndex] ?? PINNED_PK_MIN_W;
    const left = pinnedPkOffsets[pinIndex] ?? (canWriteRows ? CHECKBOX_W : 0);
    return `left: ${left}px; width: ${w}px; min-width: ${w}px;`;
  }

  function copyCell(value: unknown, event?: MouseEvent) {
    event?.stopPropagation();
    if (value === undefined) {
      void app.copy("", "Copied empty");
      return;
    }
    void app.copy(cellText(value), "Copied cell");
  }

  function downloadBlobCell(value: unknown, column: string, event?: MouseEvent) {
    event?.stopPropagation();
    if (typeof value !== "string" || value.length === 0) {
      app.toast("error", "Nothing to download");
      return;
    }
    try {
      downloadBase64Blob(value, `${basename}-${column}.bin`);
      app.toast("success", "Download started");
    } catch {
      app.toast("error", "Could not decode blob");
    }
  }

  function align(value: unknown): string {
    // Keep numbers left-aligned with headers; tabular-nums still lines digits up.
    if (cellKind(value) === "number") return "text-left tabular-nums";
    return "text-left";
  }

  /** Opaque sticky backgrounds so scrolled cells never show through. */
  function stickyCellBg(row: DisplayRow, edited = false): string {
    if (edited) {
      return "bg-[color-mix(in_oklch,var(--color-amber-500)_18%,var(--color-surface))] group-hover:bg-[color-mix(in_oklch,var(--color-amber-500)_22%,var(--color-surface-2))]";
    }
    if (row.source === "insert") {
      return "bg-[color-mix(in_oklch,var(--color-emerald-500)_12%,var(--color-surface))] group-hover:bg-[color-mix(in_oklch,var(--color-emerald-500)_16%,var(--color-surface-2))]";
    }
    return "bg-surface group-hover:bg-surface-2";
  }

  function startEdit(row: DisplayRow, column: string, value: unknown) {
    if (!canWriteRows || row.deleted) return;
    const kind = cellKind(value, columnTypes[column]);
    editing = { rowId: row.rowId, column };
    editStructured = kind === "json" || kind === "array";
    editMultiline = kind !== "number" && kind !== "boolean";
    editText = editInputText(value);
  }

  function focusEditInput(node: HTMLInputElement | HTMLTextAreaElement) {
    node.focus();
    node.select();
  }

  function cancelEdit() {
    editing = null;
    editText = "";
    editStructured = false;
    editMultiline = false;
  }

  function commitEdit(asNull = false) {
    if (!editing || !data) return;
    const { rowId, column } = editing;
    const from = app.rowChanges.originalValue(rowId, column, data.columns, data.rows, pk);
    let to: unknown;
    if (asNull) {
      to = null;
    } else if (editStructured) {
      try {
        to = JSON.parse(editText);
      } catch {
        app.toast("error", "Invalid JSON");
        return;
      }
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
      // Textarea: Enter inserts a newline; Ctrl/Cmd+Enter commits.
      if (editMultiline && !(event.ctrlKey || event.metaKey)) return;
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

{#snippet cell(value: unknown, auto = false, column = "", columnType = "")}
  {@const kind = cellKind(value, columnType)}
  <span class="group/cell inline-flex max-w-full items-start gap-1">
    <span class="min-w-0 flex-1">
      {#if auto || value === undefined}
        <span class="text-muted-foreground italic">(auto)</span>
      {:else if kind === "null"}
        <span class="text-muted-foreground italic">NULL</span>
      {:else if kind === "blob"}
        <span class="inline-flex items-center gap-1">
          <span
            class="rounded border border-border bg-surface-2 px-1.5 py-0.5 font-mono text-[11px] text-foreground"
            title="Binary value (base64 in API)"
          >
            {blobSummary(String(value))}
          </span>
          <button
            type="button"
            class="shrink-0 cursor-pointer rounded p-0.5 text-muted-foreground hover:bg-surface-2 hover:text-foreground"
            aria-label="Download blob"
            title="Download"
            onclick={(event) => downloadBlobCell(value, column || "blob", event)}
          >
            <Download class="size-3.5" />
          </button>
        </span>
      {:else if kind === "json" || kind === "array"}
        <span class="inline-flex max-w-80 items-center gap-1">
          <span
            class="rounded border border-border bg-surface-2 px-1.5 py-0.5 font-mono text-[11px] text-foreground"
            title={cellText(value)}
          >
            {cellSummary(value)}
          </span>
          <Popover.Root>
            <Popover.Trigger>
              {#snippet child({ props })}
                <button
                  {...props}
                  type="button"
                  aria-label={kind === "array" ? "Expand array" : "Expand JSON"}
                  class="shrink-0 cursor-pointer rounded text-muted-foreground hover:text-foreground"
                  onclick={(event) => event.stopPropagation()}
                >
                  {#if kind === "array"}
                    <Brackets class="size-3.5" />
                  {:else}
                    <Braces class="size-3.5" />
                  {/if}
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
        {@const text = cellText(value)}
        {@const long = kind === "text" && text.length > 64}
        <span class="inline-flex max-w-full items-center gap-1">
          <span
            class={cn("min-w-0 truncate", kind === "number" && "font-mono", long && "max-w-72")}
            title={text}
          >
            {text}
          </span>
          {#if long}
            <Popover.Root>
              <Popover.Trigger>
                {#snippet child({ props })}
                  <button
                    {...props}
                    type="button"
                    aria-label="Expand text"
                    class="shrink-0 cursor-pointer rounded text-muted-foreground hover:text-foreground"
                    onclick={(event) => event.stopPropagation()}
                  >
                    <Maximize2 class="size-3.5" />
                  </button>
                {/snippet}
              </Popover.Trigger>
              <Popover.Content class="w-96 max-w-[80vw] p-0" align="start">
                <pre class="max-h-80 overflow-auto p-3 font-mono text-[11.5px] leading-relaxed whitespace-pre-wrap">{text}</pre>
              </Popover.Content>
            </Popover.Root>
          {/if}
        </span>
      {/if}
    </span>
    {#if !auto && value !== undefined && kind !== "blob"}
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
      <table class="w-max border-separate border-spacing-0 font-mono text-[12px]">
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
                  "sticky top-0 border-b border-border bg-surface px-3 py-1 text-left font-medium",
                  col.pinned
                    ? "z-30 border-r whitespace-nowrap shadow-[2px_0_4px_-2px_rgba(0,0,0,0.12)]"
                    : "z-10",
                )}
                style={col.pinned ? pinnedStyle(col.pinIndex) : undefined}
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
            <tr
              class={cn(
                "group hover:bg-surface-2/70",
                row.source === "insert" && "bg-emerald-500/10",
                row.deleted && "text-muted-foreground line-through",
              )}
            >
              {#if canWriteRows}
                <td
                  class={cn(
                    "relative sticky left-0 z-20 border-b border-r border-border px-1.5 py-1 text-center align-middle shadow-[2px_0_4px_-2px_rgba(0,0,0,0.12)]",
                    stickyCellBg(row),
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
                    "relative border-b border-border px-3 py-1 align-top",
                    col.pinned
                      ? "sticky z-20 border-r shadow-[2px_0_4px_-2px_rgba(0,0,0,0.12)]"
                      : undefined,
                    col.pinned && !isEditing && "whitespace-nowrap",
                    !col.pinned && !isEditing && "max-w-96 overflow-hidden",
                    isEditing && "overflow-visible",
                    align(value),
                    !col.pinned && opIndex != null && "bg-amber-500/15",
                    !isEditing && !row.deleted && canWriteRows && "cursor-cell",
                    col.pinned && stickyCellBg(row, opIndex != null),
                  )}
                  style={col.pinned ? pinnedStyle(col.pinIndex) : undefined}
                  title={isEditing ? undefined : canWriteRows && !row.deleted ? "Double-click to edit" : undefined}
                  ondblclick={() => startEdit(row, col.name, value)}
                >
                  {#if isEditing}
                    <div class="flex min-w-40 items-start gap-1">
                      {#if editMultiline}
                        <textarea
                          {@attach focusEditInput}
                          rows={editStructured ? 5 : 4}
                          class="min-h-24 min-w-72 flex-1 resize rounded border border-border bg-background px-1.5 py-1 font-mono text-[12px] leading-relaxed outline-none focus-visible:border-ring"
                          bind:value={editText}
                          onkeydown={onEditKeydown}
                          onblur={() => commitEdit()}
                          onclick={(e) => e.stopPropagation()}
                          title="Ctrl/Cmd+Enter to save"
                        ></textarea>
                      {:else}
                        <input
                          {@attach focusEditInput}
                          class="h-7 min-w-0 flex-1 rounded border border-border bg-background px-1.5 font-mono text-[12px] outline-none focus-visible:border-ring"
                          bind:value={editText}
                          onkeydown={onEditKeydown}
                          onblur={() => commitEdit()}
                          onclick={(e) => e.stopPropagation()}
                        />
                      {/if}
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
                    {@render cell(
                      value,
                      row.source === "insert" && value === undefined,
                      col.name,
                      columnTypes[col.name] ?? "",
                    )}
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
                    stickyCellBg(row),
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
