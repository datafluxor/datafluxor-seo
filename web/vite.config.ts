import { writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

const outDir = resolve(__dirname, "../internal/server/dist");

// The built app is embedded in the Go binary (internal/server/dist). The
// folder keeps a committed .gitkeep so `go build` works on a fresh clone
// before the web app is built; the build empties the folder, so it is put back.
export default defineConfig({
  plugins: [
    react(),
    { name: "keep-dist-placeholder", closeBundle: () => writeFileSync(resolve(outDir, ".gitkeep"), "") },
  ],
  build: { outDir, emptyOutDir: true, sourcemap: false },
  server: { proxy: { "/api": "http://localhost:8080" } },
});
