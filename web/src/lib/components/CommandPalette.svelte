<script lang="ts">
  import { Eye, Moon, SquareTerminal, Table2, Trash2, RefreshCw } from "@lucide/svelte";
  import { tableKey, type TableInfo } from "$lib/api";
  import * as Command from "$lib/components/ui/command";
  import { getApp } from "$lib/state.svelte";

  const app = getApp();

  function close() {
    app.paletteOpen = false;
  }

  function openTable(table: TableInfo) {
    close();
    void app.selectTable(table, "data");
  }

  function isView(table: TableInfo): boolean {
    return table.type.toLowerCase().includes("view");
  }
</script>

<Command.Dialog
  title="Command palette"
  description="Jump to a table or run a command"
  open={app.paletteOpen}
  onOpenChange={(open) => (app.paletteOpen = open)}
>
  <Command.Input placeholder="Jump to a table or command…" />
  <Command.List>
    <Command.Empty>No matches.</Command.Empty>
    <Command.Group heading="Actions">
      <Command.Item
        value="toggle theme appearance"
        onSelect={() => {
          app.cycleTheme();
          close();
        }}
      >
        <Moon />
        Toggle theme
      </Command.Item>
      <Command.Item
        value="open sql console"
        onSelect={() => {
          app.openSql();
          close();
        }}
      >
        <SquareTerminal />
        Open SQL
      </Command.Item>
      <Command.Item
        value="reload tables"
        onSelect={() => {
          close();
          void app.loadTables();
        }}
      >
        <RefreshCw />
        Reload tables
      </Command.Item>
      <Command.Item
        value="clear query history"
        onSelect={() => {
          app.clearHistory();
          close();
        }}
      >
        <Trash2 />
        Clear history
      </Command.Item>
    </Command.Group>
    <Command.Separator />
    <Command.Group heading="Tables">
      {#each app.tables as table (tableKey(table))}
        <Command.Item value={`${table.schema} ${table.name} ${table.type}`} onSelect={() => openTable(table)}>
          {#if isView(table)}
            <Eye />
          {:else}
            <Table2 />
          {/if}
          <span class="truncate font-mono text-[12.5px]">{table.schema}.{table.name}</span>
          <span class="ml-auto text-[11px] text-muted-foreground">{table.type}</span>
        </Command.Item>
      {/each}
    </Command.Group>
  </Command.List>
</Command.Dialog>
