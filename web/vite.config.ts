import { fileURLToPath, URL } from 'node:url'
import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'
import AutoImport from 'unplugin-auto-import/vite'
import Components from 'unplugin-vue-components/vite'
import { ArcoResolver } from 'unplugin-vue-components/resolvers'
import compression from 'vite-plugin-compression'

// 产物直接输出到 internal/web/dist，由 Go 侧 //go:embed all:dist 打进单一二进制。
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), 'VITE_')
  return {
    base: env.VITE_PUBLIC_PATH || '/',
    resolve: {
      alias: {
        '@': fileURLToPath(new URL('./src', import.meta.url)),
        '#': fileURLToPath(new URL('./types', import.meta.url)),
      },
    },
    plugins: [
      vue(),
      AutoImport({
        imports: ['vue', 'vue-router', 'pinia', 'vue-i18n'],
        resolvers: [ArcoResolver({ sideEffect: true })],
        dirs: ['./src/store/modules/**'],
        dts: './types/auto-imports.d.ts',
      }),
      Components({
        dirs: ['src/components'],
        deep: true,
        resolvers: [ArcoResolver({ sideEffect: true, resolveIcons: true })],
        dts: './types/components.d.ts',
      }),
      compression({ algorithm: 'gzip', ext: '.gz', threshold: 10240, deleteOriginFile: false }),
      compression({ algorithm: 'brotliCompress', ext: '.br', threshold: 10240 }),
    ],
    css: {
      preprocessorOptions: {
        scss: { api: 'modern-compiler', additionalData: '@use "@/styles/mixin.scss" as *;' },
        less: { javascriptEnabled: true },
      },
    },
    server: {
      host: '0.0.0.0',
      port: 5173,
      proxy: {
        // 开发态直连本机面板；面板默认自签证书，故 secure=false
        '/api': {
          target: env.VITE_PROXY_TARGET || 'https://127.0.0.1:34567',
          changeOrigin: true,
          secure: false,
          ws: true,
        },
      },
    },
    build: {
      outDir: '../internal/web/dist',
      emptyOutDir: true,
      target: 'es2020',
      sourcemap: mode !== 'production',
      chunkSizeWarningLimit: 1024,
      reportCompressedSize: false,
      rollupOptions: {
        output: {
          chunkFileNames: 'assets/js/[name]-[hash].js',
          entryFileNames: 'assets/js/[name]-[hash].js',
          assetFileNames: 'assets/[ext]/[name]-[hash].[ext]',
          manualChunks(id: string) {
            if (!id.includes('node_modules')) return
            if (id.includes('@arco-design')) return 'arco'
            if (/[\\/](vue|vue-router|pinia|@vue)[\\/]/.test(id)) return 'vue'
            return 'vendor'
          },
        },
      },
    },
  }
})
