import { defineConfig } from "vite";

export default defineConfig({
  root: "web",
  build: {
    outDir: "../dist",
    emptyOutDir: true,
  },
  // base: "/absproxy/5173",
  server: {
    allowedHosts: true,
    // web/src/tuning.ts imports the shared tables from data/tuning, one level
    // above the Vite root.
    fs: { allow: [".."] },

    proxy: {
      "/api": "http://localhost:8080",
      "/ws": { target: "ws://localhost:8080", ws: true },
    }
  },
});
