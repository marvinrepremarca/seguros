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
| T5 | Retención documental | 10 años desde el fin de la relación | Código de Comercio art. 60 — **[VALIDAR con asesor legal]** |
| T6 | Sesión de usuario | 30 min inactividad, 8 h máximo | ARCHITECTURE.md §5 |
| T7 | Prima no devengada en cancelación | La calcula y paga la aseguradora; la agencia registra el valor informado | Deriva de N1 |

## 5. Preguntas abiertas
Ninguna bloqueante para las Fases 0–4. Antes de la Fase 5 (`Compliance` e `Integrations`) habrá que definir: listas restrictivas a consultar (proveedor), aseguradora piloto para integración y canal de notificaciones (email/SMS/WhatsApp y proveedor).
