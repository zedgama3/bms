import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { createServer } from "vite";
import type { Plugin } from "vite";
import { defineConfig } from "vitest/config";

// The bundle's version is the binary's version (decision 20): BMS_VERSION, else "dev".
// It is also what the reporter sends as app_version, so it must be a telemetry token of at
// most 64 bytes ([A-Za-z0-9/_][A-Za-z0-9._:/{}-]{0,127}).
const version = process.env["BMS_VERSION"]?.trim() || "dev";
if (version.length > 64 || !/^[A-Za-z0-9/_][A-Za-z0-9._:/{}-]*$/.test(version)) {
  throw new Error(
    `BMS_VERSION ${JSON.stringify(version)} is not a telemetry token of at most 64 bytes`,
  );
}

/** Writes dist/version.txt, which web.Version() reads, and puts back dist/.gitkeep, which the
 *  Go embed needs in a checkout without a build and which emptying dist/ would delete. */
function versionFile(): Plugin {
  return {
    name: "bms-version-file",
    generateBundle() {
      this.emitFile({ type: "asset", fileName: "version.txt", source: version });
      this.emitFile({ type: "asset", fileName: ".gitkeep", source: "" });
    },
  };
}

/** Writes dist/client-manifest.json: the screen patterns and declared action names, from the
 *  routes the registry really builds (telemetry.md T1.5). The server reads it to know which
 *  usage events to accept. The registry is loaded through a throwaway Vite server, so the
 *  file is made from the same code the bundle is. */
function clientManifest(): Plugin {
  let json = "";
  return {
    name: "bms-client-manifest",
    apply: "build",
    async buildStart() {
      const server = await createServer({
        configFile: false,
        root: fileURLToPath(new URL(".", import.meta.url)),
        logLevel: "silent",
        appType: "custom",
        server: { middlewareMode: true, hmr: false, watch: null },
        optimizeDeps: { noDiscovery: true },
        resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
        define: { __BMS_VERSION__: JSON.stringify(version) },
      });
      // The API client reads `location.origin` when it is created; Node has none.
      const g = globalThis as { location?: unknown };
      const had = "location" in g;
      if (!had) g.location = new URL("http://localhost/");
      try {
        const mod = await server.ssrLoadModule("/src/shell/manifestEntry.ts");
        json = JSON.stringify(mod["manifest"], null, 2) + "\n";
      } finally {
        if (!had) delete g.location;
        await server.close();
      }
    },
    generateBundle() {
      this.emitFile({ type: "asset", fileName: "client-manifest.json", source: json });
    },
  };
}

export default defineConfig({
  plugins: [react(), tailwindcss(), versionFile(), clientManifest()],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  define: { __BMS_VERSION__: JSON.stringify(version) },
  build: {
    target: "baseline-widely-available",
    manifest: true,
    outDir: "dist",
    emptyOutDir: true,
    assetsInlineLimit: 0,
    sourcemap: false,
  },
  server: {
    port: 5173,
    proxy: {
      "/api": "http://localhost:8080",
      "/healthz": "http://localhost:8080",
      "/readyz": "http://localhost:8080",
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    include: ["src/**/*.test.{ts,tsx}", "scripts/**/*.test.mjs", "eslint-rules/**/*.test.mjs"],
    restoreMocks: true,
    // Under load (a parallel build, a phone-sized CI box) many workers starve each other: cap them
    // and allow a slow test to finish rather than time out.
    maxWorkers: "50%",
    testTimeout: 15000,
    hookTimeout: 15000,
    coverage: {
      provider: "v8",
      include: ["src/**/*.{ts,tsx}"],
      exclude: [
        "src/**/*.test.{ts,tsx}",
        "src/test/**",
        "src/api/schema.d.ts",
        "src/i18n/keys.d.ts",
        "src/**/*.d.ts",
      ],
      reporter: ["text-summary", "lcov"],
    },
  },
});
