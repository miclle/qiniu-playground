import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

const vitePort = Number.parseInt(process.env.QINIU_PLAYGROUND_VITE_PORT ?? '19173', 10)
const apiBaseURL =
  process.env.QINIU_PLAYGROUND_API_BASE_URL ??
  `http://127.0.0.1:${process.env.QINIU_PLAYGROUND_HTTP_PORT ?? '19090'}`

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: 'build',
  },
  resolve: {
    alias: {
      "@": "/src",
      src: "/src",
    },
  },
  server: {
    port: vitePort,
    strictPort: true,
    host: "0.0.0.0",
    allowedHosts: true,
    proxy: {
      "/api/v1": apiBaseURL,
    },
  },
})
