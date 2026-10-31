<script lang="ts">
  import { Braces, ChevronDown, Download, FileSpreadsheet } from "@lucide/svelte";
  import type { ResultSet } from "$lib/api";
  import { Button } from "$lib/components/ui/button";
  import * as DropdownMenu from "$lib/components/ui/dropdown-menu";
  import { exportResult } from "$lib/export";

  interface Props {
    data: ResultSet | null;
    basename: string;
  }

  let { data, basename }: Props = $props();
</script>

<DropdownMenu.Root>
  <DropdownMenu.Trigger disabled={!data || data.columns.length === 0}>
    {#snippet child({ props })}
      <Button {...props} variant="outline" size="sm">
        <Download />
        Export
        <ChevronDown class="text-muted-foreground" />
      </Button>
    {/snippet}
  </DropdownMenu.Trigger>
  <DropdownMenu.Content align="end">
    <DropdownMenu.Item onSelect={() => data && exportResult(data, basename, "csv")}>
      <FileSpreadsheet />
      CSV
    </DropdownMenu.Item>
    <DropdownMenu.Item onSelect={() => data && exportResult(data, basename, "json")}>
      <Braces />
      JSON
    </DropdownMenu.Item>
  </DropdownMenu.Content>
</DropdownMenu.Root>
