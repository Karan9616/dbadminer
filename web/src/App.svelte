<script lang="ts">
  import { onMount } from "svelte";
  import { Toaster } from "$lib/components/ui/sonner";
  import * as Tabs from "$lib/components/ui/tabs";
  import * as Tooltip from "$lib/components/ui/tooltip";
  import { setApp, AppState } from "$lib/state.svelte";
  import { applyTheme, onSystemThemeChange, resolveTheme } from "$lib/theme";
  import { cn } from "$lib/utils";
  import CommandPalette from "$lib/components/CommandPalette.svelte";
  import DataGrid from "$lib/components/DataGrid.svelte";
  import RoutineView from "$lib/components/RoutineView.svelte";
  import Sidebar from "$lib/components/Sidebar.svelte";
  import SqlEditor from "$lib/components/SqlEditor.svelte";
  import StatusBar from "$lib/components/StatusBar.svelte";
  import StructureView from "$lib/components/StructureView.svelte";
  import TopBar from "$lib/components/TopBar.svelte";
  import WriteDialogs from "$lib/components/WriteDialogs.svelte";

  const app = new AppState();
  setApp(app);

  const SIDEBAR_MIN = 180;
  const SIDEBAR_MAX = 640;
  const SIDEBAR_DEFAULT = 260;

  let searchInput = $state<HTMLInputElement | null>(null);
  let sidebarWidth = $state(readWidth());
  let resizing = $state(false);
  let narrow = $state(window.matchMedia("(max-width: 800px)").matches);
  let navOpen = $state(!window.matchMedia("(max-width: 800px)").matches);
  let systemDark = $state(window.matchMedia("(prefers-color-scheme: dark)").matches);
  let shellEl = $state<HTMLDivElement | null>(null);

  const dark = $derived(app.theme === "dark" || (app.theme === "system" && systemDark));

  function readWidth(): number {
    const n = Number(localStorage.getItem("dbadminer:sidebar"));
    return n >= SIDEBAR_MIN && n <= SIDEBAR_MAX ? n : SIDEBAR_DEFAULT;
  }

  function clampWidth(px: number): number {
    return Math.min(SIDEBAR_MAX, Math.max(SIDEBAR_MIN, Math.round(px)));
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

  function widthFromClientX(clientX: number): number {
    const left = shellEl?.getBoundingClientRect().left ?? 0;
    return clampWidth(clientX - left);
  }

  function onResizeMove(event: PointerEvent) {
    sidebarWidth = widthFromClientX(event.clientX);
  }

  function onResizeEnd() {
    if (!resizing) return;
    resizing = false;
    document.body.style.cursor = "";
    document.body.style.userSelect = "";
    window.removeEventListener("pointermove", onResizeMove);
    window.removeEventListener("pointerup", onResizeEnd);
    window.removeEventListener("pointercancel", onResizeEnd);
    localStorage.setItem("dbadminer:sidebar", String(sidebarWidth));
  }

  function startResize(event: PointerEvent) {
    if (narrow) return;
    event.preventDefault();
    event.stopPropagation();
    resizing = true;
    sidebarWidth = widthFromClientX(event.clientX);
    document.body.style.cursor = "col-resize";
    document.body.style.userSelect = "none";
    window.addEventListener("pointermove", onResizeMove);
    window.addEventListener("pointerup", onResizeEnd);
    window.addEventListener("pointercancel", onResizeEnd);
  }

  function resetSidebarWidth() {
    sidebarWidth = SIDEBAR_DEFAULT;
    localStorage.setItem("dbadminer:sidebar", String(SIDEBAR_DEFAULT));
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
      window.removeEventListener("pointermove", onResizeMove);
      window.removeEventListener("pointerup", onResizeEnd);
      window.removeEventListener("pointercancel", onResizeEnd);
    };
  });
</script>

<svelte:window onkeydown={onKey} />

<Tooltip.Provider delayDuration={400}>
  <div class="flex h-dvh flex-col overflow-hidden bg-background text-foreground">
    <TopBar onToggleNav={() => (navOpen = !navOpen)} />
    <div bind:this={shellEl} class="relative flex min-h-0 flex-1" class:select-none={resizing}>
      {#if navOpen}
        {#if narrow}
          <div
            class="absolute inset-y-0 left-0 z-30 flex h-full w-72 flex-col border-r border-border bg-surface shadow-xl"
          >
            <Sidebar bind:searchInput />
          </div>
          <button
            type="button"
            class="absolute inset-0 z-20 bg-black/40"
            aria-label="Close tables"
            onclick={() => (navOpen = false)}
          ></button>
        {:else}
          <aside
            class="flex h-full shrink-0 flex-col overflow-hidden border-r border-border bg-surface"
            style:width={`${sidebarWidth}px`}
          >
            <Sidebar bind:searchInput />
          </aside>
          <!-- Thin border + wide invisible sash (VS Code–style) -->
          <div
            class="relative z-10 -ml-px w-px shrink-0 bg-border"
            class:bg-primary={resizing}
            role="separator"
            aria-orientation="vertical"
            aria-label="Resize sidebar"
            aria-valuenow={sidebarWidth}
            aria-valuemin={SIDEBAR_MIN}
            aria-valuemax={SIDEBAR_MAX}
            title="Drag to resize · double-click to reset"
          >
            <button
              type="button"
              class={cn(
                "absolute inset-y-0 -left-1.5 w-3 cursor-col-resize border-0 bg-transparent p-0",
                "hover:bg-primary/15",
                resizing && "bg-primary/20",
              )}
              aria-label="Resize sidebar"
              onpointerdown={startResize}
              ondblclick={resetSidebarWidth}
            ></button>
          </div>
        {/if}
      {/if}
      <main class="flex min-w-0 flex-1 flex-col">
        {#if app.view === "sql"}
          <div class="min-h-0 flex-1 flex flex-col">
            <SqlEditor />
          </div>
        {:else}
          <Tabs.Root bind:value={app.view} class="min-h-0 flex-1 gap-0">
            <div class="flex items-center border-b border-border px-2">
              <Tabs.List variant="line" class="h-9 bg-transparent">
                <Tabs.Trigger value="data">Data</Tabs.Trigger>
                <Tabs.Trigger value="structure">Structure</Tabs.Trigger>
              </Tabs.List>
            </div>
            <Tabs.Content value="data" class="flex min-h-0 flex-1 flex-col data-[state=inactive]:hidden">
              {#if app.selectedRoutine}
                <div class="grid flex-1 place-items-center gap-2 p-6 text-center text-muted-foreground">
                  <p>Routines have no row data.</p>
                  <button
                    type="button"
                    class="cursor-pointer text-primary underline-offset-2 hover:underline"
                    onclick={() => (app.view = "structure")}
                  >
                    View definition
                  </button>
                </div>
              {:else}
                <DataGrid />
              {/if}
            </Tabs.Content>
            <Tabs.Content value="structure" class="flex min-h-0 flex-1 flex-col data-[state=inactive]:hidden">
              {#if app.selectedRoutine}
                <RoutineView />
              {:else}
                <StructureView />
              {/if}
            </Tabs.Content>
          </Tabs.Root>
        {/if}
      </main>
    </div>
    <StatusBar />
  </div>
  <CommandPalette />
  <WriteDialogs />
  <Toaster theme={dark ? "dark" : "light"} />
</Tooltip.Provider>
