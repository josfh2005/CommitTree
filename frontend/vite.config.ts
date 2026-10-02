import {defineConfig} from 'vite'
import {svelte} from '@sveltejs/vite-plugin-svelte'
import wails from '../wails.json'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [svelte()],
  // The app's version has one source, wails.json, which also stamps the
  // macOS bundle; the sidebar shows it next to Settings.
  define: {
    __APP_VERSION__: JSON.stringify(wails.info.productVersion),
  },
})
