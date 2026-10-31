<script lang="ts">
  import type { Snippet } from "svelte";
  import * as Tooltip from "$lib/components/ui/tooltip";
  import { Kbd } from "$lib/components/ui/kbd";

  interface Props {
    label: string;
    shortcut?: string;
    side?: "top" | "bottom" | "left" | "right";
    children: Snippet<[Record<string, unknown>]>;
  }

  let { label, shortcut, side = "bottom", children }: Props = $props();
</script>

<Tooltip.Root>
  <Tooltip.Trigger>
    {#snippet child({ props })}
      {@render children(props)}
    {/snippet}
  </Tooltip.Trigger>
  <Tooltip.Content {side} sideOffset={6}>
    {label}
    {#if shortcut}
      <Kbd>{shortcut}</Kbd>
    {/if}
  </Tooltip.Content>
</Tooltip.Root>
