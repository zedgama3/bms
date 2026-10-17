import js from "@eslint/js";
import reactHooks from "eslint-plugin-react-hooks";
import jsxA11y from "eslint-plugin-jsx-a11y";
import globals from "globals";
import tseslint from "typescript-eslint";
import bms from "./eslint-rules/logical-properties.js";

// Module and core-area folders may import only @/api, @/i18n, @/ui, @/registry/types and
// themselves (decision 4).
const setImportBan = {
  patterns: [
    {
      group: ["@/shell", "@/shell/*", "**/shell/**"],
      message: "Screens never import the frame (decision 4).",
    },
    {
      group: ["@/core", "@/core/*", "@/modules", "@/modules/*"],
      message: "A set never imports another set or the lists (decision 4).",
    },
    {
      group: ["../../*", "../../../*"],
      message: "A set's files stay inside its own folder (decision 4).",
    },
    {
      group: ["@/registry/registry", "@/registry/registry.ts"],
      message: "Import only @/registry/types.",
    },
    {
      group: ["@/api/*", "@/i18n/*", "@/ui/*"],
      message:
        "Import @/api, @/i18n and @/ui only; their subpaths (internal.ts, client.ts, ...) are frame-private.",
    },
  ],
};

const restrictedGlobals = [
  "fetch",
  "XMLHttpRequest",
  "WebSocket",
  "EventSource",
  "localStorage",
  "sessionStorage",
  "indexedDB",
  "caches",
].map((name) => ({
  name,
  message: `${name} is not allowed here (web-shell.md "Imports and boundaries").`,
}));

export default tseslint.config(
  {
    ignores: [
      "dist",
      "coverage",
      "node_modules",
      "playwright-report",
      "test-results",
      "src/api/schema.d.ts",
      "src/i18n/keys.d.ts",
    ],
  },
  js.configs.recommended,
  ...tseslint.configs.recommendedTypeChecked,
  {
    languageOptions: {
      parserOptions: {
        projectService: { allowDefaultProject: ["eslint-rules/*.js", "eslint.config.js"] },
        tsconfigRootDir: import.meta.dirname,
      },
      globals: { ...globals.browser },
    },
  },
  reactHooks.configs.flat["recommended-latest"] ?? reactHooks.configs.flat.recommended,
  jsxA11y.flatConfigs.recommended,
  {
    files: ["src/**/*.{ts,tsx}"],
    plugins: { bms },
    rules: {
      "bms/logical-properties": "error",
      "bms/no-wide-min-width": "error",
      "bms/no-hover-disclosure": "error",
      "no-console": "error",
      "no-restricted-globals": ["error", ...restrictedGlobals],
      "no-restricted-properties": [
        "error",
        {
          object: "navigator",
          property: "sendBeacon",
          message: "Only the reporter sends beacons.",
        },
        { object: "document", property: "cookie", message: "The shell never touches cookies." },
      ],
      "no-restricted-syntax": [
        "error",
        {
          selector: "JSXAttribute[name.name='style']",
          message: "No style prop: the CSP has no 'unsafe-inline'. Use Tailwind classes.",
        },
        {
          selector: "JSXAttribute[name.name='dangerouslySetInnerHTML']",
          message: "dangerouslySetInnerHTML is not allowed (CSP).",
        },
        {
          selector: "Identifier[name='sessionStorage']",
          message: "No sessionStorage (decision 11, Data).",
        },
        { selector: "Identifier[name='indexedDB']", message: "No indexedDB (privacy-data)." },
      ],
      "@typescript-eslint/consistent-type-imports": [
        "error",
        { fixStyle: "inline-type-imports", prefer: "type-imports" },
      ],
      "@typescript-eslint/no-misused-promises": [
        "error",
        { checksVoidReturn: { attributes: false } },
      ],
    },
  },
  // fetch lives in the API client and the reporter only.
  {
    files: ["src/api/**/*.ts", "src/shell/report.ts"],
    rules: {
      "no-restricted-globals": ["error", ...restrictedGlobals.filter((g) => g.name !== "fetch")],
      "no-restricted-properties": [
        "error",
        { object: "document", property: "cookie", message: "The shell never touches cookies." },
      ],
      "no-console": ["error", { allow: ["debug", "warn"] }],
    },
  },
  {
    files: ["src/shell/report.ts"],
    rules: {
      "no-restricted-properties": [
        "error",
        { object: "document", property: "cookie", message: "The shell never touches cookies." },
      ],
    },
  },
  // The one development-only missing-translation warning (F23).
  {
    files: ["src/i18n/index.tsx"],
    rules: { "no-console": ["error", { allow: ["warn"] }] },
  },
  // localStorage lives in i18n/lang.ts only.
  {
    files: ["src/i18n/lang.ts"],
    rules: {
      "no-restricted-globals": [
        "error",
        ...restrictedGlobals.filter((g) => g.name !== "localStorage"),
      ],
    },
  },
  // Layering (web-shell.md "Imports and boundaries").
  {
    files: ["src/modules/**/*.{ts,tsx}", "src/core/**/*.{ts,tsx}"],
    ignores: ["src/modules/index.ts", "src/core/index.ts"],
    rules: { "no-restricted-imports": ["error", setImportBan] },
  },
  {
    files: ["src/shell/**/*.{ts,tsx}"],
    rules: {
      "no-restricted-imports": [
        "error",
        {
          patterns: [
            {
              group: ["@/modules/*", "@/core/*"],
              message: "The frame imports the lists (@/modules, @/core), never a set's folder.",
            },
          ],
        },
      ],
    },
  },
  {
    files: ["src/api/**/*.ts"],
    rules: {
      "no-restricted-imports": [
        "error",
        {
          patterns: [
            {
              group: [
                "@/ui",
                "@/ui/*",
                "@/shell",
                "@/shell/*",
                "@/modules*",
                "@/core*",
                "@/registry*",
                "@/i18n*",
              ],
              message: "@/api imports nothing of ours.",
            },
          ],
        },
      ],
    },
  },
  {
    files: ["src/i18n/**/*.{ts,tsx}", "src/registry/**/*.ts"],
    rules: {
      "no-restricted-imports": [
        "error",
        {
          patterns: [
            {
              group: ["@/shell", "@/shell/*", "@/modules*", "@/core*", "@/api*"],
              message: "Layering: i18n and registry sit below the frame and the API.",
            },
          ],
        },
      ],
    },
  },
  {
    files: ["src/ui/**/*.{ts,tsx}"],
    rules: {
      // ApiError (and every other @/api name) is imported as a type only: the kit never
      // makes a call and must not depend on the client at run time.
      "no-restricted-imports": "off",
      "@typescript-eslint/no-restricted-imports": [
        "error",
        {
          paths: [
            {
              name: "@/api",
              allowTypeImports: true,
              message: "ui imports types from @/api, never values.",
            },
          ],
          patterns: [
            {
              group: ["@/shell", "@/shell/*", "@/modules*", "@/core*"],
              message: "Layering: the UI kit sits below the frame.",
            },
          ],
        },
      ],
    },
  },
  // Tests may use anything.
  {
    files: ["src/**/*.test.{ts,tsx}", "src/test/**"],
    rules: {
      "no-restricted-globals": "off",
      "no-restricted-syntax": "off",
      "no-restricted-imports": "off",
      "@typescript-eslint/no-restricted-imports": "off",
      "no-restricted-properties": "off",
      "no-console": "off",
      "@typescript-eslint/unbound-method": "off",
      "@typescript-eslint/no-unsafe-assignment": "off",
      "@typescript-eslint/no-unsafe-member-access": "off",
      "@typescript-eslint/no-unsafe-call": "off",
    },
  },
  // Node scripts, configs and the end-to-end run.
  {
    files: [
      "scripts/**/*.mjs",
      "eslint-rules/**/*.{js,mjs}",
      "eslint.config.js",
      "vite.config.ts",
      "playwright.config.ts",
      "e2e/**/*.ts",
    ],
    languageOptions: { globals: { ...globals.node } },
    rules: { "no-console": "off" },
  },
  {
    files: ["scripts/**/*.mjs", "eslint-rules/**/*.{js,mjs}", "eslint.config.js"],
    ...tseslint.configs.disableTypeChecked,
  },
);
