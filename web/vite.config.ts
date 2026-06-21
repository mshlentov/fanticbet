import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Dev-сервер фронта проксирует /api на Go-бэкенд (:8080). Браузер при этом
// работает с одним origin (localhost:5173) — поэтому httpOnly refresh-cookie
// уходит на бэкенд без CORS-проблем, а Bearer-токен подставляет API-клиент.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: "http://localhost:8080",
        changeOrigin: true,
      },
    },
  },
});
