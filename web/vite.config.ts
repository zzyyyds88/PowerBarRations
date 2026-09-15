import path from "node:path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// 控制台产物落在 web/dist，由 main.go 的 go:embed 打进二进制。
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./src"),
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    // 与服务端同源，无需 CDN 前缀。
    assetsDir: "assets",
  },
  server: {
    proxy: {
      // 开发态把管理 API 与模型面代理到本地网关，避免跨域配置。
      // 后端默认已启用 HTTPS（自签）：dev 代理需 secure:false 才能连自签。
      "/api": { target: process.env.PBR_DEV_BACKEND ?? "https://127.0.0.1:5700", changeOrigin: true, secure: false },
      "/v1": { target: process.env.PBR_DEV_BACKEND ?? "https://127.0.0.1:5700", changeOrigin: true, secure: false },
    },
  },
});
