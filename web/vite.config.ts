import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The built app is embedded in the Go binary (internal/server/dist).
export default defineConfig({
  plugins: [react()],
  build: { outDir: "../internal/server/dist", emptyOutDir: true, sourcemap: false },
  server: { proxy: { "/api": "http://localhost:8080" } },
});
