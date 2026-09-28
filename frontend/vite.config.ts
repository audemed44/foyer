import preact from "@preact/preset-vite";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [preact()],
  build: { outDir: "../web/dist", emptyOutDir: true, assetsInlineLimit: 0 },
  server: {
    // `npm run dev` proxies the API to a local `foyer` binary.
    proxy: {
      "/api": "http://localhost:8080",
      "/icons": "http://localhost:8080",
      "/images": "http://localhost:8080",
    },
  },
  test: { environment: "jsdom" },
});
