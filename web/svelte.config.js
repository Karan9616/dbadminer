import { vitePreprocess } from '@sveltejs/vite-plugin-svelte'

/** @type {import('svelte/compiler').Preprocess} */
export default {
  preprocess: vitePreprocess(),
}
