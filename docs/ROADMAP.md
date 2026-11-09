# Hoja de ruta de construcción

> Orden **obligatorio** de construcción. Un agente no empieza una fase hasta que la anterior cumple su *Definición de Hecho* (DoD). Al terminar cada entregable, actualiza su estado en la tabla de seguimiento (§ final) en el mismo commit.
> Para ejecutar las fases 0 y 1 usa la skill `/bootstrap`. Para las fases de negocio usa `/new-module` y `/new-use-case`.

Prerrequisitos de la estación de trabajo: **Docker Desktop**, **VS Code** con la extensión *Dev Containers* y git. Todo lo demás (Go 1.26, Node 22, pnpm, make, sqlc, oapi-codegen, goose, golangci-lint, go-arch-lint, gosec, govulncheck, go-licenses, Claude Code) viene en el **Dev Container** (`.devcontainer/`): en VS Code, *Reopen in Container*. Las versiones de las herramientas se fijan en `.devcontainer/Dockerfile`; actualizarlas es un cambio deliberado con commit propio.

---

## Fase 0 — Fundaciones del repositorio
Objetivo: que `make check` y `pnpm check` corran en verde sobre un proyecto vacío pero bien configurado.

| # | Entregable | Detalle |
|---|---|---|
| 0.1 | `src/backend/go.mod` | módulo `github.com/<org>/seguros` (preguntar `<org>` al usuario), Go 1.26 |
| 0.2 | `Makefile` raíz | targets: `generate`, `fmt`, `lint`, `arch`, `test`, `test-integration`, `check` (= fmt+lint+arch+test), `migrate-up`, `dev-up`, `dev-down` |
| 0.3 | `.golangci.yml` | linters de `.claude/rules/architecture.md` §Estilo, incluido `depguard` y `forbidigo` |
| 0.4 | `.go-arch-lint.yml` | componentes `platform`, `domain`, `app`, `adapters`, `contracts`, `cmd`; reglas de import |
| 0.5 | `sqlc.yaml`, `oapi-codegen` config | overrides `numeric→decimal.Decimal`, `uuid→uuid.UUID`, `date→kernel.Date` |
| 0.6 | `deploy/compose.dev.yml` | postgres 17, valkey, keycloak (realm importado), seaweedfs, otel-collector; `.env.example` sin valores secretos |
| 0.7 | `src/frontend` | Vite + React 19 + TS strict + TanStack Router/Query + Tailwind v4 + shadcn/ui + Vitest + Playwright + ESLint (`boundaries`) + orval; script `check` |
| 0.8 | `.github/workflows/ci.yml` | pipeline de ARCHITECTURE.md §6.4 (staging/prod como jobs deshabilitados hasta Fase 6) |
| 0.9 | `.editorconfig`, `CODEOWNERS`, plantilla de PR con checklist de reglas no negociables |

**DoD:** `make check` y `pnpm -C src/frontend check` en verde; CI en verde; `docker compose -f deploy/compose.dev.yml up -d` levanta todo sano.

---

## Fase 1 — Plataforma (sin lógica de seguros)
Objetivo: todo lo transversal que los módulos dan por hecho.

| # | Entregable | Paquete | DoD específica |
|---|---|---|---|
| 1.1 | Kernel | `internal/platform/kernel` | `Money` (decimal, moneda, `Add/Sub/Mul/Allocate` con reparto de residuo), `Date`, `DateRange`, `Clock` (+ fake), IDs UUIDv7 tipados, `DomainError` con código. Cobertura ≥95%. |
| 1.2 | Config y observabilidad | `platform/config`, `platform/obs` | Config por env validada al arrancar; `slog` JSON con redacción; OTel traces/metrics. |
| 1.3 | Servidor HTTP | `platform/httpx` | chi, timeouts, recover, request-id, problem+json, límites de body, headers de seguridad, `/health/live`, `/health/ready`. |
| 1.4 | Base de datos | `platform/db`, `cmd/migrate` | pool pgx, `WithTenantTx` (set_config), roles `app`/`migrator`, goose por módulo. Prueba: sin `WithTenantTx` no hay acceso. |
| 1.5 | Tenancy | módulo `tenancy` | Tenant, plan, estado; resolución de tenant desde la sesión. |
| 1.6 | Identidad y BFF | módulo `identity`, `platform/authz` | Login OIDC con Keycloak (PKCE), sesión scs en Valkey, cookies `__Host-`, CSRF, `authz.Require`, catálogo de permisos, prueba de ruta sin permiso = falla. |
| 1.7 | Feature flags | `platform/flags`, módulo `featuremgmt` | `registry.yaml` + generador de constantes Go/TS, proveedor OpenFeature por tenant, caché L1/L2 + invalidación, middleware `Require`, endpoint `/bff/features`, validación de `requires` y `removeBy`, auditoría de cambios. |
| 1.8 | Mensajería | `platform/outbox`, `platform/jobs` | outbox + relay Watermill, inbox idempotente, River con `InsertTx`, `cmd/worker`. |
| 1.9 | Auditoría | módulo `audit` | `audit.events` append-only particionada; API interna `audit.Record`. |
| 1.10 | Cifrado de campos | `platform/crypto` | AES-GCM envelope con versión de llave, blind index HMAC; llave maestra desde OpenBao (dev: env). |
| 1.11 | Frontend base | `src/frontend/src/app`, `shared` | layout, login vía BFF, `FeatureGate`, menú por flags/permisos, formateadores COP/fecha, validadores CC/NIT. |
| 1.12 | Prueba de humo E2E | `test/` + Playwright | login → ver menú filtrado por flags de un tenant de prueba. |

**DoD:** todo lo anterior con pruebas; `security-auditor` y `architecture-guardian` sin hallazgos Altos/Críticos.

**Hallazgos de seguridad abiertos (Medios/Bajos, auditoría de 1.6 del 2026-11-03).** No bloquean la Fase 1; deben cerrarse antes de la Fase 6 (salida a producción):
- ~~**M1 — Revocación de sesiones:**~~ Hecho (2026-11-04): cada sesión se confirma con Keycloak por refresh token cada 5 min (`SEGUROS_SESSION_REVALIDATE_INTERVAL`), con gracia de 3 intervalos si no responde. Aplica bajas, roles y salida de la organización; ganar un rol con MFA obliga a entrar de nuevo. Pendientes: el refresh token vive en la sesión de Valkey sin cifrar a nivel de campo (cifrarlo antes de la Fase 6); Back-Channel Logout, si se requiere corte inmediato; `revokeRefreshToken` debe seguir apagado en el realm, porque peticiones simultáneas reusan el token.
- **M2 (resto) — Rate limit distribuido:** el contador de `httprate` es por réplica; moverlo a Valkey cuando haya >1 réplica de `api`. (Hecho: cabecera de IP de confianza obligatoria en staging/prod y límites separados antes de la sesión.)
- **M3 — MFA por `acr`/step-up:** hoy se verifica el claim `amr`, que depende de la configuración de cada autenticador en Keycloak. Migrar a Level of Authentication (`acr_values` + verificación de `acr`) con ADR y prueba E2E contra Keycloak con y sin OTP.
- **B7 — Organización por id:** vincular el tenant al id de la Organization de Keycloak (no solo al alias) para que recrear una organización con el mismo alias no herede el tenant.

Auditoría de 1.7/1.8/1.10 (2026-11-03). El hallazgo Alto (roles: la API podía ejecutar el relay, tocar River y escribir switches globales) y M2–M4, B1, B4–B6 quedaron corregidos. Siguen abiertos:
- ~~**M1 — Flags que activan controles:**~~ Definido en [ADR 0012](adr/0012-flags-que-activan-controles.md): la emisión se bloquea si el flag no se puede evaluar y `kill` exige `--confirm-control`.
- **B2 — Bindings criptográficos:** incluir tenant y versión en el AAD del envoltorio de la DEK. (Hecho en Fase 2: `EncryptFor`/`DecryptFor` ligan el campo al ID del registro.)
- **B3 — Llaves:** versión de la llave de blind index con ruta de reindexado; token de OpenBao con AppRole/renovación; rechazar versiones duplicadas en `SEGUROS_CRYPTO_LOCAL_KEKS`; un fallo al desenvolver una DEK retirada no debe bloquear al tenant entero.
- **B7 — Override de plataforma vs. tenant:** distinguir quién fijó un override para que un TenantAdmin no revierta un `off` de la plataforma (relevante cuando existan flags `tenantEditable`).
- **B8 — Serializar cambios de flags por tenant** (`pg_advisory_xact_lock`) para que dos cambios concurrentes no dejen dependencias inconsistentes.
- **B9 — Rate limit por usuario y tenant en la API** (hoy solo login/callback por IP).

Revisiones finales de la Fase 1 (2026-11-03): `architecture-guardian` APROBADO y `security-auditor` sin Altos/Críticos. Corregidos al cierre: consumidores que confirmaban eventos con el flag no evaluable (ahora reintentan), escritura de `tenancy.tenants` solo para `seguros_platform`, `audit.ensure_partitions` solo para el worker, verificación del rol de BD al arrancar api/worker, `sslmode=verify-full` en producción y `go mod tidy`. Quedan abiertos:
- ~~**Antes de la Fase 2:**~~ Hecho (2026-11-03): la API sirve la SPA compilada (`SEGUROS_SPA_DIR`, `httpx.SPAHandler`) con CSP por nonce, Trusted Types y headers de seguridad (`e2e/csp.spec.ts`); alta, plan y suspensión de tenants quedan en `audit.events` y publican `TenantStatusChanged`/`TenantPlanChanged`, con `--reason` y `--operator` obligatorios en platformctl.
- ~~**Antes de la Fase 3:**~~ Hecho (2026-11-05): `jobs.TenantWorker`, purga de `messaging.inbox` (30 días), reproceso dirigido al consumidor que falló y métricas `messaging.*` con aviso en el log. Abierto: definir en Grafana las reglas de alerta sobre esas métricas (Fase 6), porque una revocación de alcance de asesor (N10) perdida en `dead_letters` deja al asesor con acceso sin plazo; evaluar una conciliación periódica de `policy_scopes`.
- **Mejoras de arquitectura:** ADR "outbox propio + transporte en proceso (RabbitMQ en fase 2)" (desvía de watermill-sql en ARCHITECTURE.md §3/§5); componente de arch-lint que impida a `app` importar `httpx`/`db`/`session`; mover la selección de `/bff/features` a un caso de uso; caché corta del tenant en el autenticador; SQL de `internal/platform` a sqlc o excepción documentada; generar los permisos TS desde `authz/catalog.go`; `go mod tidy -diff` en CI.
- **Bajos de seguridad:** identidad real del operador en el historial de kill switches (no solo `--operator`); traces de Playwright sin el valor de la contraseña cuando apunten a entornos persistentes; `ListFlags` sin flags `ops`; errores del publisher normalizados por código.
- **Fase 6:** aprovisionar los roles `seguros_worker` y `seguros_platform` en staging/producción (en desarrollo los crea `deploy/postgres/init/01-roles.sh`).

---

## Fase 2 — Núcleo de negocio (módulos no apagables)
Orden: **Parties → Catalog → Policies**.
- `parties`: personas/organizaciones, documentos cifrados, contactos, consentimiento habeas data.
- `catalog`: aseguradoras, ramos, productos, esquemas JSON de riesgo por ramo, acuerdos de comisión.
- `policies`: póliza + máquina de estados completa, coberturas, bienes asegurados, endosos, cancelación, eventos de DOMAIN.md §3.

**DoD:** flujo 3 (expedición manual de póliza) y 5, 8 operables desde la UI; todas las transiciones probadas.

Revisiones de la Fase 2 (2026-11-04). `security-auditor`: el Alto (un endoso podía cambiar el tomador sin pasar por KYC) y parte de M2, B3, B4 y B5 quedaron corregidos. `architecture-guardian`: M1–M5 corregidos. Quedan abiertos:
- ~~**M1 — Alcance de `SalesAgent` sobre clientes:**~~ Hecho (DECISIONS N10): solo sus clientes; el documento se corrige con `parties.document.rectify`.
- **M2 (resto) — Oráculo de documentos:** la búsqueda y el `PUT` con documento confirman si un número existe. `search-by-document` y `reveal` ya tienen 30/min por usuario (por réplica); también el registro y la rectificación, cuyos conflictos se auditan; ya hay límite de 300/min por tenant (por réplica); faltan alertas de volumen (Fase 6).
- **M3 — KYC ligado al documento (requisito de la Fase 5):** ver ADR 0012.
- **Bajos de seguridad:** `q` con el nombre del cliente en la URL y logs de Traefik (Fase 6).
- **Bajos de arquitectura:** retirar `*_keyset_idx` solo si una medición muestra que sobran (`policies_tenant_keyset_idx` sirve al listado sin filtro). Hechos: límites y enumeraciones de los generados en los Zod (los mensajes y reglas de documento siguen propios), TanStack Table en listados, `Agent` en `identity.Directory`, bloqueo de endosos sobre otra versión del esquema de riesgo y permisos alternativos `a|b` en el guard.
- **Auditoría de N10 (2026-11-04), sin Altos ni Críticos:** corregidos B1–B3. Hecho: `worker rebuild-scopes --org <alias>` (el worker se despliega antes que la API). `ListParties` con alcance: de 1,7 s a 7 ms (`EXPLAIN ANALYZE`, 200.000 clientes, 1.000 pólizas del asesor); falta la prueba de carga con k6 (Fase 6).
- **Rotación de empleados (N11):** hecho el asesor responsable, el historial, el traspaso individual y en bloque, y el corte de sesión. Abierto: alguien nuevo solo se puede asignar tras su primer ingreso (leer la organización en Keycloak exige `manage-realm`, y no se le da a la API; resolverlo cuando exista el flujo de invitación de P2). Validar comisiones en un traspaso en la Fase 3.
- **Auditoría de sesión y traspasos (2026-11-04), sin Altos ni Críticos:** corregidos M1 (espera de 30 s tras una falla de Keycloak y una sola llamada por sesión), M2 (quien sale de la organización deja de ser asesor activo al detectarse en su sesión), M3 (caché de 30 s del recorrido del realm), M4 (`parties.party_responsible_history`), B1 (la cancelación del cliente ya no interrumpe la revalidación), B2 y B5. El refresh token ya va cifrado en la sesión. Abiertos: quien sale y no vuelve a hacer peticiones sigue como activo hasta el flujo de invitación de P2 o Back-Channel Logout; el recorrido falla con más de 10.000 `SalesAgent` en el realm; B6 la cuenta de servicio sí necesita `view-realm` (sin él, Keycloak responde 403 al listar el rol); falta aprovisionarla en staging y producción (Fase 6); B7 el relleno de `responsible_agent_id` no filtra por rol (revisar si llega a haber datos reales); Hecho B8: traspaso de más de un lote; el aislamiento del `AgentDirectory` real se prueba por unidad y con RLS. Sin revisar: comportamiento contra un Keycloak real más allá del E2E, MFA con sesión SSO reutilizada tras un cierre por rol nuevo, y 2 avisos moderados de `pnpm audit`.
- **Auditoría del cierre de Fase 2 (2026-11-04), sin Altos ni Críticos:** corregidos M1 (`/bff/auth/login?reauth=true` pide autenticarse de nuevo tras exigir MFA), M2 (un flag de control se confirma según el resultado, también al limpiar un override o cambiar de plan), M3 (límites en traspasos, búsqueda por nombre y asesores), M4 (reproceso y reconstrucción en la bitácora), B1, B2 (reproceso de más de 7 días solo con `--force`), B4 y B5. Abiertos: idempotency key en traspasos; qué `amr` emite Keycloak al reutilizar una SSO que ya usó OTP; al desplegar a un entorno con sesiones previas, vaciar `bff:sess:`; B6 2 avisos moderados de `pnpm audit`, solo en herramientas de desarrollo (`pnpm audit --prod` limpio).
- **MFA automática (N13):** `deploy/scripts/keycloak-mfa.sh` crea el flujo que pide OTP según el rol y obliga a configurarlo (`CONFIGURE_TOTP`); `make e2e-mfa` lo prueba contra Keycloak real con la API exigiendo MFA. La prueba halló que, sin la referencia `otp` en el paso del flujo, Keycloak no la informa en `amr` y la API negaba a todos. Al crear staging y producción (Fase 6), ejecutar `keycloak-mfa.sh <realm> TenantAdmin Manager Operations Accounting ComplianceOfficer`. Tras configurar el factor por primera vez hay que ingresar otra vez con el código.
- **Para el diseño de Renewals (Fase 3):** ~~flujos.md renueva 30–60 días antes del vencimiento, pero `Vencida → Renovada` exige que la póliza ya haya vencido.~~ Decidido en DECISIONS N17. Además, el relay marca publicados los eventos sin consumidores: los de pólizas expedidas antes de activar Billing no se reprocesan.

## Fase 3 — Dinero
Orden: **Billing → Commissions → Renewals**. Requiere decisiones de [docs/domain/DECISIONS.md](domain/DECISIONS.md) cerradas para estos módulos.

Revisiones de la Fase 3 (2026-11-05). Corregidos: el cableado de `main.go`, pago con fecha futura, tolerancia solo en COP, reverso antes de su pago, tasa del reverso de comisión interna, renovaciones duplicadas (índice único) y MFA de `Collections` (N21). Casos de uso separados por archivo. Abiertos:
- Hecho (2026-11-05): agregado `Accrual` y `Reconcile` en el dominio de Commissions; consumidores OFF por tenant con el despachador real; 404 entre tenants en los POST; `e2e/money.spec.ts` escrito, **sin ejecutar**.
- **E2E:** `make e2e-full` en verde (2026-11-05). `compose exec` sin entrada abierta deja a `kcadm` sin salida: con `</dev/null` o sin stdin, `keycloak-mfa.sh` falla y un reintento duplica el flujo, porque no es transaccional; revisarlo antes de usarlo en CI y en la Fase 6. Keycloak fue terminado (`Killed`) con el límite de 1 GiB de `compose.dev.yml`: se subió a 2 GiB solo en el contenedor actual; evaluar subirlo en el archivo.
- **Riesgo legal aceptado sin validar (art. 1068 C.Co.):** la mora en la prima termina el contrato automáticamente y esa regla es imperativa, mientras el sistema mantiene `EnMora` y solo cancela cuando la aseguradora lo expide (la agencia registra, no decide). Revisar antes de la Fase 6. N22–N26 quedan editables por agencia.
- **Medios:** un `InstallmentOverdue` reprocesado tras un pago marca mora sin revalidar; `AgreementFor` abre una segunda conexión dentro del consumidor; liquidación de 2.000 líneas síncrona con N+1, y `SendAlerts` N+1; `billing.payments` sin particionar (ARCHITECTURE §4.5: particionar antes de aplicarla en un entorno o registrar ADR); `OnPolicyIssued` de Renewals toca dos casos en una transacción; reglas de renovación y `Payment` fuera del dominio; `Reconcile` y las causaciones aún sin revisión de los auditores.
- **Pruebas:** ejecutar el E2E; `/comisiones` no tiene E2E porque el tenant de prueba es plan profesional; cobertura de `domain` bajo el mínimo de 90 % (ver `go test -cover`), sobre todo ramas de error de `Money`.
- **Bajos:** sin FK de `payment_allocations` a la cuota; rate limit en pagos y liquidaciones; valor anterior en la auditoría de parámetros; columnas de auditoría en `commissions.policy_terms`; UI sin pantallas de días de gracia, tasas, tolerancia y días de alerta (la API existe); la liquidación pide el ID de la aseguradora a mano. Recordatorios de cuota (T1): con Notifications, Fase 5. Facturación a la aseguradora: sin definir.
- **Despliegue:** añadir `Collections` a `keycloak-mfa.sh` en staging y producción.

Revisiones finales de la Fase 3 (2026-11-05), sin Críticos ni Altos abiertos tras corregir la conciliación por IDs, el reverso en plan cancelado y el tope de tolerancia fuera de COP. Abiertos además de los anteriores: un `PaymentApplied` reintentado tarde puede sacar de mora sin revalidar; pagos o endosos entregados antes de la proyección se dan por procesados; leer-sumar-guardar sin `version` en `commissions.policy_terms`; `Case` sin constructor y `Installment` con campos exportados; archivos de `app` con varias operaciones; `/configuracion/parametros` sin carga lazy por módulo; `support` del ajuste en la bitácora (texto libre); `PUT .../installments` sin rate limit propio; 403 sin probar en los parámetros de cartera y en `reinstate`. Cobertura de `domain` bajo 90 %: excepción aceptada por el dueño del producto.

## Fase 4 — Venta y servicio
Orden: **Crm → Quoting → Claims**. Decisiones N27–N31.

Abiertos de construcción (2026-11-05):
- **`PolicyExpiring` → Crm (DOMAIN §3):** sin consumidor: ningún documento dice qué debe hacer CRM con él. Definirlo con el dueño del producto.
- **Habeas data del prospecto:** un `Lead` guarda nombre y contacto sin `Consent` (N27: el consentimiento vive en `Party`). Validar con asesor legal antes de la Fase 6.
- **`QuoteAccepted` fallido:** si Policies rechaza el evento queda en `dead_letters` y la solicitud sigue aceptada; el reproceso es manual (alertas: Fase 6). Quoting valida catálogo, partes y asesor antes de publicar.
- **Traspaso con CRM apagado:** el evento no se reprocesa: los prospectos siguen con el asesor anterior hasta reasignarlos.
- **Alcance del asesor en siniestros:** tope de 5.000 pólizas por asesor en el listado; lo que exceda no aparece (falla cerrado).
- **Despliegue:** añadir `ClaimsAnalyst` a `keycloak-mfa.sh` en staging y producción.
- **Auditorías (2026-11-05), corregidos:** traspaso de solicitudes (Alto), soporte de decisión oculto al asesor (Alto), asegurados repetidos, riesgo validado al crear, valor anterior de lo reclamado en bitácora, vigor por lista positiva, traspaso de prospectos auditado.
- **Riesgo aceptado (N32):** `Claim.description` es texto libre visible para el asesor y podría contener salud; solo hay advertencia en la UI.
- **Medios abiertos:** consumidores `crm.*` y `quoting.*` sin prueba de flag OFF por el despachador; `quoting.offers` y `claims.checklist_items` sin columnas estándar de auditoría/versión (migración nueva).
- **Bajos abiertos:** motivo y soporte del override solo en gestiones, sin cifrar, y lectura de gestiones sin auditoría; `SetHealth` sin control de versión ni historial; `expired` calculado en el handler de quoting; `create_from_quote` en `app` usa el DTO de Quoting.
- **E2E:** `make e2e-full` en verde (2026-11-05). `sales.spec.ts` cubre prospecto y apertura de cotizaciones y siniestros; oferta→póliza y siniestro completo solo en integración. Una corrida sin stdin duplicó ejecuciones del flujo MFA local (reparado a mano): corregir `keycloak-mfa.sh` antes de CI y Fase 6.
- **UI:** la oferta registra una cobertura desde el formulario (la API admite 20); asegurados distintos del tomador solo por API.

## Fase 5 — Transversales
**Compliance → Documents → Notifications → Reporting → Integrations.Insurers** (una aseguradora piloto primero). Decisiones N33–N37.

Auditorías (2026-11-05), corregidos: archivos sensibles cifrados en la aplicación y servidos por la API (Alto, ADR 0013), soportes de siniestro siempre sensibles y `Operations` carga sensibles sin poder leerlos (Alto), capas de `integrations`/`reporting`, `make arch` ya falla con avisos, correos duplicados entre jobs (reclamo con `SKIP LOCKED`) y espera entre intentos, límite de frecuencia en archivos y exportes, lectura de KYC auditada y resultado fuera del evento y la bitácora, `ContactView` mínimo, `reporting` migra de último, recordatorio por fecha de Bogotá, nombre sin caracteres de control en correos, kill switches ocultos a la agencia.

Abiertos:
- **Legal, antes de la Fase 6:** no enviar avisos tras una revocación, y una supresión aceptada no detiene los correos si no se registra además la revocación (N35).
- **Medios de seguridad:** `result` y `source` de KYC siguen sin cifrar de campo; `Operations` y `ComplianceOfficer` leen soportes no sensibles sin permiso sobre el registro dueño; no se valida que el dueño de un documento exista; sin reclasificar un documento ya cargado; el archivo queda varias veces en memoria al validar el contrato (multipart); faltan casos de 404 entre tenants por endpoint en `complianceapi` con el directorio real.
- **Medios de arquitectura:** API y worker no arrancan sin la configuración de S3, ClamAV y SMTP aunque esos módulos estén apagados (ARCHITECTURE §1.2); llamada síncrona a la aseguradora sin retry (definir antes del piloto); `KycCheck`, `Document`, `Version` y `Message` sin encapsular en dominio; protocolo de migración de columnas usadas por vistas de `reporting` sin prueba.
- **Bajos:** plazo de 2 min de carga antes de autenticar; `PartyID` de PQR sin validar; breaker compartido entre tenants; `sales-deps` importa claims; servicios `app` con varias operaciones; ClamAV de desarrollo sin `*hardening`; worker construye `parties`/`policies` con dependencias nil.
- **Documents:** archivos no sensibles sin cifrado propio en S3 (solo disco, falta SSE); `SalesAgent` sin acceso; certificados y duplicados (N9) sin generar; lista de control de siniestros (N30) separada.
- **Notifications:** plantillas en código; T1 fijo; remitente único por entorno; `messages` sin particionar; un fallo entre enviar y marcar puede repetir un correo.
- **Reporting:** consulta en vivo; una línea de liquidación sin póliza no cuenta como recibida; fórmulas por validar (N36).
- **Integrations:** sin conector real ni uso desde Quoting; proveedor de listas (N33) pendiente.
- **Despliegue:** `SEGUROS_S3_*`, `SEGUROS_CLAMD_ADDR` y `SEGUROS_SMTP_*` obligatorias en api y worker; worker ahora necesita las llaves de cifrado; ClamAV ~2 GiB; no hay compose de producción con ClamAV.

## Fase 6 — Endurecimiento y salida a producción
Pruebas k6 (objetivos en testing.md), ZAP, restauración de backup probada, runbooks, despliegue en VPS, monitoreo y alertas, onboarding del primer tenant real.

---

## Seguimiento

| Ítem | Estado | Fecha | Notas |
|---|---|---|---|
| Fase 0 | ✅ Hecho | 2026-10-30 | DoD verificada: make check, pnpm check, compose sano y CI en verde (PR #1) |
| Fase 1 | ✅ Hecho | 2026-11-03 | 1.1 ✅ · 1.2 ✅ · 1.3 ✅ · 1.4 ✅ (sesión A, PR #2) · 1.5 ✅ · 1.6 ✅ · 1.7 ✅ · 1.8 ✅ · 1.9 ✅ (adelantada: la usan login y flags; `platform/audit` + `migrations/audit`) · 1.10 ✅ (`platform/crypto`; se cablea en api/worker con el primer módulo con datos Restringidos, Fase 2) · 1.11 ✅ · 1.12 ✅ (`make e2e-full`). DoD verificada: make check, integración, pnpm check, E2E 3/3; architecture-guardian y security-auditor sin Altos/Críticos (hallazgos Medios/Bajos abiertos arriba) |
| Fase 2 | ✅ Hecho | 2026-11-04 | Previos ✅ (CSP de la SPA, auditoría de tenants) · ADR 0012 ✅ · `parties` ✅ · `catalog` ✅ · `policies` ✅ · UI ✅. DoD verificada: make check, integración (`-p 1`), pnpm check, E2E (`make e2e-full`, bajo la CSP real). Sin hallazgos Altos/Críticos abiertos; Medios y Bajos arriba. Transiciones de mora y renovación: sin endpoint hasta la Fase 3, probadas en dominio y persistencia |
| Fase 3 | ✅ Hecho | 2026-11-05 | `billing` ✅ · `commissions` ✅ · `renewals` ✅ · decisiones N14–N26. Verificado: make check, integración, pnpm check, E2E (`make e2e-full`). Sin Críticos ni Altos abiertos en la última auditoría. **Excepción aceptada por el dueño del producto:** cobertura de `domain` bajo 90 % (billing 86,9 %, commissions 84,6 %), sobre todo ramas de error defensivas. Medios y Bajos arriba |
| Fase 4 | ✅ Hecho | 2026-11-05 | `crm` ✅ · `quoting` ✅ · `claims` ✅ · decisiones N27–N32. Verificado: make check, integración, pnpm check, E2E. Sin Críticos ni Altos abiertos; Medios y Bajos arriba |
| Fase 5 | 🟨 En curso | 2026-11-05 | `compliance` ✅ · `documents` ✅ · `notifications` ✅ · `reporting` ✅ · `integrations` (capa y simulador) ✅. Falta la aseguradora piloto y el conector real; ADR 0013 y N34 aceptados; N35–N36 por validar. Medios abiertos arriba |
| Fase 6 | ⬜ Pendiente | | |

Estados: ⬜ Pendiente · 🟨 En curso · ✅ Hecho (DoD verificada) · ⛔ Bloqueado (indicar motivo).
