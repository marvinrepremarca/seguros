import { fileURLToPath, URL } from "node:url";

import tailwindcss from "@tailwindcss/vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig(({ command }) => ({
  // Solo en el build: la API (httpx.SPAHandler) reemplaza este marcador por el nonce de cada
  // petición. En desarrollo no hay CSP y Vite inyecta scripts propios.
  ...(command === "build" ? { html: { cspNonce: "__CSP_NONCE__" } } : {}),
  plugins: [
    // Debe ir antes de react(): genera src/app/routeTree.gen.ts y divide el código por ruta.
    tanstackRouter({
      target: "react",
      routesDirectory: "./src/app/routes",
      generatedRouteTree: "./src/app/routeTree.gen.ts",
      autoCodeSplitting: true,
    }),
    react(),
    tailwindcss(),
  ],
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
  server: {
    port: 5173,
    strictPort: true,
    // El navegador solo habla con el BFF (cookies httpOnly); nunca con Keycloak ni con tokens.
    proxy: {
      "/api": "http://localhost:8080",
      "/bff": "http://localhost:8080",
    },
  },
  build: {
    sourcemap: false,
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
    restoreMocks: true,
    coverage: {
      provider: "v8",
      include: ["src/**/*.{ts,tsx}"],
      exclude: ["src/**/*.gen.ts", "src/**/api/generated/**", "src/main.tsx"],
    },
  },
}));
