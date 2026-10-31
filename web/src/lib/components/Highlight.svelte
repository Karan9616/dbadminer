<script lang="ts">
  interface Props {
    text: string;
    query: string;
  }

  let { text, query }: Props = $props();

  const parts = $derived.by(() => {
    const q = query.trim().toLowerCase();
    if (!q) return [{ text, match: false }];
    const i = text.toLowerCase().indexOf(q);
    if (i < 0) return [{ text, match: false }];
    return [
      { text: text.slice(0, i), match: false },
      { text: text.slice(i, i + q.length), match: true },
      { text: text.slice(i + q.length), match: false },
    ];
  });
</script>

{#each parts as part, i (i)}{#if part.match}<mark class="rounded-sm bg-primary/15 text-primary">{part.text}</mark>
    >{:else}{part.text}{/if}{/each}
