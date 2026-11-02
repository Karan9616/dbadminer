<script lang="ts">
  import { onMount } from "svelte";
  import { Toaster } from "$lib/components/ui/sonner";
  import * as Tabs from "$lib/components/ui/tabs";
  import * as Tooltip from "$lib/components/ui/tooltip";
  import { setApp, AppState } from "$lib/state.svelte";
  import { applyTheme, onSystemThemeChange, resolveTheme } from "$lib/theme";
  import CommandPalette from "$lib/components/CommandPalette.svelte";
  import DataGrid from "$lib/components/DataGrid.svelte";
  import Sidebar from "$lib/components/Sidebar.svelte";
  import SqlEditor from "$lib/components/SqlEditor.svelte";
  import StatusBar from "$lib/components/StatusBar.svelte";
  import StructureView from "$lib/components/StructureView.svelte";
  import TopBar from "$lib/components/TopBar.svelte";
  import WriteDialogs from "$lib/components/WriteDialogs.svelte";

  const app = new AppState();
  setApp(app);

  let searchInput = $state<HTMLInputElement | null>(null);
  let sidebarWidth = $state(readWidth());
  let resizing = $state(false);
  let narrow = $state(window.matchMedia("(max-width: 800px)").matches);
  let navOpen = $state(!window.matchMedia("(max-width: 800px)").matches);
  let systemDark = $state(window.matchMedia("(prefers-color-scheme: dark)").matches);

  const dark = $derived(app.theme === "dark" || (app.theme === "system" && systemDark));

  function readWidth(): number {
    const n = Number(localStorage.getItem("dbadminer:sidebar"));
    return n >= 200 && n <= 480 ? n : 260;
  }

  function typingTarget(target: EventTarget | null): boolean {
    if (!(target instanceof HTMLElement)) return false;
    const tag = target.tagName;
    return tag === "INPUT" || tag === "TEXTAREA" || target.isContentEditable;
  }

  function onKey(event: KeyboardEvent) {
    const meta = event.metaKey || event.ctrlKey;
    if (meta && event.key.toLowerCase() === "k") {
      event.preventDefault();
      app.paletteOpen = !app.paletteOpen;
      return;
    }
    if (meta && event.shiftKey && event.key.toLowerCase() === "e") {
      event.preventDefault();
      app.openSql();
      return;
    }
    if (event.key === "/" && !event.metaKey && !event.ctrlKey && !event.altKey && !typingTarget(event.target)) {
      event.preventDefault();
      if (narrow) navOpen = true;
      searchInput?.focus();
    }
  }

  function startResize(event: PointerEvent) {
    resizing = true;
    (event.currentTarget as HTMLElement).setPointerCapture(event.pointerId);
  }

  function moveResize(event: PointerEvent) {
    if (!resizing) return;
    sidebarWidth = Math.min(480, Math.max(200, event.clientX));
  }

  function endResize() {
    if (!resizing) return;
    resizing = false;
    localStorage.setItem("dbadminer:sidebar", String(sidebarWidth));
  }

  $effect(() => {
    document.documentElement.classList.toggle("dark", dark);
  });

  onMount(() => {
    applyTheme(resolveTheme(app.theme) === "dark" ? "dark" : "light");
    const stopTheme = onSystemThemeChange(() => {
      systemDark = window.matchMedia("(prefers-color-scheme: dark)").matches;
    });
    const widthQuery = window.matchMedia("(max-width: 800px)");
    const onWidth = () => {
      narrow = widthQuery.matches;
      if (!narrow) navOpen = true;
    };
    widthQuery.addEventListener("change", onWidth);
    void app.init();
    return () => {
      stopTheme();
      widthQuery.removeEventListener("change", onWidth);
    };
  });
</script>

<svelte:window onkeydown={onKey} />

<Tooltip.Provider delayDuration={400}>
  <div class="flex h-dvh flex-col overflow-hidden bg-background text-foreground">
    <TopBar onToggleNav={() => (navOpen = !navOpen)} />
    <div class="relative flex min-h-0 flex-1">
      {#if navOpen}
        <div
          class="absolute inset-y-0 left-0 z-30 flex h-full border-r border-border bg-surface shadow-xl md:static md:z-0 md:shadow-none"
          style:width={narrow ? "18rem" : `${sidebarWidth}px`}
        >
          <Sidebar bind:searchInput />
          {#if !narrow}
            <div
              class="absolute inset-y-0 -right-1 z-10 w-2 cursor-col-resize"
              role="separator"
              aria-orientation="vertical"
              aria-label="Resize tables"
              onpointerdown={startResize}
              onpointermove={moveResize}
              onpointerup={endResize}
              onpointercancel={endResize}
            ></div>
          {/if}
        </div>
        {#if narrow}
          <button type="button" class="absolute inset-0 z-20 bg-black/40" aria-label="Close tables" onclick={() => (navOpen = false)}></button>
        {/if}
      {/if}
      <main class="flex min-w-0 flex-1 flex-col">
        <Tabs.Root bind:value={app.view} class="min-h-0 flex-1 gap-0">
          <div class="flex items-center border-b border-border px-2">
            <Tabs.List variant="line" class="h-9 bg-transparent">
              <Tabs.Trigger value="data">Data</Tabs.Trigger>
              <Tabs.Trigger value="structure">Structure</Tabs.Trigger>
              <Tabs.Trigger value="sql">SQL</Tabs.Trigger>
            </Tabs.List>
          </div>
          <Tabs.Content value="data" class="flex min-h-0 flex-1 flex-col data-[state=inactive]:hidden">
            <DataGrid />
          </Tabs.Content>
          <Tabs.Content value="structure" class="flex min-h-0 flex-1 flex-col data-[state=inactive]:hidden">
            <StructureView />
          </Tabs.Content>
          <Tabs.Content value="sql" class="flex min-h-0 flex-1 flex-col data-[state=inactive]:hidden">
            <SqlEditor />
          </Tabs.Content>
        </Tabs.Root>
      </main>
    </div>
    <StatusBar />
  </div>
  <CommandPalette />
  <WriteDialogs />
  <Toaster theme={dark ? "dark" : "light"} />
</Tooltip.Provider>
