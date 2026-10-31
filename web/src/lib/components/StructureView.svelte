<script lang="ts">
  import { tableKey } from "$lib/api";
  import { Badge } from "$lib/components/ui/badge";
  import { getApp } from "$lib/state.svelte";

  const app = getApp();
  const key = $derived(app.selected ? tableKey(app.selected) : "");
  const detail = $derived(key ? app.details.get(key) : undefined);
  const error = $derived(key ? app.detailErrors.get(key) : undefined);
</script>

<div class="flex min-h-0 flex-1 flex-col">
  {#if !app.selected}
    <div class="grid flex-1 place-items-center text-muted-foreground">Choose a table to see its columns.</div>
  {:else if error && !detail}
    <p class="m-3 rounded-md bg-danger-soft px-3 py-2 text-danger">{error}</p>
  {:else if !detail}
    <div class="grid flex-1 place-items-center text-muted-foreground">Loading structure…</div>
  {:else}
    <div class="flex items-center gap-2 border-b border-border px-3 py-2">
      <h2 class="font-mono text-[13px] font-medium">{tableKey(detail)}</h2>
      {#if detail.primaryKey.length > 0}
        <span class="text-[12px] text-muted-foreground">Primary key {detail.primaryKey.join(", ")}</span>
      {/if}
    </div>
    <div class="min-h-0 flex-1 overflow-auto">
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
    </div>
  {/if}
</div>
