---
paths:
  - "deploy/**"
  - "**/Dockerfile*"
  - "**/*compose*.yml"
  - ".github/**"
---
# Reglas de contenedores y DevOps

## Dockerfiles
- Multi-stage. Build con `golang:1.25` (`CGO_ENABLED=0 go build -trimpath -ldflags="-s -w"`, caché de módulos con `--mount=type=cache`); runtime `gcr.io/distroless/static-debian12:nonroot`. Frontend: estáticos embebidos con `embed` o `nginx-unprivileged`.
- Imagen base fijada por **digest**. `USER nonroot` (UID 65532). Sin shell en runtime; healthcheck con subcomando del propio binario (`/app/api healthcheck`).
- `.dockerignore` excluye `.git`, `node_modules`, `.env*`, `**/*_test.go`, `test/`.
- Desarrollo local en Windows/macOS: Podman Desktop o Rancher Desktop (Docker Desktop requiere suscripción paga en empresas medianas/grandes).
- Sin secretos en `ARG`/`ENV`/capas. Usa BuildKit `--mount=type=secret` si un build lo necesita.

## Compose / runtime
- `read_only: true`, `tmpfs` para `/tmp`, `security_opt: [no-new-privileges:true]`, `cap_drop: [ALL]`, límites `deploy.resources`.
- Redes `edge` / `app` / `data`; PostgreSQL, Valkey y SeaweedFS solo en `data`, sin `ports:` publicados.
- Solo Traefik expone 80→443. TLS automático, HSTS, middlewares de rate limit y headers.
- Volúmenes nombrados para datos; backups de PostgreSQL con pgBackRest a destino externo cifrado.
- `restart: unless-stopped`, healthchecks con `depends_on: condition: service_healthy`.

## CI/CD
- Pipeline (orden fijo): lint (golangci-lint, eslint) → build → unit (`-race`) → architecture (go-arch-lint) → integration → SAST (gosec, Semgrep CE) → SCA (govulncheck, pnpm audit) → licencias (go-licenses, license-checker) → secret scan → image build → Trivy (falla en HIGH/CRITICAL con fix disponible) → SBOM + cosign → staging → E2E + ZAP → aprobación → prod rolling → smoke.
- Acciones de GitHub fijadas por SHA, `permissions:` mínimos por job, OIDC para despliegue (sin llaves SSH de larga vida si es posible).
- Migraciones como job separado previo al rollout, con backup inmediato antes.

## Prohibido sin confirmación humana explícita
`docker volume rm`, `docker system prune -a --volumes`, borrar backups, cambiar reglas de firewall de producción, ejecutar migraciones contra producción, rotar llaves de cifrado.
