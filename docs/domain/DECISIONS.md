# Decisiones de negocio y producto

> Fuente de verdad para parámetros de negocio. Prevalece sobre los supuestos [VALIDAR] de DOMAIN.md.
> **Regla para agentes:** todo valor marcado *editable* se implementa como **parámetro configurable por tenant** (y por aseguradora/ramo cuando se indique) con el valor por defecto aquí definido; nunca como constante en código.
> Estados: ✅ Decidido (por el dueño del producto) · 🔧 Por defecto técnico (razonable, cambiable sin ADR) · [PENDIENTE] requiere respuesta.

## 1. Decisiones de negocio

| # | Tema | Decisión | Estado | Impacto en el sistema |
|---|---|---|---|---|
| N1 | Recaudo de primas | **Siempre pago directo a la aseguradora.** La agencia nunca recibe ni transfiere dinero de primas. | ✅ 2026-10-29 | `Billing` registra pagos informados por el cliente o la aseguradora (con soporte), gestiona cartera y cobro. **No existe** modelo `CollectedByAgency` ni cuentas de dinero de terceros. Devoluciones por cancelación las paga la aseguradora; la agencia solo registra el valor. |
| N2 | Días para marcar mora | **30 días** tras el vencimiento de la cuota. | ✅ editable por tenant y aseguradora | Job diario River evalúa cuotas vencidas; emite `InstallmentOverdue` al cumplir el plazo. Recordatorios antes del vencimiento: ver T1. |
| N3 | Base de comisión | **Prima efectivamente pagada.** | ✅ editable por acuerdo de comisión (aseguradora + ramo [+ producto]) a `prima emitida` | `CommissionAgreement.basis ∈ {paid_premium, written_premium}`, default `paid_premium`. Con `paid_premium`, se consume `PaymentApplied`; con `written_premium`, `PolicyIssued`/`PolicyEndorsed`. |
| N4 | SARLAFT al expedir | **Bloquear** el paso a `Vigente` si el `KycCheck` del tomador no está aprobado (cuando `module.compliance` está ON). | ✅ | Invariante en `Policy.Issue`. Error `policies.issue.kyc_not_approved`. Sin excepciones manuales. |
| N5 | Comisión interna de asesores | **Porcentaje por asesor y ramo**, sobre la comisión **conciliada** de la agencia. | ✅ editable | Tabla `AgentCommissionRate(agent, lineOfBusiness, pct, vigencia)`; si no hay tasa para el ramo, usa la tasa general del asesor; si no hay ninguna, 0 % y alerta. |
| N6 | Aplicación de pagos | **A la cuota vencida más antigua primero (FIFO)**; el sobrante pasa a la siguiente; sobrante final queda como saldo a favor. | ✅ | Sin reasignación manual en v1. Un error se corrige con reverso + nuevo pago (pagos inmutables). |
| N7 | Fin de vigencia | **`EffectiveTo` es el último día cubierto.** Vigencia = `[EffectiveFrom, EffectiveTo]`, ambos inclusive. | ✅ 2026-11-03 | Endosos y cancelaciones valen hasta `EffectiveTo` inclusive. El job diario marca `Vencida` a partir de `EffectiveTo + 1` (zona `America/Bogota`). |
| N8 | Valores de prima | **Prima neta e impuestos/gastos por separado**, tal como los informa la aseguradora; el sistema no los calcula. Total = neta + impuestos. | ✅ 2026-11-03 | `Policy.netPremium` y `Policy.taxes`. Los eventos llevan ambos. Las cuotas suman el total; la base de comisión la define N14. |
| N9 | Servicio postventa (Flujo 5) | **Endoso con estados:** `Solicitado → Emitido \| Rechazado`. Prima, sumas aseguradas, partes y bienes cambian solo al emitirse. | ✅ 2026-11-03 | Certificados y duplicados quedan fuera hasta `Documents` (Fase 5). |
| N10 | Clientes del asesor | **Un `SalesAgent` solo alcanza a sus clientes.** Si un documento ya existe, recibe el aviso sin datos del cliente y debe pedir acceso. No corrige el número de documento. | ✅ 2026-11-04 | Permisos `parties.party.read-own`, `parties.party.update-own` y `parties.document.rectify` (Operations, ComplianceOfficer). "Sus clientes" lo define N11. |
| N11 | Cartera y rotación | **La cartera es de la agencia: el acceso sale de la asignación vigente, no del historial.** Su cliente es aquel del que es **asesor responsable**, o parte de una póliza suya no terminada. Registrar un cliente lo vuelve responsable inicial, y se puede traspasar. Las pólizas rechazadas, canceladas, renovadas o no renovadas dejan de dar acceso, pero el asesor las sigue viendo como historial. El asesor de una póliza no terminada se cambia por póliza o en bloque, solo Manager u Operations, con motivo auditado. | ✅ 2026-11-04 | `parties.responsible_agent_id`, `policies.policy_agent_history` (append-only), permisos `parties.party.assign` y `policies.policy.assign`. Solo se asigna a usuarios activos con rol `SalesAgent`, y aparecen tras su primer ingreso. El traspaso en bloque y el acceso por póliza se procesan en el worker: normalmente segundos, sin plazo si está caído o el evento queda en `dead_letters` (ver ROADMAP). Comisiones en un traspaso: ver N16. |
| N12 | Motivos de traspaso | **El motivo de un cambio de asesor sale de un catálogo:** retiro del asesor, cambio de cargo, ausencia prolongada, redistribución de cartera, solicitud del cliente, cambio de sucursal o zona, corrección de asignación y otro. La nota es opcional, salvo en "otro". | ✅ 2026-11-04 | `kernel.TransferReason`. La nota es texto libre: vive solo en el historial y en `portfolio_transfer_requests`, nunca en eventos ni bitácora. Un traspaso en bloque no mueve lo que se le asignó al origen después de pedirlo. |
| N13 | MFA de Operaciones | **`Operations` exige MFA**, porque revela documentos de identidad (dato Restringido). | 🔧 2026-11-04 | `authz.MFARoles`. Por defecto técnico: cambiable sin ADR. |
| N21 | MFA de Cartera | **`Collections` exige MFA**, porque registra y reversa pagos, que sacan pólizas de mora y causan comisión (ARCHITECTURE §8 A07). | 🔧 2026-11-05 | `authz.MFARoles`. Por defecto técnico: cambiable sin ADR. |
| N22 | Antigüedad de un pago | **Vale la fecha del soporte:** no futura y no anterior a 60 días antes del primer vencimiento del plan (el inicio de la gestión de renovación, T2). | 🔧 editable por agencia 2026-11-05 | `billing.payment_settings`. El dueño del producto pidió usar lo más lógico, sin validación externa. |
| N23 | Ajuste de calendario | **Exige la referencia del soporte de la aseguradora.** Si un ajuste o un endoso deja la póliza sin cuotas fuera de gracia, sale de mora. | 🔧 2026-11-05 | Evento `ArrearsCleared`. |
| N24 | Salida de mora sin Billing | **Operaciones registra manualmente que la aseguradora confirmó el pago:** `EnMora → Vigente`, auditado. También sirve con Billing encendido. | 🔧 2026-11-05 | `POST .../reinstate`. |
| N25 | Datos de una renovación | **Puede cambiar de aseguradora, pero conserva tomador y ramo;** con otro tomador es una póliza nueva. | 🔧 editable por agencia 2026-11-05 | `policies.renewal.holder_changed`, `policies.renewal.line_changed`. Flujo 6 prevé recotizar con otras aseguradoras. |
| N26 | Tolerancia de conciliación | **Máximo COP 400.000 por póliza** (referencia de mercado: USD 50–100 por línea, fuentes de proveedores). En otra moneda, coincidencia exacta salvo que la agencia fije otra, con máximo de 100 unidades. | 🔧 editable por agencia y moneda 2026-11-05 | `commissions.tolerances`. El máximo en COP es constante de plataforma. Si la aseguradora liquida en COP una póliza en moneda extranjera (la Superfinanciera indicó que la comisión no se paga en esa moneda), la línea queda como diferencia para revisión manual. |
| N14 | Valor base de comisión | **Solo prima neta.** Comisión = prima neta × %; los impuestos y gastos no comisionan. | ✅ 2026-11-05 | Con `paid_premium`, cada pago cuenta por su parte de prima neta, en proporción a neta/total de la póliza. |
| N15 | Generación de cuotas | **Según la forma de pago, ajustable.** Pago único: 1 cuota. Mensual o trimestral: una por periodo de vigencia; la primera vence en `EffectiveFrom`. Débito automático: mensual. Financiación de primas: 1 cuota, porque la financiera paga la prima completa. | ✅ 2026-11-05 | `Billing` genera el plan al consumir `PolicyIssued`; Operaciones puede ajustar fechas y número de cuotas según lo que informe la aseguradora, manteniendo Σ cuotas = total. |
| N16 | Comisión interna en un traspaso | **Por fecha de causación.** Lo causado antes del traspaso sigue con el asesor original; lo causado después es del nuevo. | ✅ 2026-11-05 | `Commissions` consume `PolicyAgentChanged.EffectiveAt`. Con `paid_premium`, la fecha que cuenta es la del pago. |
| N18 | Endoso y plan de pagos | **La variación se reparte entre las cuotas sin pagar**; el residuo va a la última. Sin cuotas pendientes, un aumento crea una cuota con vencimiento en la fecha efectiva del endoso y una disminución queda como saldo a favor. | ✅ 2026-11-05 | `Billing` consume `PolicyEndorsed`. |
| N19 | Salida de mora | **La póliza sale de mora cuando no le quedan cuotas que hayan superado los días de gracia** de N2, aunque tenga otras vencidas dentro de ese plazo. | ✅ 2026-11-05 | `Billing` lo informa en `PaymentApplied`; `Policies` pasa a `Vigente`. |
| N20 | Cancelación y cartera | **Las cuotas pendientes se anulan y salen de cartera; los pagos no se tocan.** La prima no devengada se registra como devolución informada: la paga la aseguradora al cliente y no se vuelve saldo a favor. | ✅ 2026-11-05 | `Billing` consume `PolicyCancelled`. |
| N27 | Prospecto y cliente | **El `Lead` vive solo con datos de contacto; al pasar a cotizar se enlaza una `Party`** existente del alcance del asesor o una recién registrada. Solo entonces se pide el documento. | ✅ 2026-11-05 | `Lead.partyId`, `POST .../start-quoting`. |
| N28 | Aceptar una oferta | **La póliza sale de la oferta y la solicitud.** La oferta trae aseguradora, producto, prima, impuestos, coberturas y vigencia; la solicitud, cliente y riesgo. Al aceptar se confirman forma de pago y asegurados (por defecto, el tomador). Operaciones ajusta en `EnTramite`. Una sola oferta por solicitud: las demás quedan descartadas. | ✅ 2026-11-05 | `QuoteAccepted` lleva los términos y el ID de la póliza. El `Lead` queda `Ganado` al aceptar (DOMAIN §3), aunque la aseguradora rechace después. |
| N29 | Asesor en siniestros | **Un `SalesAgent` reporta y consulta el estado de los siniestros de las pólizas donde es el asesor**, sin datos de salud, notas ni cambios de estado; fuera de su alcance, 404. | ✅ 2026-11-05 | Se decidió "de sus clientes"; la implementación se limita a sus pólizas (más estricta que N11: el responsable de un cliente no ve el siniestro de una póliza de otro asesor). `ClaimsAnalyst` exige MFA por datos de salud. |
| N30 | Pago mayor a lo reclamado y salud | **Solo `Manager` registra un pago mayor a lo reclamado, con motivo y soporte auditados.** Salud: un campo opcional cifrado, con lectura auditada para `ClaimsAnalyst` y `Manager`. La UI advierte no poner datos de salud en la descripción. Soportes: lista de control hasta `Documents` (Fase 5). | ✅ 2026-11-05 | `claims.claim.override-payment`, `claims.health.read`, `claims.health.write`. |
| N31 | Transiciones del prospecto | `Nuevo → Contactado \| Perdido`; `Contactado → Interesado \| Seguimiento \| Perdido`; `Interesado → EnCotizacion \| Seguimiento \| Perdido`; `EnCotizacion → Ganado \| Seguimiento \| Perdido`; `Seguimiento → Contactado \| Interesado \| Perdido`. | 🔧 2026-11-05 | Lectura de DOMAIN y Flujo 1. `Ganado` solo llega por `QuoteAccepted`. |
| N32 | Valor reclamado y descripción | **Fijar o cambiar el valor reclamado exige motivo**, porque de él depende el tope del pago (N30). **La descripción del siniestro sigue visible para el asesor**, con advertencia en la UI de no incluir datos de salud. | ✅ 2026-11-05 | El motivo queda en las gestiones; la bitácora guarda valor nuevo, anterior y autor. |
| N33 | Cumplimiento (Flujo 10) | **Listas restrictivas: consulta manual** del oficial, inmutable, ligada al documento vigente (ADR 0012); el proveedor automático queda detrás de un puerto. **Supresión de habeas data: se registra y se decide a mano; ningún dato se borra automáticamente.** 🔧 PQR `Recibida → En trámite → Resuelta` (la respuesta es obligatoria). | ✅ 2026-11-05, estados 🔧 | `compliance.kyc.*`, `compliance.complaint.manage`, `compliance.habeas.*`. |
| N34 | Documentos | **Retención: 1 año** desde la carga, por `SEGUROS_DOCUMENTS_RETENTION_DAYS` (global, no por agencia). **Sin borrado automático:** cumplido el plazo solo entra a revisión. 🔧 PDF, PNG o JPEG hasta 20 MiB; solo se descarga tras el antivirus. 🔧 Roles: `Operations`, `ClaimsAnalyst` y `ComplianceOfficer` cargan y leen; `Manager` lee; sensibles: leen `ClaimsAnalyst`, `Manager` y `ComplianceOfficer`, y `Operations` también los carga; los soportes de siniestro siempre son sensibles; leer un documento exige además poder leer el cliente, póliza o siniestro dueño; `SalesAgent` sin acceso por ahora. | ✅ 2026-11-05, ratificado 2026-11-09 por el dueño del producto, parametrizable. Difiere de la referencia T5 (10 años, art. 60 C.Co.). | `documents.document.*`, `documents.retention.read`. |
| N35 | Notificaciones | **Correo por SMTP configurable** (SMS y WhatsApp irán detrás del mismo puerto). Avisos al tomador según DOMAIN §3 y T1. Hasta 5 intentos, con espera creciente; no se envía si la última decisión del titular para alguna finalidad es una revocación. | ✅ 2026-11-09 por el dueño del producto | `notifications.message.*`. |
| N36 | Indicadores | Los de DOMAIN §5. Siniestralidad = pagado en siniestros ocurridos en el periodo / primas netas expedidas en el periodo, por moneda. Atención de siniestros: de reporte a cierre. Conversión: solicitudes aceptadas / creadas. | ✅ 2026-11-09 por el dueño del producto | `reporting.kpi.read`: `Manager`, `Accounting`. |
| N37 | Integraciones | **Capa, kill switch y simulador.** La aseguradora piloto solo tiene portal web: no se automatiza y sus ofertas se registran a mano en Cotizaciones; un conector real se construirá cuando una aseguradora ofrezca API. El simulador nunca se registra en staging ni producción y sus valores van marcados como no oferta. | ✅ 2026-11-05 | `integrations.connector.read`, `integrations.quote.request`. |
| N17 | Renovación anticipada | **La nueva póliza se tramita y expide por adelantado**, con vigencia futura y enlazada a la anterior. La anterior sigue `Vigente` hasta su fin; al vencer pasa a `Vencida → Renovada` de forma automática. | ✅ 2026-11-05 | No cambia la máquina de estados. El asesor conserva el acceso (N11) hasta el fin de la cobertura. |

## 2. Decisiones de producto (SaaS)

| # | Tema | Decisión | Estado |
|---|---|---|---|
| P1 | Planes comerciales | **3 planes escalonados + add-ons** (tabla §3). | ✅ |
| P2 | Alta de agencias | **Manual por super-administrador** de la plataforma: crea el tenant, asigna plan e invita al `TenantAdmin` por email (Keycloak). Autoregistro queda fuera del alcance v1 (si se quiere después: flag `ops.platform.self-signup` + ADR). | ✅ |
| P3 | Moneda | COP por defecto; el modelo soporta otra moneda por póliza (ej. USD en ramos corporativos) sin conversión automática. | 🔧 |
| P4 | Zona horaria y formatos | `America/Bogota`, `es-CO`, fechas `dd/MM/yyyy`, moneda `$ 1.234.567`. | 🔧 |

## 3. Planes y módulos (fuente para `registry.yaml`)

| Módulo (flag) | Básico | Profesional | Empresarial | Add-on |
|---|:-:|:-:|:-:|:-:|
| Núcleo: tenancy, identity, featuremgmt, audit, parties, catalog, policies | ✅ | ✅ | ✅ | — |
| `module.billing` (cartera y cobro) | ✅ | ✅ | ✅ | — |
| `module.renewals` | ✅ | ✅ | ✅ | — |
| `module.documents` | ✅ | ✅ | ✅ | — |
| `module.crm` | — | ✅ | ✅ | ✅ |
| `module.quoting` | — | ✅ | ✅ | ✅ |
| `module.claims` | — | ✅ | ✅ | ✅ |
| `module.notifications` | — | ✅ | ✅ | ✅ |
| `module.commissions` (requiere billing) | — | — | ✅ | ✅ |
| `module.compliance` | — | — | ✅ | ✅ |
| `module.reporting` | — | — | ✅ | ✅ |
| `module.integrations` (por aseguradora: `ops.integrations.<insurer>`) | — | — | ✅ | ✅ |

Nombres comerciales y precios: fuera del alcance del código; se configuran en `tenancy`.

## 4. Parámetros por defecto técnicos (🔧, editables por tenant)

| # | Parámetro | Valor por defecto | Fuente |
|---|---|---|---|
| T1 | Recordatorio de cuota | 5 días antes y el día del vencimiento | Práctica común |
| T2 | Alertas de renovación | 60, 30 y 15 días antes del fin de vigencia | flujos.md Flujo 6 (30–60 días) |
| T3 | Vigencia de una oferta de cotización | 30 días (la aseguradora puede indicar otra) | Práctica común |
| T4 | Tolerancia de conciliación de comisiones | COP 1.000 por póliza | Redondeos entre sistemas |
| T5 | Retención documental | Referencia: 10 años desde el fin de la relación (Código de Comercio art. 60). **El sistema usa 1 año (N34)** | **[VALIDAR con asesor legal]** |
| T6 | Sesión de usuario | 30 min inactividad, 8 h máximo | ARCHITECTURE.md §5 |
| T7 | Prima no devengada en cancelación | La calcula y paga la aseguradora; la agencia registra el valor informado | Deriva de N1 |

## 5. Preguntas abiertas
Ninguna bloqueante para las Fases 0–4. Pendiente: proveedor automático de listas restrictivas (N33).
