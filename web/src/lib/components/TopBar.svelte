<script lang="ts">
  import { Database, Monitor, Moon, PanelLeft, Search, ShieldAlert, ShieldCheck, Sun, SquareTerminal } from "@lucide/svelte";
  import { Badge } from "$lib/components/ui/badge";
  import { Button } from "$lib/components/ui/button";
  import { dialectLabel } from "$lib/format";
  import { getApp } from "$lib/state.svelte";
  import { modKey } from "$lib/utils";
  import Logo from "./Logo.svelte";
  import Tip from "./ui/Tip.svelte";

  interface Props {
    onToggleNav?: () => void;
  }

  let { onToggleNav }: Props = $props();

  const app = getApp();

  const themeLabel = $derived(
    app.theme === "system" ? "Theme: system" : app.theme === "dark" ? "Theme: dark" : "Theme: light",
  );
</script>

<header class="flex h-11 shrink-0 items-center gap-2 border-b border-border bg-surface px-2 sm:gap-3 sm:px-3">
  <Button variant="ghost" size="icon-sm" class="md:hidden" aria-label="Toggle tables" onclick={onToggleNav}>
    <PanelLeft />
  </Button>

  <Logo wordmark class="text-foreground" />

  {#if app.info}
    <div class="ml-1 hidden items-center gap-1.5 md:flex">
      <Badge variant="outline" title="Database dialect">
        <Database />
        {dialectLabel(app.info.dialect)}
      </Badge>
      {#if app.info.schema}
        <Badge variant="outline" class="font-mono" title="Schema">
          {app.info.schema}
        </Badge>
      {/if}
      {#if app.readOnly}
        <Badge variant="outline" class="border-transparent bg-emerald-500/15 text-emerald-700 dark:text-emerald-300" title="Only read statements are allowed">
          <ShieldCheck />
          Read-only
        </Badge>
      {:else}
        <Badge variant="outline" class="border-transparent bg-amber-500/15 text-amber-800 dark:text-amber-200" title="Write statements are allowed">
          <ShieldAlert />
          Full access
        </Badge>
      {/if}
      <Badge variant="secondary" class="font-mono" title="DBAdminer version">v{app.info.version}</Badge>
    </div>
  {/if}

  <button
    type="button"
    onclick={() => (app.paletteOpen = true)}
    class="mx-auto flex h-7 w-full max-w-sm cursor-pointer items-center gap-2 rounded-md border border-border bg-background px-2.5 text-left text-[12.5px] text-muted-foreground transition-colors hover:border-border-strong"
  >
    <Search class="size-3.5" />
    <span class="flex-1 truncate">Jump to table or command…</span>
    <span class="hidden font-mono text-[10.5px] sm:inline">{modKey} K</span>
  </button>

  <div class="flex items-center gap-1">
    <Tip label="SQL console" shortcut="{modKey} ⇧ E">
      {#snippet children(props)}
        <Button
          {...props}
          variant={app.view === "sql" ? "default" : "ghost"}
          size="sm"
          onclick={() => app.openSql()}
        >
          <SquareTerminal />
          <span class="hidden sm:inline">SQL</span>
        </Button>
      {/snippet}
    </Tip>
    <Tip label={themeLabel}>
      {#snippet children(props)}
        <Button {...props} variant="ghost" size="icon-sm" aria-label={themeLabel} onclick={() => app.cycleTheme()}>
          {#if app.theme === "system"}
            <Monitor />
          {:else if app.theme === "dark"}
            <Moon />
          {:else}
            <Sun />
          {/if}
        </Button>
      {/snippet}
    </Tip>
  </div>
</header>
