---
name: adr
description: Registra una decisión de arquitectura (ADR) cuando se propone cambiar stack, fronteras de módulo, modelo de datos compartido, seguridad o estrategia de flags.
---
# Registrar un ADR

1. Lee `docs/adr/README.md` y toma el siguiente número.
2. Crea `docs/adr/NNNN-titulo-kebab.md` con la plantilla. Mínimo dos opciones reales con pros/contras, considerando: VPS/costo, seguridad, rendimiento, testeabilidad, impacto en multi-tenancy y flags.
3. Estado inicial `Propuesto`. **No implementes** la decisión hasta que el usuario la marque `Aceptado`.
4. Al aceptarse: actualiza la tabla del README, y si aplica `ARCHITECTURE.md`, `CLAUDE.md` (sección Stack) y las reglas en `.claude/rules/`. Si reemplaza otro ADR, marca el anterior `Reemplazado por NNNN`.
