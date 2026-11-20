import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import tailwindcss from '@tailwindcss/vite'
import wails from '@wailsio/runtime/plugins/vite'
import { fileURLToPath } from 'node:url'

export default defineConfig({
	// wails() first: it resolves the generated bindings under ./bindings, and
	// without it `vite build` fails on the import rather than in one place.
	plugins: [wails('./bindings'), tailwindcss(), svelte()],
	resolve: {
		alias: {
			$lib: fileURLToPath(new URL('./src/lib', import.meta.url))
		}
	},
	// Wails serves this folder, and `//go:embed all:frontend/dist` reads it.
	build: { outDir: 'dist', emptyOutDir: true }
})