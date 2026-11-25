import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  build: {
    outDir: '../internal/web/dist',
    emptyOutDir: true,
    // Shared vendor code is stable across releases and can stay cached while
    // business chunks change. Without this, one i18n lookup dragged the whole
    // English catalogue into the first-load chunk.
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('node_modules/@lucide/')) return 'icons'
          if (id.includes('node_modules/@vue/') || id.includes('node_modules/vue/')) return 'vue'
          return undefined
        },
      },
    },
  },
  server: { proxy: { '/api': 'http://127.0.0.1:8181' } },
})
