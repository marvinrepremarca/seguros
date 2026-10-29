---
name: security-check
description: Auditoría de seguridad del cambio actual contra OWASP Top 10:2025, multi-tenancy, feature flags y datos sensibles de seguros. Úsala antes de cerrar cualquier cambio que toque autenticación, autorización, dinero, PII/salud, archivos o integraciones.
---
# Revisión de seguridad del cambio

1. Obtén el diff (`git diff` contra la rama base) y lista archivos por módulo/capa.
2. Revisa cada punto y reporta **hallazgo, archivo:línea, severidad (Crítica/Alta/Media/Baja), corrección propuesta**:

| Área | Verificar |
|---|---|
| Acceso (A01) | Operación con `x-permission` y `authz.Require`; verificación de propiedad; sin `tenantId` del cliente; 404 a otro tenant |
| Tenancy | Tablas nuevas con `tenant_id` + RLS; todo acceso vía `db.WithTenantTx`; claves de caché con tenant |
| Flags | Flag aplicado en API, consumidores/jobs y UI; fail-closed |
| Inyección (A05) | Sin `fmt.Sprintf` en SQL; validación completa; `MaxBytesReader`; uploads validados; `html/template` |
| Cripto (A04) | Campos Restringidos cifrados con AES-GCM; `crypto/rand`; sin secretos literales |
| Config (A02) | Headers, CORS, timeouts del `http.Server`, `pprof`/docs deshabilitados en prod |
| Supply chain (A03) | Dependencias nuevas justificadas, licencia permitida (ARCHITECTURE §9), `govulncheck` limpio |
| Concurrencia | Sin data races (`-race`), goroutines con dueño, `ctx` propagado |
| Integridad (A08) | Idempotencia en pagos; webhooks firmados; auditoría |
| Logging (A09) | Sin PII/tokens en logs; eventos de seguridad auditados |
| Errores (A10) | problem+json genérico; ningún `error` ignorado; sin `panic` en negocio; sin fail-open |
| Dinero | `kernel.Money`, redondeo explícito, sin `float64`/`number` |
| Habeas data | Consentimiento verificado antes de usar datos para nuevas finalidades |

3. Si hay hallazgos Críticos/Altos: el cambio **no** está listo. Propón el arreglo; no lo declares terminado.
4. Para revisión profunda, delega en el subagente `security-auditor`.
