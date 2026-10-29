# Tareas del repositorio. Todas las rutas de Go se ejecutan dentro de src/backend.
SHELL := bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

BACKEND  := src/backend
FRONTEND := src/frontend
GO_MOD   := github.com/repremarca/seguros
COMPOSE  := docker compose -f deploy/compose.dev.yml --env-file deploy/.env

.PHONY: help generate generate-sqlc generate-openapi fmt fmt-check lint arch test test-integration \
        check vuln licenses migrate-up dev-up dev-down

help: ## Lista los targets
	@grep -hE '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-18s %s\n", $$1, $$2}'

generate: generate-sqlc generate-openapi ## sqlc + oapi-codegen + go generate (constantes de flags)
	cd $(BACKEND) && go generate ./...

generate-sqlc:
	cd $(BACKEND) && sqlc generate

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

licenses: ## Falla si alguna dependencia enlazada tiene licencia fuera de ARCHITECTURE.md §9
	cd $(BACKEND) && go-licenses check ./... \
	  --allowed_licenses=MIT,BSD-2-Clause,BSD-3-Clause,Apache-2.0,MPL-2.0,ISC,PostgreSQL

migrate-up: ## Aplica migraciones (local) con el rol migrator
	cd $(BACKEND) && go run ./cmd/migrate up

dev-up: ## Levanta dependencias de desarrollo
	$(COMPOSE) up -d --wait

dev-down: ## Detiene dependencias de desarrollo (conserva volúmenes)
	$(COMPOSE) down
