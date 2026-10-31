import vue from "@vitejs/plugin-vue";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [vue()],
  resolve: {
    dedupe: ["vue"],
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    assetsDir: "assets",
  },
  server: {
    proxy: {
      "/v1": "http://127.0.0.1:8443",
      "/login": "http://127.0.0.1:8443",
      "/logout": "http://127.0.0.1:8443",
    },
  },
});
