import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The production bundle is embedded by the Go binary. Fixed entry names keep
// the embed layout simple; the server marks assets no-cache so an app update
// cannot leave a stale console in the browser.
export default defineConfig({
  plugins: [react()],
  base: "/",
  build: {
    outDir: "../internal/remote/webui",
    emptyOutDir: true,
    assetsDir: "assets",
    sourcemap: false,
    rollupOptions: {
      output: {
        entryFileNames: "assets/app.js",
        chunkFileNames: "assets/chunk-[name].js",
        assetFileNames: (assetInfo) => {
          if (assetInfo.name?.endsWith(".css")) return "assets/app.css";
          return "assets/[name][extname]";
        },
      },
    },
  },
  server: {
    host: "127.0.0.1",
    port: 5173,
    proxy: {
      "/api": {
        target: "http://127.0.0.1:7899",
        changeOrigin: true,
        configure: (proxy) => {
          // Vite 8's proxy typing omits the Node proxy events; the dev server
          // only needs to make same-origin checks pass when /api is proxied.
          (proxy as unknown as { on: (event: string, cb: (request: { setHeader: (name: string, value: string) => void }) => void) => void }).on(
            "proxyReq",
            (request) => request.setHeader("Origin", "http://127.0.0.1:7899"),
          );
        },
      },
    },
  },
});
