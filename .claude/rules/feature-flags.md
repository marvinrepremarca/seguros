---
paths:
  - "src/**"
---
# Reglas de Feature Flags (obligatorias)

El producto vende módulos y funcionalidades que cada tenant puede tener **encendidos o apagados de forma independiente**. Cualquier código que no respete estas reglas rompe el modelo comercial y la seguridad.

## Nomenclatura (kebab-case, sin espacios)
| Tipo | Patrón | Ejemplo |
|---|---|---|
| Módulo | `module.<modulo>` | `module.claims` |
| Funcionalidad | `<modulo>.<funcionalidad>` | `billing.payment-reminders` |
| Release (temporal) | `release.<modulo>.<cambio>` | `release.commissions.auto-reconciliation` |
| Operación / kill switch | `ops.<area>.<cosa>` | `ops.integrations.insurer-sura` |

## Registro
- Todo flag existe en el registro central `src/backend/internal/platform/flags/registry.yaml` (clave, tipo, descripción, default, `requires[]`, dueño, planes, y para release: `removeBy`).
- Prohibido evaluar claves literales no registradas: usa constantes generadas por `make generate` (Go: `flags.ClaimsModule`; TS: `features.claims.module`).
- Flags de módulo declaran sus dependencias en `requires` (ej. `module.commissions` requiere `module.billing` porque causa comisión sobre prima pagada). Los módulos núcleo nunca aparecen como dependientes de un apagable. No se puede encender un módulo con dependencias apagadas, ni apagar uno del que dependan módulos encendidos. Consulta ARCHITECTURE.md §1.2.

## Aplicación en TODAS las capas (las 4 son obligatorias)
1. **Endpoint**: middleware `flags.Require(flags.X)` en el grupo de rutas chi → 404 problem+json si OFF (no 403: no revelar que existe).
2. **Handlers de eventos, jobs y consumidores**: primera línea evalúa el flag para el `tenantId` del mensaje; si OFF, registra en debug y termina con éxito (no reintentar).
3. **Frontend**: rutas protegidas con `<FeatureGate flag=...>` y `beforeLoad` del router; menú filtrado; módulo cargado con lazy import para no descargar código apagado.
4. **Datos**: apagar nunca borra ni modifica datos. Volver a encender restaura acceso.

## Evaluación
- Contexto obligatorio: `tenantId`, `plan`, `userId`, `roles`. Nunca evalúes sin tenant (excepto flags `ops.*` globales).
- Precedencia: `ops` kill switch global → override de tenant → plan → default.
- **Fail-closed** para no-núcleo; los módulos núcleo no tienen flag de apagado.
- El frontend NO es autoridad: el backend revalida siempre.
- Prohibido ramificar lógica de dominio por flag dentro de agregados. El flag se evalúa en `adapters/http` o `app` y se selecciona la implementación del puerto (interfaz) correspondiente.

## Código limpio con flags
- Máximo **un** punto de decisión por flag por capa (no `if enabled` dispersos). Usa interfaz + selección en `module.go`, decorador o middleware.
- Flags `release.*` tienen fecha `removeBy`; al pasar la fecha, `make check` falla (prueba sobre `registry.yaml`). Retirar = borrar flag + rama muerta + pruebas de la rama OFF, en un mismo PR.

## Pruebas obligatorias por flag
- Integración: endpoint con flag ON responde; con OFF responde 404.
- Consumidor: con OFF no produce efectos ni excepciones.
- Aislamiento: tenant A ON y tenant B OFF en el mismo test → B no ve nada.
- UI: componente/ruta oculto con OFF (Vitest) y E2E del módulo con ON.

## Auditoría
Todo cambio de flag por tenant se audita (quién, cuándo, valor anterior/nuevo, motivo) y emite `FeatureToggled` para invalidar cachés (Valkey pub/sub).
