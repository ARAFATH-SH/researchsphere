import { defineConfig, globalIgnores } from "eslint/config";
import nextVitals from "eslint-config-next/core-web-vitals";
import nextTs from "eslint-config-next/typescript";

const eslintConfig = defineConfig([
  ...nextVitals,
  ...nextTs,
  // API calls go through src/lib/api/client.ts. The one exception is the upload with progress
  // in src/lib/api/upload.ts, which needs XMLHttpRequest (docs/decisions.md, 2026-11-10).
  {
    files: ["src/**/*.{ts,tsx}"],
    ignores: ["src/lib/api/upload.ts", "src/lib/api/upload.test.ts"],
    rules: {
      "no-restricted-globals": [
        "error",
        { name: "XMLHttpRequest", message: "Upload with uploadFile from src/lib/api/upload.ts; call the API through src/lib/api/client.ts." },
      ],
    },
  },
  // Override default ignores of eslint-config-next.
  globalIgnores([
    // Default ignores of eslint-config-next:
    ".next/**",
    "out/**",
    "build/**",
    "next-env.d.ts",
    // Generated from docs/openapi.yaml by `npm run generate`.
    "src/lib/api/schema.gen.ts",
    "coverage/**",
  ]),
]);

export default eslintConfig;
