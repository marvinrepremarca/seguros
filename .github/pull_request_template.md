## Qué y por qué

<!-- Flujo de negocio (1–10) y módulo dueño. Enlaza issue/ADR si aplica. -->

## Cómo se verificó

<!-- Pega el resultado real, no "funciona". -->
- [ ] `make check` en verde
- [ ] `pnpm -C src/frontend check` en verde (si toca frontend)
- [ ] `make test-integration` en verde (si toca BD, endpoints o eventos)

## Reglas no negociables (CLAUDE.md)

- [ ] **Fronteras de módulo**: solo se importa `internal/modules/<otro>/contracts`.
- [ ] **Multi-tenant**: tablas nuevas con `tenant_id` + RLS (`ENABLE` + `FORCE` + policy); consultas dentro de `db.WithTenantTx`; `tenantId` nunca viene del cliente.
- [ ] **Feature flags**: funcionalidad nueva protegida en endpoint, consumidores/jobs y UI; flag registrado en `registry.yaml`; pruebas ON/OFF.
- [ ] **Dinero**: solo `kernel.Money`; sin `float` en Go ni `number` en TS para montos; redondeo explícito.
- [ ] **Fechas**: vigencias con `kernel.Date`; instantes en UTC; sin `time.Now()` directo (`kernel.Clock`).
- [ ] **Seguridad**: deny-by-default (`x-permission`), sin secretos, sin PII en logs, campos sensibles cifrados.
- [ ] **Estados**: cambios solo por métodos del agregado con transiciones validadas.
- [ ] **Errores**: ninguno ignorado; envueltos con `%w`; sin `panic` en negocio.
- [ ] **Pruebas**: dominio + integración del endpoint, incluyendo flag ON/OFF y aislamiento entre tenants.
- [ ] **Reglas de negocio**: ninguna inventada; todas trazables a `flujos.md`, `DOMAIN.md` o `DECISIONS.md`.
- [ ] **Migraciones**: ninguna migración aplicada fue editada; cambio reversible o justificado.
- [ ] **Licencias**: dependencias nuevas dentro de ARCHITECTURE.md §9.

## Documentación

- [ ] ADR si cambia una decisión de arquitectura.
- [ ] `DOMAIN.md` actualizado si cambian invariantes o estados.
- [ ] `docs/ROADMAP.md` actualizado si cierra un entregable.
