# 0013 — Reporting en vivo, archivos sensibles por la API y dependencias de la Fase 5
- Estado: Aceptado
- Fecha: 2026-11-05
- Decisores: dueño del producto (aceptado 2026-11-09), a partir de la revisión de arquitectura y seguridad de la Fase 5

## Contexto
ARCHITECTURE §4.3 pide que Reporting lea vistas materializadas o réplica, y las reglas piden servir archivos por URL prefirmada. PostgreSQL no aplica RLS sobre vistas materializadas, y una URL prefirmada no puede entregar un archivo cifrado por la aplicación.

## Decisión
1. **Reporting lee vistas `security_invoker` en vivo**, su única frontera con otros esquemas, y migra de último. Cambiar una columna usada exige una migración de `reporting` que suelte y recree la vista. Pasar a réplica cuando k6 (Fase 6) muestre impacto en lo transaccional.
2. **Los archivos sensibles se cifran en la aplicación (envelope por tenant) y se sirven por la API**, que autoriza y audita al entregar. Los demás siguen con URL prefirmada de corta vida.
3. **Dependencias:** `minio-go` (cliente, Apache-2.0; ADR 0011 descarta el servidor MinIO, no este cliente), ClamAV (GPL, servicio sin modificar) y Mailpit (solo desarrollo).

## Consecuencias
- Reportes de periodos largos cargan la base principal; el rango se limita a 1.830 días.
- Un archivo sensible pasa por la memoria de la API al cargar y descargar, por eso el tope de tamaño y el límite de frecuencia.
