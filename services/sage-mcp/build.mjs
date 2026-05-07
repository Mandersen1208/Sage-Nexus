// build.mjs — esbuild bundler (replaces tsc for environments without typescript installed)
import { build } from "esbuild";
import { mkdirSync } from "fs";

mkdirSync("dist", { recursive: true });

await build({
  entryPoints: ["src/index.ts"],
  bundle: true,
  platform: "node",
  target: "node18",
  format: "esm",
  outfile: "dist/index.js",
  external: [
    "@modelcontextprotocol/sdk",
    "zod",
  ],
  sourcemap: true,
});

console.log("Build complete → dist/index.js");
