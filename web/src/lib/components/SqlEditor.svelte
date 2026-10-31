<script lang="ts">
  import { Play, Trash2 } from "@lucide/svelte";
  import { onMount } from "svelte";
  import { Button } from "$lib/components/ui/button";
  import { formatDuration } from "$lib/format";
  import { getApp } from "$lib/state.svelte";
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

  const dark = $derived(app.theme === "dark" || (app.theme === "system" && systemDark));
  const history = $derived(
    app.history.filter((entry) => entry.sql.toLowerCase().includes(historyQuery.trim().toLowerCase())),
  );

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
    const sql = handle?.runnable() ?? app.sql.text;
    void app.runSql(sql);
  }

  function recall(sql: string) {
    app.sql.text = sql;
    handle?.focus();
  }
</script>

<div class="grid min-h-0 flex-1 grid-cols-1 lg:grid-cols-[minmax(0,1fr)_18rem]">
  <div class="flex min-h-0 min-w-0 flex-col border-b border-border lg:border-r lg:border-b-0">
    <div class="flex items-center gap-2 border-b border-border px-3 py-2">
      <span class="text-[12px] font-medium">SQL</span>
      <span class="hidden text-[11.5px] text-muted-foreground sm:inline">{modKey}+Enter runs the selection, or the whole editor</span>
      <div class="ml-auto">
        <Button size="sm" onclick={run} disabled={app.sql.running}>
          <Play />
          {app.sql.running ? "Running" : "Run"}
        </Button>
      </div>
    </div>
    <div class="relative min-h-48 flex-1" {@attach mountEditor}></div>
    <div class="min-h-0 flex-1 overflow-hidden border-t border-border">
      {#if app.sql.error}
        <p class="m-3 rounded-md bg-danger-soft px-3 py-2 font-mono text-[12px] text-danger">{app.sql.error}</p>
      {:else if app.sql.result?.columns}
        <div class="flex h-full min-h-40 flex-col">
          <div class="flex items-center gap-2 px-3 py-1.5 text-[12px] text-muted-foreground">
            <span>{app.sql.result.rows.length} rows</span>
            {#if app.sql.durationMs !== null}
              <span>{formatDuration(app.sql.durationMs)}</span>
            {/if}
            {#if app.sql.result.truncated}
              <span class="text-amber-700 dark:text-amber-200">truncated</span>
            {/if}
            <span class="ml-auto">
              <ExportMenu data={app.sql.result} basename="query" />
            </span>
          </div>
          <QueryGrid />
        </div>
      {:else if app.sql.result && typeof app.sql.result.rowsAffected === "number"}
        <p class="px-3 py-2 text-[12.5px]">{app.sql.result.rowsAffected} rows affected</p>
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
            <span class="mt-0.5 block text-[10.5px] text-muted-foreground">
              {new Date(entry.at).toLocaleString()} · {formatDuration(entry.ms)}
            </span>
          </button>
        </li>
      {/each}
    </ul>
  </aside>
</div>

{#snippet QueryGrid()}
  <div class="min-h-0 flex-1 overflow-auto">
    <table class="w-max min-w-full border-separate border-spacing-0 font-mono text-[12px]">
      <thead>
        <tr>
          {#each app.sql.result?.columns ?? [] as column, index (index)}
            <th class="sticky top-0 border-b border-border bg-surface px-2 py-1.5 text-left font-medium">{column}</th>
          {/each}
        </tr>
      </thead>
      <tbody>
        {#each app.sql.result?.rows ?? [] as row, index (index)}
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
