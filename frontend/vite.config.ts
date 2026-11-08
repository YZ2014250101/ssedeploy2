import path from "node:path"
import { defineConfig } from "vite"
import react from "@vitejs/plugin-react"

// 开发环境把 /api 代理到后端；可用 VITE_API_TARGET 覆盖默认地址
const apiTarget = process.env.VITE_API_TARGET || "http://127.0.0.1:8080"

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/api": { target: apiTarget, changeOrigin: true, ws: true },
      "/healthz": { target: apiTarget, changeOrigin: true },
    },
  },
})
