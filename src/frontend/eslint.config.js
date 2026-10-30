// Reglas de .claude/rules/frontend.md y security.md aplicadas con ESLint (flat config).
import js from "@eslint/js";
import { defineConfig } from "eslint/config";
import boundaries from "eslint-plugin-boundaries";
import reactHooks from "eslint-plugin-react-hooks";
import reactRefresh from "eslint-plugin-react-refresh";
import globals from "globals";
import tseslint from "typescript-eslint";

export default defineConfig(
  {
    ignores: [
      "dist",
      "coverage",
      "playwright-report",
      "test-results",
      "src/app/routeTree.gen.ts",
      "src/modules/*/api/generated/**",
    ],
  },
  js.configs.recommended,
  ...tseslint.configs.strictTypeChecked,
  ...tseslint.configs.stylisticTypeChecked,
  {
    languageOptions: {
      ecmaVersion: 2023,
      globals: globals.browser,
      parserOptions: {
        projectService: { allowDefaultProject: ["eslint.config.js"] },
        tsconfigRootDir: import.meta.dirname,
      },
    },
    plugins: {
      "react-hooks": reactHooks,
      "react-refresh": reactRefresh,
      boundaries,
    },
    settings: {
      "import/resolver": { typescript: true },
      "boundaries/include": ["src/**/*"],
      "boundaries/elements": [
        { type: "app", pattern: "src/app" },
        { type: "module", pattern: "src/modules/*", capture: ["module"] },
        { type: "shared", pattern: "src/shared" },
        { type: "test", pattern: "src/test" },
      ],
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      "react-refresh/only-export-components": ["error", { allowConstantExport: true }],

      // Fronteras: un módulo no importa de otro módulo; lo compartido sube a shared/.
      "boundaries/dependencies": [
        "error",
        {
          default: "disallow",
          policies: [
            {
              from: { element: { type: "app" } },
              allow: { to: { element: { types: { anyOf: ["app", "module", "shared"] } } } },
            },
            {
              from: { element: { type: "module" } },
              allow: [
                { to: { element: { type: "shared" } } },
                {
                  to: {
                    element: {
                      type: "module",
                      captured: { module: "{{from.element.captured.module}}" },
                    },
                  },
                },
              ],
            },
            {
              from: { element: { type: "shared" } },
              allow: { to: { element: { type: "shared" } } },
            },
            {
              from: { element: { type: "test" } },
              allow: { to: { element: { types: { anyOf: ["app", "module", "shared", "test"] } } } },
            },
          ],
        },
      ],
      "boundaries/no-unknown-dependencies": "error",

      // TypeScript: sin any; aserciones `as` solo en límites, con eslint-disable justificado.
      "@typescript-eslint/no-explicit-any": "error",
      "@typescript-eslint/consistent-type-assertions": ["error", { assertionStyle: "never" }],
      "@typescript-eslint/consistent-type-imports": "error",

      // Seguridad en cliente y datos (frontend.md §Seguridad, §Datos).
      "no-eval": "error",
      "no-implied-eval": "error",
      "no-new-func": "error",
      "no-script-url": "error",
      "no-restricted-globals": [
        "error",
        {
          name: "localStorage",
          message: "No guardes datos de negocio, PII ni tokens en el navegador.",
        },
        {
          name: "sessionStorage",
          message: "No guardes datos de negocio, PII ni tokens en el navegador.",
        },
        { name: "fetch", message: "Usa el cliente generado por orval + TanStack Query." },
      ],
      "no-restricted-syntax": [
        "error",
        {
          selector: "JSXAttribute[name.name='dangerouslySetInnerHTML']",
          message: "Prohibido dangerouslySetInnerHTML (solo DOMPurify con justificación).",
        },
        {
          selector: "MemberExpression[property.name=/^(localStorage|sessionStorage)$/]",
          message: "No guardes datos de negocio, PII ni tokens en el navegador.",
        },
      ],
      "no-console": ["error", { allow: ["warn", "error"] }],
    },
  },
  {
    // Los archivos de ruta de TanStack exportan `Route`; el plugin del router gestiona el HMR.
    files: ["src/app/routes/**/*.tsx", "src/modules/*/routes/**/*.tsx"],
    rules: { "react-refresh/only-export-components": "off" },
  },
  {
    files: ["**/*.test.{ts,tsx}", "e2e/**/*.ts", "src/test/**/*.ts"],
    rules: {
      "@typescript-eslint/no-non-null-assertion": "off",
    },
  },
  {
    files: ["*.config.{js,ts}", "e2e/**/*.ts"],
    languageOptions: { globals: globals.node },
    rules: { "boundaries/no-unknown-dependencies": "off" },
  },
);
