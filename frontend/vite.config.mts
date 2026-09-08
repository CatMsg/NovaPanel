// Plugins
import vue from '@vitejs/plugin-vue'
import vuetify, { transformAssetUrls } from 'vite-plugin-vuetify'

// Utilities
import { defineConfig } from 'vite'
import { fileURLToPath, URL } from 'node:url'

export default defineConfig({
  base: '',
  plugins: [
    vue({
      template: { transformAssetUrls },
    }),
    vuetify({
      autoImport: true,
      styles: {
        configFile: 'src/styles/settings.scss',
      },
    })
  ],
  build: {
    manifest: false,
    outDir: 'dist',
    chunkSizeWarningLimit: 2000,
    rollupOptions: {
      output: {
        entryFileNames: 'assets/[name]-[hash].js',
        chunkFileNames: 'assets/[name]-[hash].js',
        assetFileNames: 'assets/[name]-[hash][extname]',
        manualChunks(id) {
          if (!id.includes('node_modules')) {
            return undefined
          }

          if (id.includes('vue/') || id.includes('vue-router') || id.includes('pinia')) {
            return 'framework'
          }
          if (id.includes('vuetify') || id.includes('@mdi/font') || id.includes('roboto-fontface')) {
            return 'ui'
          }
          if (id.includes('chart.js') || id.includes('vue-chartjs')) {
            return 'charts'
          }
          if (id.includes('vue3-persian-datetime-picker') || id.includes('moment')) {
            return 'datetime'
          }
          if (id.includes('notivue') || id.includes('axios') || id.includes('yaml') || id.includes('qrcode.vue')) {
            return 'vendor'
          }

          return 'vendor'
        },
      },
    }
  },
  define: { 'process.env': {} },
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
    extensions: ['.js', '.json', '.jsx', '.mjs', '.ts', '.tsx', '.vue'],
  },
  server: {
    port: 3000,
    proxy: {
      '/app/api': {
        target: 'http://localhost:2095',
        changeOrigin: true,
      },
    },
  }
})
