import { defineConfig } from 'vite'
import legacy from '@vitejs/plugin-legacy'
import vue from '@vitejs/plugin-vue'
import postcss from 'postcss'
import flexGapPolyfill from 'flex-gap-polyfill'

const inheritedFlexGapSelectors = [
  '.el-cascader--large .el-cascader__tags',
  '.el-cascader--small .el-cascader__tags',
  '.el-collapse-icon-position-left .el-collapse-item__header',
  '.el-select--large .el-select__wrapper',
  '.el-select--large .el-select__selection',
  '.el-select--large .el-select__prefix,.el-select--large .el-select__suffix',
  '.el-select--small .el-select__wrapper',
  '.el-select--small .el-select__selection',
  '.el-select--small .el-select__prefix,.el-select--small .el-select__suffix',
]

const elementPlusFlexGapFallback = () => ({
  name: 'element-plus-flex-gap-fallback',
  enforce: 'pre',
  async transform(source, id) {
    if (!id.split('?')[0].endsWith('/element-plus/dist/index.css')) return null
    const result = await postcss([
      flexGapPolyfill({ only: inheritedFlexGapSelectors }),
    ]).process(source, { from: id })
    return { code: result.css, map: null }
  },
})

export default defineConfig({
  plugins: [
    elementPlusFlexGapFallback(),
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
