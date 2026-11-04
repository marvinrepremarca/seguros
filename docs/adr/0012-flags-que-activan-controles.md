# 0012 — Flags que activan controles regulatorios: bloquear si no se pueden evaluar
- Estado: Aceptado
- Fecha: 2026-11-03
- Decisores: dueño del producto (Marvin Delgado), revisión de seguridad de la Fase 1 (hallazgo M1)

## Contexto
La regla general de flags es *fail-closed*: si un flag no se puede evaluar, se considera apagado. Es segura cuando el flag **da acceso** a una funcionalidad. Es peligrosa cuando el flag **activa un control**: `module.compliance` encendido exige un `KycCheck` aprobado para expedir (DECISIONS N4, SARLAFT). Si una falla de Valkey o de la BD lo evaluara como apagado, se expedirían pólizas sin verificación de conocimiento del cliente, sin rastro de que el control se omitió.

`Checker.IsEnabled` no distingue "apagado" de "no se pudo evaluar": devuelve `false` en ambos casos.

## Opciones consideradas
1. **Mantener fail-closed.** Simple, pero una falla de infraestructura desactiva un control regulatorio en silencio.
2. **Exigir KYC siempre que falle la evaluación.** No se puede cumplir: el KYC se consulta con la misma infraestructura, y a un tenant sin el módulo se le pediría un control que no contrató.
3. **Tres resultados en la decisión (exigido / no exigido / indeterminado) y bloquear lo indeterminado.** La operación falla de forma visible y reintentable.

## Decisión
Opción 3, para todo flag marcado `control: true` en `registry.yaml` (hoy solo `module.compliance`):

1. **El código que aplica un control usa `Evaluator.Resolve`, nunca `IsEnabled`.** Es indeterminado, y la operación se bloquea, si `Resolve` devuelve error, si el origen es `SourceError` o si no hay plan en el contexto (sin plan, el flag caería al default apagado).
2. **Expedir con la evaluación indeterminada falla con `policies.issue.compliance_unavailable` (HTTP 503)**, sin cambios en la póliza. Se registra una advertencia en el log y se puede reintentar.
3. **La evaluación ocurre antes de abrir la transacción de expedición**, y la decisión llega al agregado como un valor (`KycClearance`); `Policy.Issue` no conoce los flags. El valor cero se rechaza (deny-by-default).
4. **El último valor conocido sigue siendo válido** (ARCHITECTURE.md §7): si la lectura falla pero hay valor en la L1, se usa. Expedir exige además una transacción en la misma BD, así que una caída de la BD bloquea la emisión de todas formas.
5. **`platformctl flag kill` sobre un flag de control exige `--confirm-control <clave>`**, con la clave repetida. Apagar el flag desactiva el control en todos los tenants, y es un cambio deliberado que queda en `global_switch_history`.

Mientras no exista `Compliance` (Fase 5), un tenant con `module.compliance` encendido no tiene forma de aprobar un `KycCheck`, así que **no puede expedir** (`policies.issue.kyc_not_approved`). N4 no admite excepciones y es la consecuencia correcta: el módulo solo está en el plan Empresarial, y ningún tenant real opera antes de la Fase 6.

## Consecuencias
- Positivas: una falla de infraestructura no omite un control regulatorio; el comportamiento queda probado por tipo de falla.
- Negativas: una falla de Valkey y de la BD a la vez, sin L1, detiene la emisión de todos los tenants, incluidos los que no tienen el módulo, porque no se puede saber cuáles lo tienen.
- Un endoso no cambia el tomador: el nuevo no pasaría este control (`policies.endorsement.holder_change_not_allowed`). Si negocio lo requiere, debe evaluar `KycGate` para el nuevo tomador.
- Un flag de control no admite `requires`: apagar la dependencia desactivaría el control sin confirmación.
- Requisito para Compliance (Fase 5): el `KycCheck` se liga al documento vigente del tercero y se invalida al rectificarlo. Si solo dependiera del ID, cambiar el documento conservaría el KYC de otra persona.
- Riesgo abierto: `flag set --value off` y un cambio de plan también apagan el control para **un** tenant. Son decisiones comerciales auditadas y no piden confirmación adicional; revisarlo cuando exista `Compliance`.
- Actualizado: `registry.yaml` (campo `control`), `.claude/rules/feature-flags.md`, DOMAIN.md, ROADMAP.md.
