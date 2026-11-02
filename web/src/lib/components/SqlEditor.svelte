<script lang="ts">
  import { ChevronDown, Play, Trash2 } from "@lucide/svelte";
  import { onMount } from "svelte";
  import { Badge } from "$lib/components/ui/badge";
  import { Button } from "$lib/components/ui/button";
  import * as DropdownMenu from "$lib/components/ui/dropdown-menu";
  import { formatDuration } from "$lib/format";
  import { getApp } from "$lib/state.svelte";
  import { SQL_TEMPLATES } from "$lib/sql-templates";
  import { modKey } from "$lib/utils";
  import type { EditorHandle } from "$lib/sql-editor";
  import ExportMenu from "./ui/ExportMenu.svelte";

  function formatCell(value: unknown): string {
    if (value === null || value === undefined) return "NULL";
    if (typeof value === "object") return JSON.stringify(value);
    return String(value);
  }

  const app = getApp();

  let handle = $state<EditorHandle | null>(null);
  let historyQuery = $state("");
  let systemDark = $state(window.matchMedia("(prefers-color-scheme: dark)").matches);
  let selectedResult = $state(0);

  const dark = $derived(app.theme === "dark" || (app.theme === "system" && systemDark));
  const history = $derived(
    app.history.filter((entry) => entry.sql.toLowerCase().includes(historyQuery.trim().toLowerCase())),
  );
  const multi = $derived(app.sql.results.length > 1);
  const resultIndex = $derived(
    Math.min(selectedResult, Math.max(0, app.sql.results.length - 1)),
  );
  const activeResult = $derived.by(() => {
    if (multi) {
      const entry = app.sql.results[resultIndex] ?? app.sql.results[0];
      return entry?.result ?? null;
    }
    return app.sql.result;
  });
  const activeError = $derived.by(() => {
    if (multi) {
      const entry = app.sql.results[resultIndex] ?? app.sql.results[0];
      return entry?.error ?? null;
    }
    return app.sql.error;
  });

  onMount(() => {
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const onChange = () => {
      systemDark = media.matches;
    };
    media.addEventListener("change", onChange);
    return () => media.removeEventListener("change", onChange);
  });

  function mountEditor(node: HTMLElement) {
    // CodeMirror mounts a <style> tag on document, which the page CSP blocks.
    // A shadow root has no head, so style-mod uses adoptedStyleSheets instead.
    const shadow = node.attachShadow({ mode: "open" });
    const inner = document.createElement("div");
    inner.style.cssText = "position:absolute;inset:0;";
    shadow.appendChild(inner);
    let cancelled = false;
    let editor: EditorHandle | undefined;
    void import("$lib/sql-editor").then(({ createSqlEditor }) => {
      if (cancelled) return;
      editor = createSqlEditor(inner, {
        text: app.sql.text,
        dialect: app.info?.dialect ?? "postgres",
        tables: app.tables,
        dark,
        onChange: (text) => {
          app.sql.text = text;
        },
        onRun: (sql) => {
          selectedResult = 0;
          void app.runSql(sql);
        },
      });
      handle = editor;
    });
    return () => {
      cancelled = true;
      editor?.destroy();
      handle = null;
    };
  }

  $effect(() => {
    const text = app.sql.text;
    handle?.setText(text);
  });

  $effect(() => {
    handle?.setContext({
      dialect: app.info?.dialect ?? "postgres",
      tables: app.tables,
      dark,
    });
  });

  function run() {
    selectedResult = 0;
    const sql = handle?.runnable() ?? app.sql.text;
    void app.runSql(sql);
  }

  function recall(sql: string) {
    app.sql.text = sql;
    handle?.focus();
  }

  function insertTemplate(id: string) {
    const tpl = SQL_TEMPLATES.find((t) => t.id === id);
    if (!tpl) return;
    const dialect = app.info?.dialect ?? "postgres";
    const schema = app.defaultSchema || undefined;
    app.openSql(tpl.body(dialect, schema));
    handle?.focus();
  }
</script>

<div class="grid min-h-0 flex-1 grid-cols-1 lg:grid-cols-[minmax(0,1fr)_18rem]">
  <div class="flex min-h-0 min-w-0 flex-col border-b border-border lg:border-r lg:border-b-0">
    <div class="flex items-center gap-2 border-b border-border px-3 py-2">
      <span class="text-[12px] font-medium">SQL</span>
      <span class="hidden text-[11.5px] text-muted-foreground sm:inline"
        >{modKey}+Enter runs the selection, or the whole editor</span
      >
      <div class="ml-auto flex items-center gap-1.5">
        <DropdownMenu.Root>
          <DropdownMenu.Trigger>
            {#snippet child({ props })}
              <Button {...props} variant="outline" size="sm">
                Templates
                <ChevronDown class="text-muted-foreground" />
              </Button>
            {/snippet}
          </DropdownMenu.Trigger>
          <DropdownMenu.Content align="end">
            {#each SQL_TEMPLATES as tpl (tpl.id)}
              <DropdownMenu.Item onSelect={() => insertTemplate(tpl.id)}>
                {tpl.label}
              </DropdownMenu.Item>
            {/each}
          </DropdownMenu.Content>
        </DropdownMenu.Root>
        <Button size="sm" onclick={run} disabled={app.sql.running}>
          <Play />
          {app.sql.running ? "Running" : "Run"}
        </Button>
      </div>
    </div>
    <div class="relative min-h-48 flex-1" {@attach mountEditor}></div>
    <div class="min-h-0 flex-1 overflow-hidden border-t border-border">
      {#if multi}
        <div class="flex gap-1 overflow-x-auto border-b border-border px-2 py-1.5">
          {#each app.sql.results as entry, i (i)}
            <button
              type="button"
              class="h-6 shrink-0 cursor-pointer rounded-md px-2 font-mono text-[11px] {resultIndex === i
                ? 'bg-surface-2 text-foreground'
                : 'text-muted-foreground hover:bg-surface-2/70'}"
              onclick={() => (selectedResult = i)}
            >
              #{i + 1}
              {#if entry.error}
                <span class="text-danger">err</span>
              {:else if typeof entry.result?.rowsAffected === "number"}
                <span class="text-muted-foreground">{entry.result.rowsAffected}</span>
              {:else if entry.result?.columns}
                <span class="text-muted-foreground">{entry.result.rows.length}r</span>
              {/if}
            </button>
          {/each}
        </div>
      {/if}
      {#if activeError}
        <p class="m-3 rounded-md bg-danger-soft px-3 py-2 font-mono text-[12px] text-danger">{activeError}</p>
      {:else if activeResult?.columns}
        <div class="flex h-full min-h-40 flex-col">
          <div class="flex items-center gap-2 px-3 py-1.5 text-[12px] text-muted-foreground">
            <span>{activeResult.rows.length} rows</span>
            {#if app.sql.durationMs !== null}
              <span>{formatDuration(app.sql.durationMs)}</span>
            {/if}
            {#if activeResult.truncated}
              <span class="text-amber-700 dark:text-amber-200">truncated</span>
            {/if}
            <span class="ml-auto">
              <ExportMenu data={activeResult} basename="query" />
            </span>
          </div>
          {@render QueryGrid(activeResult)}
        </div>
      {:else if activeResult && typeof activeResult.rowsAffected === "number"}
        <p class="px-3 py-2 text-[12.5px]">{activeResult.rowsAffected} rows affected</p>
      {:else}
        <p class="px-3 py-6 text-[12.5px] text-muted-foreground">Results appear here.</p>
      {/if}
    </div>
  </div>

  <aside class="flex min-h-40 flex-col">
    <div class="flex items-center gap-2 border-b border-border px-3 py-2">
      <span class="text-[12px] font-medium">History</span>
      <Button variant="ghost" size="icon-sm" class="ml-auto" aria-label="Clear history" onclick={() => app.clearHistory()}>
        <Trash2 />
      </Button>
    </div>
    <div class="px-2 py-2">
      <input
        bind:value={historyQuery}
        type="text"
        spellcheck="false"
        placeholder="Search history"
        aria-label="Search history"
        class="h-7 w-full rounded-md border border-border bg-background px-2 text-[12px] outline-none focus:border-primary"
      />
    </div>
    <ul class="min-h-0 flex-1 overflow-y-auto px-1.5 pb-2">
      {#if history.length === 0}
        <li class="px-2 py-4 text-center text-[12px] text-muted-foreground">No statements yet.</li>
      {/if}
      {#each history as entry (entry.at + entry.sql)}
        <li>
          <button
            type="button"
            class="w-full cursor-pointer rounded-md px-2 py-1.5 text-left hover:bg-surface-2"
            onclick={() => recall(entry.sql)}
          >
            <span class="line-clamp-2 font-mono text-[11.5px] {entry.ok ? '' : 'text-danger'}">{entry.sql}</span>
            <span class="mt-0.5 flex flex-wrap items-center gap-1 text-[10.5px] text-muted-foreground">
              <span>{new Date(entry.at).toLocaleString()} · {formatDuration(entry.ms)}</span>
              {#if entry.mutated}
                <Badge variant="outline" class="h-4 px-1 text-[9px]">mutated</Badge>
              {/if}
              {#if typeof entry.rowsAffected === "number"}
                <Badge variant="secondary" class="h-4 px-1 font-mono text-[9px]">
                  {entry.rowsAffected} aff
                </Badge>
              {/if}
            </span>
          </button>
        </li>
      {/each}
    </ul>
  </aside>
</div>

{#snippet QueryGrid(result: { columns: string[]; rows: unknown[][] })}
  <div class="min-h-0 flex-1 overflow-auto">
    <table class="w-max min-w-full border-separate border-spacing-0 font-mono text-[12px]">
      <thead>
        <tr>
          {#each result.columns as column, index (index)}
            <th class="sticky top-0 border-b border-border bg-surface px-2 py-1.5 text-left font-medium">{column}</th>
          {/each}
        </tr>
      </thead>
      <tbody>
        {#each result.rows as row, index (index)}
          <tr class="hover:bg-surface-2/70">
            {#each row as value, cellIndex (`${index}-${cellIndex}`)}
              <td class="max-w-96 border-b border-border px-2 py-1 align-top break-all">{formatCell(value)}</td>
            {/each}
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
{/snippet}
