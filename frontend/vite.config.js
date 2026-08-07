import { defineConfig } from 'vite'
import legacy from '@vitejs/plugin-legacy'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [
    vue(),
    legacy({
      targets: ['Chrome >= 72'],
      modernPolyfills: ['es.object.from-entries', 'es.array.flat'],
      renderLegacyChunks: false,
    }),
  ],
  server: {
    port: 3000,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
      '/ws': {
        target: 'ws://localhost:8080',
        ws: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: false,
    target: 'chrome72',
    cssTarget: 'chrome72',
  },
})
