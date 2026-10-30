// Loaded by the build (vite.config.ts) to write dist/client-manifest.json from the real
// registry. Not imported by the application.
import { coreSets } from "@/core";
import { moduleSets } from "@/modules";
import { validateRegistry } from "@/registry/registry";
import { clientManifest } from "./manifest";

export const manifest = clientManifest(validateRegistry([...coreSets, ...moduleSets]));
