<script lang="ts">
  import { ChevronRight, Eye, Folder, RefreshCw, Search, Table2, X } from "@lucide/svelte";
  import { SvelteSet } from "svelte/reactivity";
  import { tableKey, type TableInfo } from "$lib/api";
  import { getApp } from "$lib/state.svelte";
  import { cn } from "$lib/utils";
  import Highlight from "./Highlight.svelte";
  import { Kbd } from "$lib/components/ui/kbd";
  import Tip from "./ui/Tip.svelte";

  interface Props {
    searchInput?: HTMLInputElement | null;
  }

  let { searchInput = $bindable(null) }: Props = $props();

  const app = getApp();
  let query = $state("");
  const collapsed = new SvelteSet<string>();

  const groups = $derived.by(() => {
    const q = query.trim().toLowerCase();
    const order: string[] = [];
    const grouped: Record<string, TableInfo[]> = {};
    for (const t of app.tables) {
      if (q && !tableKey(t).toLowerCase().includes(q)) continue;
      const list = grouped[t.schema];
      if (list) list.push(t);
      else {
        grouped[t.schema] = [t];
        order.push(t.schema);
      }
    }
    return order.map((schema) => ({ schema, tables: grouped[schema] }));
  });

  const matchCount = $derived(groups.reduce((n, g) => n + g.tables.length, 0));

  function registerSearch(node: HTMLInputElement) {
    searchInput = node;
    return () => {
      if (searchInput === node) searchInput = null;
    };
  }

  function isView(t: TableInfo): boolean {
    return t.type.toLowerCase().includes("view");
  }

  function toggle(schema: string): void {
    if (collapsed.has(schema)) collapsed.delete(schema);
    else collapsed.add(schema);
  }

  function onSearchKey(e: KeyboardEvent): void {
    if (e.key === "Escape") {
      query = "";
      searchInput?.blur();
    } else if (e.key === "Enter") {
      const first = groups[0]?.tables[0];
      if (first) void app.selectTable(first);
    }
  }
</script>

<aside class="flex h-full min-h-0 flex-col bg-surface">
  <div class="flex h-9 shrink-0 items-center justify-between px-3">
    <span class="text-[11px] font-semibold tracking-wider text-muted-foreground uppercase">
      Explorer
      {#if !app.tablesLoading}
        <span class="ml-1 font-mono font-normal normal-case">{app.tables.length}</span>
      {/if}
    </span>
    <Tip label="Reload tables">
      {#snippet children(props)}
        <button
          {...props}
          type="button"
          aria-label="Reload tables"
          onclick={() => app.loadTables()}
          class="grid size-6 cursor-pointer place-items-center rounded text-muted-foreground hover:bg-surface-2 hover:text-foreground"
        >
          <RefreshCw class={cn("size-3.5", app.tablesLoading && "animate-spin")} />
        </button>
      {/snippet}
    </Tip>
  </div>

  <div class="px-2 pb-2">
    <label
      class="flex h-7 items-center gap-2 rounded-md border border-border bg-background px-2 focus-within:border-primary focus-within:ring-2 focus-within:ring-ring"
    >
      <Search class="size-3.5 shrink-0 text-muted-foreground" />
      <input
        {@attach registerSearch}
        bind:value={query}
        onkeydown={onSearchKey}
        type="text"
        spellcheck="false"
        autocomplete="off"
        placeholder="Filter tables"
        aria-label="Filter tables"
        class="min-w-0 flex-1 bg-transparent text-[12.5px] outline-none placeholder:text-muted-foreground"
      />
      {#if query}
        <button type="button" aria-label="Clear filter" class="cursor-pointer text-muted-foreground hover:text-foreground" onclick={() => (query = "")}>
          <X class="size-3.5" />
        </button>
      {:else}
        <Kbd class="h-4 min-w-4 px-1 text-[10px]">/</Kbd>
      {/if}
    </label>
  </div>

  <nav class="min-h-0 flex-1 overflow-y-auto px-1.5 pb-3" aria-label="Tables">
    {#if app.tablesError}
      <p class="m-2 rounded-md bg-danger-soft p-2 text-[12px] text-danger">{app.tablesError}</p>
    {:else if app.tablesLoading && app.tables.length === 0}
      {#each [0, 1, 2, 3, 4, 5] as i (i)}
        <div class="mx-1.5 my-2 h-3.5 animate-pulse rounded bg-surface-2"></div>
      {/each}
    {:else if matchCount === 0}
      <p class="px-2 py-6 text-center text-[12px] text-muted-foreground">
        {query ? "No tables match your filter." : "This database has no tables."}
      </p>
    {:else}
      {#each groups as group (group.schema)}
        {@const open = query !== "" || !collapsed.has(group.schema)}
        <div class="mb-1">
          <button
            type="button"
            onclick={() => toggle(group.schema)}
            aria-expanded={open}
            class="flex h-6 w-full cursor-pointer items-center gap-1 rounded px-1 text-[11.5px] font-medium text-muted-foreground hover:text-foreground"
          >
            <ChevronRight class={cn("size-3 transition-transform", open && "rotate-90")} />
            <Folder class="size-3.5" />
            <span class="truncate">{group.schema}</span>
            <span class="ml-auto font-mono text-[10.5px]">{group.tables.length}</span>
          </button>
          {#if open}
            <ul class="mt-0.5">
              {#each group.tables as t (tableKey(t))}
                {@const active = app.selected !== null && tableKey(app.selected) === tableKey(t)}
                <li>
                  <button
                    type="button"
                    title="{tableKey(t)} ({t.type})"
                    onclick={() => app.selectTable(t)}
                    aria-current={active ? "true" : undefined}
                    class={cn(
                      "group flex h-6.5 w-full cursor-pointer items-center gap-2 rounded pr-2 pl-5 text-left font-mono text-[12px] transition-colors",
                      active ? "bg-primary/10 text-primary" : "text-foreground/85 hover:bg-surface-2",
                    )}
                  >
                    {#if isView(t)}
                      <Eye class={cn("size-3.5 shrink-0", active ? "text-primary" : "text-muted-foreground")} />
                    {:else}
                      <Table2 class={cn("size-3.5 shrink-0", active ? "text-primary" : "text-muted-foreground")} />
                    {/if}
                    <span class="truncate"><Highlight text={t.name} {query} /></span>
                  </button>
                </li>
              {/each}
            </ul>
          {/if}
        </div>
      {/each}
    {/if}
  </nav>
</aside>
