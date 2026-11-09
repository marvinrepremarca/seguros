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

### Party (Parties)
- Raíz: `Party` (`Person` u `Organization`) → contactos; `Consent[]` es un registro aparte y append-only.
- Invariantes:
  - Documento: `Person` usa CC, CE o PPT; `Organization` usa NIT con dígito de verificación válido.
  - Una sola `Party` por `(tenant, tipo, número)`: una segunda da `parties.party.document_taken`.
- El número de documento es **Restringido**: cifrado, con blind index, ligado al ID de la `Party`; la UI y las respuestas solo muestran los últimos 4 caracteres, y ninguno en documentos de menos de 8. Revelarlo exige `parties.document.reveal` y se audita. Nunca va en URLs: la búsqueda por documento es `POST`.
- Asesor responsable (DECISIONS N11): lo fija el registro de un asesor y se traspasa con motivo; lo que registra Operaciones no tiene responsable hasta que se le asigne. `created_by` es solo auditoría.
- Alcance del asesor (DECISIONS N10, N11): un cliente fuera de su alcance responde 404, también en la búsqueda por documento, que queda auditada. Un documento repetido responde 409 sin datos y se audita.
- Habeas data: cada autorización o revocación es un `Consent` nuevo con fecha, finalidad y versión del texto. Consulta y rectificación están aquí; exporte y supresión llegan con `Compliance` (Fase 5). El consentimiento no bloquea la expedición.

### Catalog
- `Insurer`, `LineOfBusiness` (con su esquema de riesgo), `InsuranceProduct` (aseguradora + ramo), por tenant. Nombres únicos en su ámbito. Se desactivan, no se borran.
- Esquemas de riesgo: versionados, definidos en código (`auto`, `life`, `home`, `business`, `generic`, de los datos típicos de flujos.md Flujo 2), sin campos desconocidos. **No incluyen datos de salud:** la declaración de salud se gestiona como documento (Fase 5), porque sería dato Restringido dentro de JSONB sin cifrar.
- `CommissionAgreement(insurer, lineOfBusiness, product?, basis, percentage, vigencia)`: `percentage` entre 0 y 100; `basis` por defecto `paid_premium` (N3); no puede solaparse en vigencia con otro acuerdo del mismo ámbito.

### Policy (Policies)
- Raíz: `Policy` → `Coverage[]`, `InsuredItem[]`, `PolicyParty[]`, `Endorsement[]`.
- Invariantes:
  - `PolicyNumber` único por `(tenant, insurer)`.
  - `EffectiveTo > EffectiveFrom`.
  - Debe tener exactamente un `PolicyHolder` y ≥1 `Insured`.
  - Vigencia `[EffectiveFrom, EffectiveTo]`, ambos inclusive (DECISIONS N7).
  - `NetPremium` ≥ 0 y `Taxes` ≥ 0, en `Money` de la misma moneda (DECISIONS N8). Las sumas aseguradas de las coberturas usan esa moneda.
  - Antes de expedirse (`EnCotizacion`, `EnTramite`) los datos se pueden ajustar; después solo cambian por endoso.
  - No se emite (`Vigente`) si el `KycCheck` del tomador no está aprobado **cuando** `module.compliance` está ON (DECISIONS N4, sin excepciones). Si el flag no se puede evaluar, la emisión se bloquea con `policies.issue.compliance_unavailable` (ADR 0012).
  - Expedir exige el `PolicyNumber` que asignó la aseguradora.
  - Un endoso solo aplica a póliza `Vigente` o `EnMora`; su fecha efectiva está dentro de la vigencia. Estados: `Solicitado → Emitido | Rechazado` (DECISIONS N9). Al emitirse aplica variación de prima e impuestos (resultado ≥ 0), sumas aseguradas, partes y datos de bienes; debe quedar ≥1 `Insured`. Un endoso **no cambia el `PolicyHolder`** (eludiría el KYC de N4).
  - La cancelación registra lo que expidió la aseguradora: causa (solicitud del cliente, venta del bien, mora, revocación de la aseguradora, otra), fecha efectiva dentro de la vigencia y prima no devengada informada, entre 0 y la prima neta (DECISIONS T7). Un endoso `Solicitado` no impide cancelar, pero ya no se puede emitir.
  - El asesor se cambia en cualquier estado no terminal (DECISIONS N11), sin cambiar el estado; queda en `policy_agent_history` con fecha y motivo. Una póliza terminada conserva el suyo.
  - La renovación crea una **nueva** `Policy` con `RenewedFromPolicyId`, que puede expedirse antes de que venza la anterior, con vigencia futura (DECISIONS N17). La anterior no cambia al expedirla: sigue `Vigente` y, al vencer, pasa automáticamente a `Vencida → Renovada`.
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

- Una póliza en `EnMora` no pasa a `Vencida` al terminar la vigencia: la máquina de estados solo permite `Vigente` o `Cancelada` desde `EnMora`.
- Eventos: `PolicyIssued`, `PolicyEndorsed(netPremiumDelta, taxesDelta)`, `PolicyCancelled(effectiveDate, unearnedPremium)`, `PolicyExpired`, `PolicyRenewed`, `PolicyFellIntoArrears`, `PolicyReinstated`.

### PaymentPlan (Billing)
- Se crea al consumir `PolicyIssued` (si `module.billing` ON), con las cuotas que dicta la forma de pago; Operaciones puede ajustar fechas y número de cuotas (DECISIONS N15).
- Σ `Installment.Amount` = prima total (+ ajustes de endosos). La diferencia por redondeo se asigna a la **última** cuota.
- Un `Payment` se aplica a la cuota vencida más antigua primero (FIFO); sobrante a la siguiente; sobrante final = saldo a favor (DECISIONS N6).
- Cuota vencida + días configurables (default 30, por tenant/aseguradora) → evento `InstallmentOverdue` → Policies marca `EnMora` (DECISIONS N2).
- Un endoso reparte su variación entre las cuotas sin pagar (DECISIONS N18). Una póliza sale de mora cuando no le quedan cuotas fuera de los días de gracia (N19). Una cancelación anula las cuotas pendientes y registra la devolución informada, sin tocar los pagos (N20).
- **El cliente siempre paga directo a la aseguradora** (DECISIONS N1). Billing registra pagos **informados** (soporte del cliente o reporte de la aseguradora) y gestiona cartera y cobro; la agencia nunca custodia dinero de primas.
- Pagos son **inmutables**: corrección = reverso + nuevo pago. Comandos de pago requieren idempotency key.

### Claim (Claims)
- Requiere póliza existente; si la fecha de ocurrencia está fuera de vigencia, la póliza no está en vigor o está en mora, se registra igual pero con **alerta**, no bloqueo (la aseguradora decide). [VALIDAR]
- Estados: `Reportado → AvisadoAseguradora → EnDocumentacion → EnEvaluacion → Aprobado | Objetado → Pagado → Cerrado` (`Objetado → EnEvaluacion` por reconsideración; `Objetado → Cerrado`).
- `PaidAmount ≤ ClaimedAmount` salvo override auditado de `Manager` (DECISIONS N30). La agencia **no aprueba** siniestros: el estado `Aprobado/Objetado` solo se registra con soporte de la aseguradora.
- Datos de salud: un campo opcional cifrado, acceso solo a roles `ClaimsAnalyst`, `Manager` con auditoría de lectura.
- `SalesAgent` reporta y consulta el estado de los siniestros de sus pólizas (DECISIONS N29).

### Commissions
- `ExpectedCommission` = base × % de `CommissionAgreement(insurer, lineOfBusiness, product, vigencia)`.
- Base según `CommissionAgreement.basis`: default **prima pagada** (consume `PaymentApplied`); alternativa prima emitida (DECISIONS N3). En ambos casos solo cuenta la prima neta: cada pago aporta su parte proporcional a neta/total (DECISIONS N14).
- En un traspaso, lo causado antes de `PolicyAgentChanged.EffectiveAt` sigue con el asesor original y lo posterior es del nuevo (DECISIONS N16).
- Cancelación con prima no devengada (valor informado por la aseguradora) → `CommissionClawback` negativo.
- Conciliación: `ExpectedCommission` vs líneas de `InsurerStatement`; diferencias > tolerancia (default COP 1.000) generan `ReconciliationDiscrepancy`.
- `AgentCommission` = comisión **conciliada** × % del asesor para el ramo (fallback: % general del asesor; sin tasa → 0 % + alerta) (DECISIONS N5).

### Lead / Quote
- `Lead` estados y transiciones: DECISIONS N31; `Perdido` exige motivo y `Seguimiento`, fecha. Al pasar a `EnCotizacion` se enlaza una `Party` (N27).
- `QuoteOffer` tiene `ValidUntil`; una oferta vencida no puede aceptarse.
- Aceptar oferta → evento `QuoteAccepted` → Policies crea `Policy` en `EnTramite` (DECISIONS N28). La oferta vale hasta `ValidUntil` inclusive.

### Documents, Notifications, Reporting, Integrations
- `Document` pertenece a un cliente, póliza o siniestro; sus versiones son inmutables. Una versión solo se descarga si el antivirus la dejó limpia; pendiente o infectada, nunca. Un documento sensible es invisible sin `documents.document.sensitive`. Nada se borra automáticamente (DECISIONS N34).
- `Message` es un aviso al tomador: `pending → sent | skipped | failed`; `failed → pending` por reintento manual. Una clave de negocio repetida no genera otro aviso (DECISIONS N35).
- Reporting solo lee vistas `security_invoker`; cada sección existe si el módulo que la alimenta está encendido (DECISIONS N36).
- Integrations habla con cada aseguradora por el puerto `Gateway`, tras su kill switch y un circuit breaker (DECISIONS N37).

## 3. Mapa de eventos entre módulos

| Evento (Contracts) | Publica | Consumen |
|---|---|---|
| `QuoteAccepted` | Quoting | Policies, Crm |
| `PolicyPartiesChanged` | Policies | Parties (alcance del asesor) |
| `PolicyAgentChanged` | Policies | Commissions |
| `PortfolioTransferRequested` | Policies | Policies, Parties, Crm |
| `PolicyIssued` | Policies | Billing, Commissions, Renewals, Notifications, Reporting |
| `PolicyEndorsed` | Policies | Billing, Commissions |
| `PolicyCancelled` | Policies | Billing (anula cuotas pendientes, registra devolución informada), Commissions (clawback), Renewals |
| `PolicyExpired` | Policies (job diario) | Renewals |
| `PolicyRenewed` | Policies | Renewals, Reporting |
| `PolicyFellIntoArrears` / `PolicyReinstated` | Policies | Notifications, Reporting |
| `PolicyExpiring(daysLeft)` | Renewals (job) | Notifications, Crm |
| `InstallmentOverdue` | Billing | Policies, Notifications |
| `InstallmentDue(daysLeft)` | Billing (job, T1) | Notifications |
| `PaymentApplied` / `PaymentReversed` | Billing | Commissions, Policies (reinstate, solo `PaymentApplied`) |
| `RenewalLost` | Renewals | Policies (`Vencida → NoRenovada`) |
| `ClaimReported` / `ClaimStatusChanged` | Claims | Notifications, Reporting |
| `KycCheckCompleted` | Compliance | Policies |

En la Fase 2 solo emiten `PolicyIssued`, `PolicyEndorsed`, `PolicyCancelled` y `PolicyExpired`: los demás eventos de póliza llegan con quien dispara su transición (Billing y Renewals, Fase 3). Los métodos del agregado ya existen y están probados.

Todo consumidor verifica el flag del módulo para el tenant del evento y es **idempotente** (clave = `EventId`).

## 4. Roles y permisos base

| Rol | Permisos clave |
|---|---|
| `TenantAdmin` | Configuración, usuarios, flags del tenant |
| `Manager` | Todo lectura; aprobación de overrides (incluye pago mayor a lo reclamado, N30); asignación de prospectos; comisiones; acuerdos de comisión; traspaso de cartera |
| `SalesAgent` | Leads y cotizaciones propias; pólizas propias (lectura); reporta y consulta siniestros de sus pólizas (N29); registra clientes y actualiza los suyos, sin cambiar el documento (DECISIONS N10) |
| `Operations` | Expedición, endosos, renovaciones, cancelaciones; traspaso de cartera |
| `ClaimsAnalyst` | Siniestros (incluye datos de salud) |
| `Collections` | Cuotas, pagos, gestión de cobro |
| `Accounting` | Comisiones, conciliación, facturación |
| `ComplianceOfficer` | SARLAFT, PQR, auditoría, exportes de habeas data |

Permisos finos con formato `<modulo>.<recurso>.<accion>` (ej. `billing.payment.reverse`). Un usuario puede tener varios roles (agencias pequeñas).

## 5. KPIs (Reporting)
Primas emitidas, conversión cotización→póliza, retención (renovadas/por vencer), cartera vencida por edades (0-30, 31-60, 61-90, >90), comisiones causadas vs recibidas, tiempo de expedición, tiempo de atención de siniestros, pólizas por cliente, siniestralidad.
