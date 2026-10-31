<script lang="ts">
  import { cn } from "$lib/utils";

  interface Props {
    size?: number;
    wordmark?: boolean;
    class?: string;
  }

  let { size = 22, wordmark = false, class: className = "" }: Props = $props();
</script>

<span class={cn("inline-flex items-center gap-2", className)}>
  <svg
    width={size}
    height={size}
    viewBox="0 0 24 24"
    fill="none"
    stroke-width="2"
    stroke-linecap="round"
    stroke-linejoin="round"
    role="img"
    aria-label="DBAdminer"
  >
    <g stroke="currentColor">
      <ellipse cx="10" cy="5" rx="7" ry="3" />
      <path d="M3 5v12c0 1.66 3.13 3 7 3" />
      <path d="M3 11c0 1.66 3.13 3 7 3" />
      <path d="M17 5v5" />
    </g>
    <g class="text-primary" stroke="currentColor">
      <circle cx="17.5" cy="16.5" r="3.5" />
      <path d="m20.2 19.2 2.3 2.3" />
    </g>
  </svg>
  {#if wordmark}
    <span class="text-[14px] font-semibold tracking-tight">
      DB<span class="text-primary">Adminer</span>
    </span>
  {/if}
</span>
