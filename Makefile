# Tareas del repositorio. Todas las rutas de Go se ejecutan dentro de src/backend.
SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

BACKEND  := src/backend
FRONTEND := src/frontend
GO_MOD   := github.com/marvinrepremarca/seguros
COMPOSE  := deploy/scripts/compose.sh

.PHONY: help generate generate-sqlc generate-openapi fmt fmt-check lint arch test test-integration \
        check vuln licenses migrate-up migrate-status run-api dev-env dev-up dev-down dev-status dev-logs

help: ## Lista los targets
	@grep -hE '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-18s %s\n", $$1, $$2}'

generate: generate-sqlc generate-openapi ## sqlc + oapi-codegen + go generate (constantes de flags)
	cd $(BACKEND) && go generate ./...

generate-sqlc:
	@cd $(BACKEND) && if grep -qE '^sql: \[\]' sqlc.yaml; then echo "sqlc: sin módulos configurados"; \
	else sqlc generate && sqlc vet; fi

# Un spec por módulo: api/openapi/<modulo>.yaml → internal/modules/<modulo>/adapters/http/api.gen.go
generate-openapi:
	@cd $(BACKEND) && for spec in api/openapi/*.yaml; do \
	  [ -e "$$spec" ] || continue; \
	  mod=$$(basename "$$spec" .yaml); \
	  case "$$mod" in oapi-codegen|common) continue;; esac; \
	  out=internal/modules/$$mod/adapters/http; mkdir -p "$$out"; \
	  echo "oapi-codegen $$spec"; \
	  oapi-codegen -config api/openapi/oapi-codegen.yaml -package "$${mod}http" -o "$$out/api.gen.go" "$$spec"; \
	done

fmt: ## Formatea el código Go (gofmt + goimports)
	cd $(BACKEND) && gofmt -w . && goimports -local $(GO_MOD) -w .

fmt-check: ## Falla si hay archivos Go sin formatear
	@cd $(BACKEND) && out="$$(gofmt -l . ; goimports -local $(GO_MOD) -l .)"; \
	if [ -n "$$out" ]; then echo "Archivos sin formatear (ejecuta make fmt):"; echo "$$out" | sort -u; exit 1; fi

lint: ## golangci-lint
	cd $(BACKEND) && golangci-lint run ./...

arch: ## go-arch-lint (capas) + prueba de fronteras entre módulos
	cd $(BACKEND) && go-arch-lint check
	cd $(BACKEND) && go test -count=1 ./test/arch/...

test: ## Pruebas unitarias con -race
	cd $(BACKEND) && go test -race -count=1 ./...

test-integration: ## Pruebas de integración (testcontainers; requiere Docker)
	cd $(BACKEND) && go test -race -count=1 -tags=integration ./...

check: fmt-check lint arch test ## Verificación completa del backend

vuln: ## govulncheck
	cd $(BACKEND) && govulncheck ./...

licenses: ## Falla si alguna dependencia enlazada tiene licencia fuera de ADR 0011
	cd $(BACKEND) && go-licenses check ./... --ignore $(GO_MOD) \
	  --allowed_licenses=MIT,MIT-0,BSD-2-Clause,BSD-3-Clause,0BSD,ISC,Apache-2.0,PostgreSQL,Zlib,BlueOak-1.0.0,Unlicense,CC0-1.0,Python-2.0,MPL-2.0

DEV_DB_URL := $(CURDIR)/deploy/scripts/dev-db-url.sh

migrate-up: ## Aplica migraciones al PostgreSQL de desarrollo con el rol migrator
	cd $(BACKEND) && SEGUROS_MIGRATOR_DB_URL="$$($(DEV_DB_URL) migrator)" go run ./cmd/migrate up

migrate-status: ## Versión y migraciones pendientes por módulo (desarrollo)
	cd $(BACKEND) && SEGUROS_MIGRATOR_DB_URL="$$($(DEV_DB_URL) migrator)" go run ./cmd/migrate status

run-api: ## Ejecuta la API local contra las dependencias de desarrollo
	cd $(BACKEND) && SEGUROS_DB_URL="$$($(DEV_DB_URL) app)" go run ./cmd/api

dev-env: ## Crea/completa deploy/.env con secretos aleatorios locales
	@deploy/scripts/dev-env.sh

dev-up: dev-env ## Levanta dependencias de desarrollo (crea deploy/.env si falta)
	$(COMPOSE) up -d --wait

dev-status: ## Estado de las dependencias de desarrollo
	$(COMPOSE) ps

dev-logs: ## Logs de las dependencias (make dev-logs s=keycloak)
	$(COMPOSE) logs --tail 100 $(s)

dev-down: ## Detiene dependencias de desarrollo (conserva volúmenes)
	$(COMPOSE) down
