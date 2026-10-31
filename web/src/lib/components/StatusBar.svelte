<script lang="ts">
  import { formatCount, formatDuration } from "$lib/format";
  import { getApp } from "$lib/state.svelte";

  const app = getApp();

  const error = $derived(app.view === "sql" ? app.sql.error : app.grid.error);
  const duration = $derived(app.view === "sql" ? app.sql.durationMs : app.grid.durationMs);
  const loading = $derived(app.view === "sql" ? app.sql.running : app.grid.loading);

  const range = $derived.by(() => {
    if (app.view !== "data" || !app.selected) return "";
    const rows = app.grid.data?.rows.length ?? 0;
    if (!rows) return "0 rows";
    const from = app.grid.offset + 1;
    const to = app.grid.offset + rows;
    const total = app.grid.total;
    return total === null ? `${formatCount(from)}–${formatCount(to)}` : `${formatCount(from)}–${formatCount(to)} of ${formatCount(total)}`;
  });
</script>

<footer class="flex h-7 shrink-0 items-center gap-3 border-t border-border bg-surface px-3 font-mono text-[11px] text-muted-foreground">
  {#if app.info}
    <span class="hidden sm:inline">{app.info.dialect}</span>
    <span class="hidden md:inline">{app.readOnly ? "read-only" : "full access"}</span>
  {/if}
  {#if loading}
    <span>Working…</span>
  {:else if range}
    <span>{range}</span>
  {/if}
  {#if duration !== null && !loading}
    <span>{formatDuration(duration)}</span>
  {/if}
  {#if error}
    <span class="min-w-0 truncate text-danger" role="status">{error}</span>
  {/if}
</footer>
