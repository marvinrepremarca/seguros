# Modelo de dominio — lenguaje ubicuo, agregados e invariantes

> Fuente de negocio: [flujos.md](../../flujos.md). Este documento traduce los flujos a reglas implementables. Los parámetros de negocio decididos están en [DECISIONS.md](DECISIONS.md) (prevalece sobre este documento). Toda regla aún marcada **[VALIDAR]** se implementa configurable por tenant con el default indicado.

## 1. Glosario (español → código)

| Negocio | Código | Módulo dueño |
|---|---|---|
| Agencia (cliente SaaS) | `Tenant` | Platform.Tenancy |
| Cliente / Tercero | `Party` (`Person` / `Organization`) | Parties |
| Tomador / Asegurado / Beneficiario | `PolicyHolder` / `Insured` / `Beneficiary` (roles de `Party` en una póliza) | Policies |
| Aseguradora | `Insurer` | Catalog |
| Ramo | `LineOfBusiness` | Catalog |
| Producto | `InsuranceProduct` | Catalog |
| Prospecto / Lead | `Lead` | Crm |
| Cotización / Propuesta | `QuoteRequest` / `QuoteOffer` | Quoting |
| Póliza | `Policy` | Policies |
| Cobertura / amparo | `Coverage` | Policies |
| Bien o persona asegurada | `InsuredItem` (datos de riesgo en JSONB tipado por ramo) | Policies |
| Endoso | `Endorsement` | Policies |
| Renovación | `Renewal` | Renewals |
| Cancelación / revocación | `Cancellation` | Policies |
| Plan de pagos / Cuota / Pago | `PaymentPlan` / `Installment` / `Payment` | Billing |
| Cartera | `Receivables` (proyección) | Billing |
| Siniestro | `Claim` | Claims |
| Comisión pactada | `CommissionAgreement` | Catalog |
| Comisión esperada / liquidada | `ExpectedCommission` / `InsurerStatement` | Commissions |
| Comisión interna (asesor) | `AgentCommission` | Commissions |
| Asesor | `Agent` (rol de `User`) | Platform.Identity |
| Verificación SARLAFT | `KycCheck` | Compliance |
| Queja / PQR | `Complaint` | Compliance |

## 2. Agregados e invariantes

### Policy (Policies)
- Raíz: `Policy` → `Coverage[]`, `InsuredItem[]`, `PolicyParty[]`, `Endorsement[]`.
- Invariantes:
  - `PolicyNumber` único por `(tenant, insurer)`.
  - `EffectiveTo > EffectiveFrom`.
  - Debe tener exactamente un `PolicyHolder` y ≥1 `Insured`.
  - `Premium` ≥ 0, en `Money`.
  - No se emite (`Vigente`) si el `KycCheck` del tomador no está aprobado **cuando** `module.compliance` está ON (DECISIONS N4, sin excepciones).
  - Un endoso solo aplica a póliza `Vigente` o `EnMora`; su fecha efectiva está dentro de la vigencia.
  - La renovación crea una **nueva** `Policy` con `RenewedFromPolicyId`; nunca muta la anterior.
- Máquina de estados:

```
EnCotizacion → EnTramite (cliente acepta)
EnTramite → Vigente (aseguradora expide) | Rechazada
Vigente → EnMora (cuota vencida) | Cancelada (solicitud/revocación) | Vencida (fin de vigencia)
EnMora → Vigente (paga) | Cancelada (no paga)
Vencida → Renovada | NoRenovada
Estados terminales: Rechazada, Cancelada, Renovada, NoRenovada
```
Cualquier otra transición devuelve el error de dominio `ErrInvalidPolicyTransition` y no persiste.

- Eventos: `PolicyIssued`, `PolicyEndorsed(premiumDelta)`, `PolicyCancelled(effectiveDate, unearnedPremium)`, `PolicyExpired`, `PolicyRenewed`, `PolicyFellIntoArrears`, `PolicyReinstated`.

### PaymentPlan (Billing)
- Se crea al consumir `PolicyIssued` (si `module.billing` ON).
- Σ `Installment.Amount` = prima total (+ ajustes de endosos). La diferencia por redondeo se asigna a la **última** cuota.
- Un `Payment` se aplica a la cuota vencida más antigua primero (FIFO); sobrante a la siguiente; sobrante final = saldo a favor (DECISIONS N6).
- Cuota vencida + días configurables (default 30, por tenant/aseguradora) → evento `InstallmentOverdue` → Policies marca `EnMora` (DECISIONS N2).
- **El cliente siempre paga directo a la aseguradora** (DECISIONS N1). Billing registra pagos **informados** (soporte del cliente o reporte de la aseguradora) y gestiona cartera y cobro; la agencia nunca custodia dinero de primas.
- Pagos son **inmutables**: corrección = reverso + nuevo pago. Comandos de pago requieren idempotency key.

### Claim (Claims)
- Requiere póliza existente; si la fecha de ocurrencia está fuera de vigencia o la póliza está en mora, se registra igual pero con **alerta**, no bloqueo (la aseguradora decide). [VALIDAR]
- Estados: `Reportado → AvisadoAseguradora → EnDocumentacion → EnEvaluacion → Aprobado | Objetado → Pagado → Cerrado` (`Objetado → EnEvaluacion` por reconsideración; `Objetado → Cerrado`).
- `PaidAmount ≤ ClaimedAmount` salvo override auditado. La agencia **no aprueba** siniestros: el estado `Aprobado/Objetado` solo se registra con soporte de la aseguradora.
- Datos de salud: campos cifrados, acceso solo a roles `ClaimsAnalyst`, `Manager` con auditoría de lectura.

### Commissions
- `ExpectedCommission` = base × % de `CommissionAgreement(insurer, lineOfBusiness, product, vigencia)`.
- Base según `CommissionAgreement.basis`: default **prima pagada** (consume `PaymentApplied`); alternativa prima emitida (DECISIONS N3).
- Cancelación con prima no devengada (valor informado por la aseguradora) → `CommissionClawback` negativo.
- Conciliación: `ExpectedCommission` vs líneas de `InsurerStatement`; diferencias > tolerancia (default COP 1.000) generan `ReconciliationDiscrepancy`.
- `AgentCommission` = comisión **conciliada** × % del asesor para el ramo (fallback: % general del asesor; sin tasa → 0 % + alerta) (DECISIONS N5).

### Lead / Quote
- `Lead` estados: `Nuevo → Contactado → Interesado → EnCotizacion → Ganado | Perdido(motivo obligatorio) | Seguimiento(fecha)`.
- `QuoteOffer` tiene `ValidUntil`; una oferta vencida no puede aceptarse.
- Aceptar oferta → evento `QuoteAccepted` → Policies crea `Policy` en `EnTramite`.

## 3. Mapa de eventos entre módulos

| Evento (Contracts) | Publica | Consumen |
|---|---|---|
| `QuoteAccepted` | Quoting | Policies, Crm |
| `PolicyIssued` | Policies | Billing, Commissions, Renewals, Notifications, Reporting |
| `PolicyEndorsed` | Policies | Billing, Commissions |
| `PolicyCancelled` | Policies | Billing (anula cuotas pendientes, registra devolución informada), Commissions (clawback), Renewals |
| `PolicyExpiring(daysLeft)` | Renewals (job) | Notifications, Crm |
| `InstallmentOverdue` | Billing | Policies, Notifications |
| `PaymentApplied` | Billing | Commissions, Policies (reinstate) |
| `ClaimReported` / `ClaimStatusChanged` | Claims | Notifications, Reporting |
| `KycCheckCompleted` | Compliance | Policies |

Todo consumidor verifica el flag del módulo para el tenant del evento y es **idempotente** (clave = `EventId`).

## 4. Roles y permisos base

| Rol | Permisos clave |
|---|---|
| `TenantAdmin` | Configuración, usuarios, flags del tenant |
| `Manager` | Todo lectura; aprobación de overrides; comisiones |
| `SalesAgent` | Leads y cotizaciones propias; pólizas propias (lectura) |
| `Operations` | Expedición, endosos, renovaciones, cancelaciones |
| `ClaimsAnalyst` | Siniestros (incluye datos de salud) |
| `Collections` | Cuotas, pagos, gestión de cobro |
| `Accounting` | Comisiones, conciliación, facturación |
| `ComplianceOfficer` | SARLAFT, PQR, auditoría, exportes de habeas data |

Permisos finos con formato `<modulo>.<recurso>.<accion>` (ej. `billing.payment.reverse`). Un usuario puede tener varios roles (agencias pequeñas).

## 5. KPIs (Reporting)
Primas emitidas, conversión cotización→póliza, retención (renovadas/por vencer), cartera vencida por edades (0-30, 31-60, 61-90, >90), comisiones causadas vs recibidas, tiempo de expedición, tiempo de atención de siniestros, pólizas por cliente, siniestralidad.
