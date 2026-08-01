// Vite 配置统一应用基础路径、React 构建与测试运行环境。
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
  base: process.env.VITE_BASE_PATH ?? "/travel/",
  plugins: [react()],
  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes("/node_modules/@phosphor-icons/")) return "icons";
          if (id.includes("/node_modules/@tanstack/")) return "query";
          if (
            id.includes("/node_modules/react/")
            || id.includes("/node_modules/react-dom/")
            || id.includes("/node_modules/react-router/")
          ) return "react";
          return undefined;
        }
      }
    }
  },
  test: {
    environment: "node",
    exclude: ["e2e/**", "node_modules/**", "dist/**"],
    setupFiles: ["./src/test/setup.ts"],
    coverage: {
      provider: "v8",
      reporter: ["text", "json-summary"],
      include: ["src/**/*.{ts,tsx}"],
      exclude: [
        "src/**/*.test.{ts,tsx}",
        "src/test/**",
        "src/main.tsx",
        "src/vite-env.d.ts"
      ],
      thresholds: {
        lines: 16,
        functions: 15,
        statements: 16,
        branches: 13
      }
    }
  },
  server: {
    host: "127.0.0.1",
    port: 5173,
    proxy: {
      "/travel/api": {
        target: "http://127.0.0.1:8502",
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/travel\/api/, "")
      },
      "/_AMapService": {
        target: "http://127.0.0.1:8502",
        changeOrigin: true
      }
    }
  }
});
