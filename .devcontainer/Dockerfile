# Entorno de desarrollo: Go + herramientas de generación, lint y seguridad.
# Node 22, pnpm y Claude Code se añaden como features en devcontainer.json.
FROM mcr.microsoft.com/devcontainers/go:2.3.5-1.26-bookworm

# Versiones fijadas (actualizar deliberadamente, nunca @latest).
# Todo se instala con `go install`, verificado contra sum.golang.org.
ARG SQLC_VERSION=v1.31.1
ARG OAPI_CODEGEN_VERSION=v2.8.1
ARG GOOSE_VERSION=v3.29.0
ARG GOLANGCI_LINT_VERSION=v2.15.0
ARG GOVULNCHECK_VERSION=v1.9.0
ARG GOSEC_VERSION=v2.30.0
ARG GO_LICENSES_VERSION=v2.0.1
ARG GOIMPORTS_VERSION=v0.52.0
ARG GO_ARCH_LINT_VERSION=v1.19.1

RUN go install github.com/sqlc-dev/sqlc/cmd/sqlc@${SQLC_VERSION} \
 && go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@${OAPI_CODEGEN_VERSION} \
 && go install github.com/pressly/goose/v3/cmd/goose@${GOOSE_VERSION} \
 && go clean -cache -modcache

RUN go install golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION} \
 && go install github.com/securego/gosec/v2/cmd/gosec@${GOSEC_VERSION} \
 && go install github.com/google/go-licenses/v2@${GO_LICENSES_VERSION} \
 && go install golang.org/x/tools/cmd/goimports@${GOIMPORTS_VERSION} \
 && go install github.com/fe3dback/go-arch-lint@${GO_ARCH_LINT_VERSION} \
 && go clean -cache -modcache

RUN go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION} \
 && go clean -cache -modcache \
 && chmod -R g+rwX /go
