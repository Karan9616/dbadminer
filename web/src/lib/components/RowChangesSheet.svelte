<script lang="ts">
  import { ChevronDown, ChevronUp, X } from "@lucide/svelte";
  import { opSummary, type PendingOp } from "$lib/row-changes.svelte";
  import { Button } from "$lib/components/ui/button";
  import { cn } from "$lib/utils";
  import { getApp } from "$lib/state.svelte";

  const app = getApp();

  const ops = $derived(app.rowChanges.ops);
  const pending = $derived(ops.length > 0);
  const expanded = $derived(app.rowChanges.sheetExpanded);
  const applying = $derived(app.rowChanges.applying);

  function toggle() {
    app.rowChanges.sheetExpanded = !app.rowChanges.sheetExpanded;
  }

  function verbClass(tone: ReturnType<typeof opSummary>["tone"]): string {
    if (tone === "insert") return "bg-emerald-500/15 text-emerald-700 dark:text-emerald-400";
    if (tone === "delete") return "bg-danger-soft text-danger";
    return "bg-sky-500/15 text-sky-700 dark:text-sky-400";
  }

  function summaryFor(op: PendingOp) {
    return opSummary(op);
  }
</script>

{#if pending}
  <div class="shrink-0 border-t border-border bg-surface">
    <div class="flex items-center gap-2 px-3 py-2">
      <button
        type="button"
        class="inline-flex min-w-0 flex-1 cursor-pointer items-center gap-1.5 text-left text-[12px] font-medium hover:text-foreground"
        onclick={toggle}
      >
        {#if expanded}
          <ChevronDown class="size-3.5 shrink-0 text-muted-foreground" />
        {:else}
          <ChevronUp class="size-3.5 shrink-0 text-muted-foreground" />
        {/if}
        <span class="truncate">
          {ops.length} pending change{ops.length === 1 ? "" : "s"}
        </span>
      </button>
      <Button
        variant="outline"
        size="sm"
        disabled={applying}
        onclick={() => app.rowChanges.discard()}
      >
        Discard
      </Button>
      <Button size="sm" disabled={applying} onclick={() => void app.applyRowChanges()}>
        {applying ? "Applying…" : "Apply"}
      </Button>
    </div>
    {#if expanded}
      <ul class="max-h-52 overflow-auto border-t border-border px-1 py-1 text-[12px]">
        {#each ops as op, i (op.id)}
          {@const s = summaryFor(op)}
          <li class="flex items-start gap-2 rounded px-2 py-1.5 hover:bg-surface-2/70">
            <span class="mt-0.5 w-5 shrink-0 text-right font-mono text-muted-foreground tabular-nums"
              >{i + 1}</span
            >
            <span
              class={cn(
                "mt-0.5 shrink-0 rounded px-1.5 py-0.5 font-mono text-[10px] font-semibold tracking-wide",
                verbClass(s.tone),
              )}
            >
              {s.verb}
            </span>
            <div class="min-w-0 flex-1">
              <div class="truncate font-mono text-foreground" title={s.title}>{s.title}</div>
              {#if s.detail}
                <div class="truncate text-[11px] text-muted-foreground" title={s.detail}>{s.detail}</div>
              {/if}
            </div>
            <button
              type="button"
              class="mt-0.5 shrink-0 cursor-pointer rounded p-0.5 text-muted-foreground hover:bg-surface-2 hover:text-foreground"
              aria-label="Remove change"
              disabled={applying}
              onclick={() => app.rowChanges.removeOp(op.id)}
            >
              <X class="size-3.5" />
            </button>
          </li>
        {/each}
      </ul>
    {/if}
  </div>
{/if}
