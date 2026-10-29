---
paths:
  - "src/frontend/**"
---
# Reglas de frontend (React + TypeScript)

## Estructura
```
src/frontend/src/
  app/                  bootstrap, router raíz, providers, layout
  modules/<modulo>/     routes/, components/, hooks/, api/ (generado), schemas/, index.ts
  shared/ui/            design system (shadcn/ui personalizado)
  shared/lib/           formatters (moneda COP, fechas), feature flags, auth
```
- Un módulo no importa de otro módulo; lo compartido sube a `shared/` (regla ESLint `boundaries`).
- Cada módulo se carga con lazy route y está envuelto en su `FeatureGate`.

## TypeScript
- `strict`, `noUncheckedIndexedAccess`, `exactOptionalPropertyTypes`. Prohibido `any` y `as` salvo en límites con comentario justificando.
- Montos llegan como **string decimal**; se manejan con `decimal.js`/`big.js` y se formatean con `Intl.NumberFormat('es-CO', {style:'currency', currency:'COP'})`. Nunca aritmética con `number` sobre dinero.
- Fechas de vigencia (solo fecha) como `YYYY-MM-DD` string; no convertir a `Date` (evita corrimientos de zona).

## Datos y estado
- Estado de servidor solo con TanStack Query y el cliente generado por orval. Prohibido `fetch` manual a la API.
- Query keys incluyen el módulo y parámetros; invalidación explícita tras mutaciones.
- Zustand solo para estado de UI efímero. No guardar datos de negocio ni PII en `localStorage`/`sessionStorage`.

## Formularios
- React Hook Form + Zod; los esquemas Zod se derivan del OpenAPI y se extienden con reglas de UI. Mensajes en español.
- Validación de documentos colombianos (CC, CE, NIT con dígito de verificación, PPT) en `shared/lib/validators`.

## Seguridad en cliente
- Sin tokens en JS. Todas las llamadas van al BFF con cookies; incluye header anti-CSRF.
- Prohibido `dangerouslySetInnerHTML`, `eval`, `new Function`, URLs `javascript:`. HTML externo solo vía DOMPurify.
- Datos Restringidos enmascarados por defecto (`•••• 1234`), revelar requiere acción explícita (y el backend lo audita).
- Ocultar un botón NO es autorización: el permiso real lo valida el backend; la UI usa permisos solo para UX.

## UX / accesibilidad
- WCAG 2.2 AA: navegación por teclado, labels, foco visible, contraste. Probar con `@axe-core/playwright` en E2E.
- Tablas grandes (cartera, pólizas) virtualizadas con TanStack Table + paginación servidor.
- Estados vacíos, de carga (skeleton) y de error en toda vista.
- Responsivo desde 360 px.

## Pruebas
- Vitest + Testing Library (comportamiento, no implementación) + MSW para mocks de API.
- Playwright E2E para flujos críticos: cotizar→expedir, registrar pago, reportar siniestro, apagar/encender módulo.
