<script lang="ts">
  import { routineKey } from "$lib/api";
  import { Badge } from "$lib/components/ui/badge";
  import { Button } from "$lib/components/ui/button";
  import { getApp } from "$lib/state.svelte";

  const app = getApp();
  const routine = $derived(app.selectedRoutine);
  const key = $derived(routine ? routineKey(routine) : "");
  const detail = $derived(key ? app.routineDetails.get(key) : undefined);
  const error = $derived(key ? app.routineErrors.get(key) : undefined);
</script>

<div class="flex min-h-0 flex-1 flex-col">
  {#if !routine}
    <div class="grid flex-1 place-items-center text-muted-foreground">
      Choose a procedure or function.
    </div>
  {:else if error && !detail}
    <p class="m-3 rounded-md bg-danger-soft px-3 py-2 text-danger">{error}</p>
  {:else if !detail}
    <div class="grid flex-1 place-items-center text-muted-foreground">Loading routine…</div>
  {:else}
    <div class="flex flex-wrap items-center gap-2 border-b border-border px-3 py-2">
      <h2 class="font-mono text-[13px] font-medium">
        {detail.schema}.{detail.name}{#if detail.identityArgs}({detail.identityArgs}){/if}
      </h2>
      <Badge variant="outline" class="h-5 capitalize">{detail.type}</Badge>
      {#if detail.language}
        <span class="text-[12px] text-muted-foreground">{detail.language}</span>
      {/if}
      {#if detail.returnType}
        <span class="font-mono text-[12px] text-muted-foreground">→ {detail.returnType}</span>
      {/if}
      <div class="ml-auto flex flex-wrap items-center gap-1.5">
        <Button variant="outline" size="sm" onclick={() => app.openRoutineInSql(detail)}>
          Open in SQL
        </Button>
        <Button
          variant="outline"
          size="sm"
          onclick={() => void app.loadRoutine(detail)}
        >
          Reload
        </Button>
      </div>
    </div>
    <div class="min-h-0 flex-1 overflow-auto">
      {#if detail.parameters.length > 0}
        <section class="border-b border-border">
          <h3 class="px-3 py-2 text-[11px] font-semibold tracking-wide text-muted-foreground uppercase">
            Parameters
          </h3>
          <table class="w-full border-separate border-spacing-0 text-[12.5px]">
            <thead>
              <tr class="text-left text-[11px] tracking-wide text-muted-foreground uppercase">
                <th class="sticky top-0 border-b border-border bg-surface px-3 py-1.5 font-medium">#</th>
                <th class="sticky top-0 border-b border-border bg-surface px-3 py-1.5 font-medium">Name</th>
                <th class="sticky top-0 border-b border-border bg-surface px-3 py-1.5 font-medium">Mode</th>
                <th class="sticky top-0 border-b border-border bg-surface px-3 py-1.5 font-medium">Type</th>
              </tr>
            </thead>
            <tbody>
              {#each detail.parameters as param (param.position + param.name)}
                <tr class="hover:bg-surface-2/70">
                  <td class="border-b border-border px-3 py-1.5 font-mono text-muted-foreground"
                    >{param.position}</td
                  >
                  <td class="border-b border-border px-3 py-1.5 font-mono">{param.name || "—"}</td>
                  <td class="border-b border-border px-3 py-1.5 font-mono text-muted-foreground"
                    >{param.mode || "—"}</td
                  >
                  <td class="border-b border-border px-3 py-1.5 font-mono text-muted-foreground"
                    >{param.dataType}</td
                  >
                </tr>
              {/each}
            </tbody>
          </table>
        </section>
      {/if}
      <section>
        <h3 class="px-3 py-2 text-[11px] font-semibold tracking-wide text-muted-foreground uppercase">
          Definition
        </h3>
        <pre
          class="overflow-x-auto border-t border-border bg-background px-3 py-3 font-mono text-[12px] leading-relaxed whitespace-pre-wrap"
        >{detail.definition || "(empty)"}</pre>
      </section>
    </div>
  {/if}
</div>
