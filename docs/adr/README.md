# Architecture Decision Records

Toda decisión que cambie stack, fronteras de módulo, modelo de datos compartido, seguridad o estrategia de flags requiere un ADR **antes** de implementarse. Usa la skill `/adr`.

## Registro

| # | Título | Estado |
|---|---|---|
| 0001 | Monolito modular con hexagonal por módulo | Aceptado |
| 0002 | Go 1.25 (chi, pgx, sqlc, OpenAPI-first) como backend — reemplaza propuesta inicial .NET 10 por política de cero licencias de pago y menor consumo en VPS | Aceptado |
| 0003 | PostgreSQL único con RLS para multi-tenancy (modelo pool) | Aceptado |
| 0004 | JSONB para datos de riesgo por ramo en lugar de NoSQL | Aceptado |
| 0005 | Watermill + outbox en PostgreSQL y River OSS para jobs; RabbitMQ diferido a fase 2 | Aceptado |
| 0006 | Keycloak + BFF; sin tokens en el navegador | Aceptado |
| 0007 | OpenFeature + proveedor propio para entitlements y flags por tenant | Aceptado |
| 0008 | Valkey en lugar de Redis (licencia) | Aceptado |
| 0009 | Docker Compose + Traefik en VPS; k3s en fase 2 | Aceptado |
| 0010 | Política de licencias: solo MIT/BSD/Apache/MPL/PostgreSQL; sin ediciones Pro/Enterprise; SeaweedFS en vez de MinIO; OpenBao en vez de Vault; Podman/Rancher Desktop en vez de Docker Desktop | Aceptado |

Las justificaciones de 0001–0009 están en [ARCHITECTURE.md](../architecture/ARCHITECTURE.md); escribe archivos individuales cuando alguna se revise.

## Plantilla (`docs/adr/NNNN-titulo-kebab.md`)

```markdown
# NNNN — Título
- Estado: Propuesto | Aceptado | Reemplazado por NNNN | Obsoleto
- Fecha: YYYY-MM-DD
- Decisores:

## Contexto
Problema, fuerzas, restricciones (rendimiento, seguridad, costo, VPS, regulación).

## Opciones consideradas
1. ... (pros / contras)
2. ...

## Decisión
Qué se elige y por qué.

## Consecuencias
Positivas, negativas, riesgos, qué se debe actualizar (CLAUDE.md, rules, DOMAIN.md).
```
