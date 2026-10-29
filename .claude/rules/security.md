---
paths:
  - "src/**"
  - "deploy/**"
---
# Reglas de seguridad (OWASP Top 10:2025 + datos sensibles)

## Clasificación de datos
| Nivel | Ejemplos | Requisitos |
|---|---|---|
| **Restringido** | Datos de salud, número de documento, cuentas bancarias, resultados SARLAFT, credenciales de portales de aseguradoras | Cifrado de campo (AES-256-GCM, envelope), blind index para búsqueda, auditoría de **lectura**, enmascarado en UI, nunca en logs/caché/URLs/eventos |
| **Confidencial** | Nombre, teléfono, email, dirección, primas, pólizas | Autorización por recurso, auditoría de escritura, nunca en logs |
| **Interno** | Catálogos, configuración | Autorización estándar |

Los eventos de integración transportan **IDs**, no datos Restringidos.

## Control de acceso (A01)
- Deny-by-default: toda operación del OpenAPI declara `x-permission`; una prueba recorre el router y falla si alguna ruta no tiene permiso registrado. Rutas públicas solo en lista blanca explícita.
- Verifica propiedad del recurso (tenant por RLS + reglas como "asesor solo ve sus leads").
- Nunca aceptes `tenantId`, `userId` o `roles` desde body/query/headers del cliente.
- IDs no secuenciales (UUIDv7). Responde 404 (no 403) a recursos de otro tenant.

## Autenticación y sesión (A07)
- Solo vía Keycloak/OIDC con PKCE; el BFF guarda tokens server-side; cookies `__Host-` `HttpOnly; Secure; SameSite=Strict`.
- MFA obligatoria para `TenantAdmin`, `Manager`, `Accounting`, `ComplianceOfficer`.
- No implementes login, hashing de contraseñas ni JWT propios. Validación de tokens solo con `go-oidc` (verificar `iss`, `aud`, `exp`, algoritmo fijo).

## Inyección (A05)
- SQL solo vía queries sqlc o pgx con placeholders `$n`. Prohibido `fmt.Sprintf`/concatenación para construir SQL.
- Validar toda entrada (go-playground/validator + constructores de dominio / Zod): longitudes máximas, formatos (cédula/NIT), rangos de montos. Limitar tamaño de body (`http.MaxBytesReader`) y usar `json.Decoder.DisallowUnknownFields`.
- HTML/plantillas solo con `html/template` (nunca `text/template` para salida HTML).
- Archivos subidos: lista blanca de MIME verificada por *magic bytes*, tamaño máximo, nombre regenerado, escaneo antivirus (ClamAV) asíncrono antes de habilitar descarga, servidos vía URL prefirmada de corta vida.
- Plantillas de PDF/email: sin evaluación de expresiones con datos de usuario.

## Criptografía (A04)
- Solo stdlib de Go (`crypto/aes` + `cipher.NewGCM`, `crypto/hmac` + SHA-256, `crypto/rand`). Prohibido `math/rand` para secretos, MD5/SHA1/ECB y algoritmos caseros. Comparaciones de secretos con `subtle.ConstantTimeCompare`.
- Llaves fuera del repo y de la BD (**OpenBao**, MPL-2.0; no HashiCorp Vault por licencia BSL); rotación soportada por versión de llave en el ciphertext.
- Servidor HTTP con `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout` definidos (evita Slowloris).

## Configuración y secretos (A02, A03)
- Ningún secreto en código, `appsettings*.json`, Dockerfiles, compose o tests. Usa variables de entorno/secret store y `.env.example` sin valores.
- Headers: HSTS, CSP estricta, `X-Content-Type-Options`, `Referrer-Policy: strict-origin-when-cross-origin`, `Permissions-Policy`, `frame-ancestors 'none'`.
- CORS: solo orígenes explícitos; nunca `*` con credenciales.
- Nuevas dependencias: justificar, verificar mantenimiento y licencia (ARCHITECTURE.md §9), fijar versión (`go.sum`, `pnpm-lock.yaml`), pasar `govulncheck`. Prohibido paquetes con < 1 año o sin mantenedor activo para funciones de seguridad.

## Integridad y errores (A08, A10)
- Webhooks entrantes (pasarelas, aseguradoras): firma HMAC + timestamp + anti-replay.
- Middleware de recover + mapeo de errores → problem+json genérico; detalles solo en logs con `traceId`. Errores de dominio mapeados explícitamente; cualquier otro error = 500 genérico.
- Fail-closed en autorización, flags y validaciones de pago.

## Logging y auditoría (A09)
- Logs estructurados con `log/slog` + OTel, con `tenant_id`, `user_id`, `trace_id`. **Nunca** PII ni tokens: los tipos sensibles implementan `slog.LogValuer` devolviendo valor redactado; prohibido loguear structs de dominio completos o requests crudos.
- Auditar: login, cambios de permisos, flags, pagos/reversos, cambios de estado de póliza/siniestro, exportes de datos, lectura de datos Restringidos.

## Rate limiting
Por usuario y por tenant en API; más estricto en login, exportes, carga de archivos y endpoints de búsqueda de personas.

## Cumplimiento Colombia
- Habeas data (Ley 1581/2012): registrar autorización de tratamiento por `Party` con fecha, finalidad y versión del texto; soportar consulta, rectificación y supresión (respetando retención legal).
- SARLAFT: consultas a listas restrictivas registradas e inmutables.
