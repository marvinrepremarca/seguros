// Genera un cliente TanStack Query + esquemas Zod por módulo desde el contrato OpenAPI del backend:
// src/backend/api/openapi/<modulo>.yaml → src/modules/<modulo>/api/generated/
import { readdirSync } from "node:fs";

import { defineConfig, type ConfigExternal } from "orval";

const specDir = "../backend/api/openapi";
const nonModuleSpecs = new Set(["oapi-codegen", "common"]);

const modules = readdirSync(specDir)
  .filter((f) => f.endsWith(".yaml"))
  .map((f) => f.replace(/\.yaml$/, ""))
  .filter((m) => !nonModuleSpecs.has(m));

const config: ConfigExternal = {};
for (const m of modules) {
  config[m] = {
    input: `${specDir}/${m}.yaml`,
    output: {
      target: `src/modules/${m}/api/generated/${m}.ts`,
      client: "react-query",
      httpClient: "fetch",
      mode: "single",
      clean: true,
      override: { mutator: { path: "src/shared/lib/api-client.ts", name: "apiClient" } },
    },
  };
  config[`${m}-zod`] = {
    input: `${specDir}/${m}.yaml`,
    output: { target: `src/modules/${m}/api/generated/${m}.zod.ts`, client: "zod", mode: "single" },
  };
}

export default defineConfig(config);
