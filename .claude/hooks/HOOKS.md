# Especificación de hooks de Claude Code

> Los hooks se ejecutan por el harness (no por el modelo). **Implementados** en `.claude/settings.json` con scripts Node (≥18, multiplataforma) en esta carpeta:
> `guard-files.mjs` (H1, H2, H4) · `guard-commands.mjs` (H3) · `post-edit.mjs` (H5, H6) · `stop-verify.mjs` (H7) · `session-start.mjs` (H8) · `lib.mjs` (utilidades).
> Exit code 2 = bloquea y devuelve el motivo a Claude. H5 y H7 se activan solo cuando existan las herramientas (Go, pnpm) y el código. Si cambias un hook, actualiza esta tabla.

| # | Evento | Matcher | Comportamiento | Resultado si falla |
|---|---|---|---|---|
| H1 | `PreToolUse` | `Edit\|Write` | Bloquear edición de `.env*`, `**/secrets/**`, `*.pem`, `*.key`, `config.production.*` | exit 2 + mensaje: "Archivo de secretos protegido" |
| H2 | `PreToolUse` | `Edit\|Write` | Bloquear modificación de migraciones existentes en `src/backend/migrations/**/*.sql` (permitir solo archivos nuevos) y de código generado (`*.gen.go`, `**/sqlc/**`, `src/frontend/**/api/generated/**`) | exit 2: "Crea una migración nueva / edita la fuente y ejecuta make generate" |
| H3 | `PreToolUse` | `Bash\|PowerShell` | Bloquear comandos destructivos: `rm -rf /`, `docker volume rm`, `docker system prune --volumes`, `DROP DATABASE`, `DROP SCHEMA`, `TRUNCATE`, `git push --force` a `main`, `goose ... up/down` contra DSN de producción | exit 2 con explicación |
| H4 | `PreToolUse` | `Edit\|Write` | Escanear contenido nuevo con patrones de secretos (gitleaks rules: llaves AWS, JWT, connection strings con password) | exit 2 |
| H5 | `PostToolUse` | `Edit\|Write` | Formatear archivo editado: `.go` → `gofmt -w` + `goimports -w`; `.ts/.tsx` → `prettier --write` + `eslint --fix`; `.sql` → `sqlfluff fix` (dialecto postgres) | No bloquea; reporta advertencias |
| H6 | `PostToolUse` | `Edit\|Write` | Detectar anti-patrones en archivos editados: `float64`/`float32` junto a `Premium\|Amount\|Commission`, `time.Now()` fuera de `kernel`, `fmt.Sprintf(` con `SELECT\|INSERT\|UPDATE\|DELETE`, `_ = ` sobre errores, `panic(` en `domain`/`app`, `math/rand` en código de seguridad, `dangerouslySetInnerHTML`, `localStorage` | Feedback al modelo (exit 2 en PostToolUse) para que corrija |
| H7 | `Stop` | — | Si hubo cambios en `src/backend`: `go build ./...`, `go vet ./...`, `go-arch-lint check`. Si hubo cambios en `src/frontend`: `pnpm typecheck lint` | Bloquea el cierre y devuelve errores al modelo |
| H8 | `SessionStart` | — | Inyectar contexto: rama actual, módulos con cambios pendientes, flags `release.*` próximos a vencer | Informativo |

## Principios
- Hooks rápidos (< 10 s) en Pre/PostToolUse; lo pesado solo en `Stop`.
- Los scripts deben funcionar en Windows (PowerShell) y Linux (bash) o usar un runtime común (Node o un pequeño binario Go en `tools/hooks`).
- Ningún hook debe tener acceso a red ni a secretos.
