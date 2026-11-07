# scaffold — developer entrypoint. `make` (or `make help`) prints the target menu.
#
# Conventions:
#   - Every public target carries a `## description`; `help` renders them, `## Section`
#     lines become headings. A target without a `##` is internal.
#   - `.env` is loaded when present; a fresh clone without one still works because every
#     variable below has a local-dev default matching .env.example and docker-compose.yml.
#   - Tool versions are pinned in one place (below) so `make setup` is the only file to
#     touch when bumping.
#   - `make gen-check` is the same dirty-tree gate CI runs — run it before pushing.

SHELL := /bin/bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help
MAKEFLAGS += --no-builtin-rules

# ── environment ───────────────────────────────────────────────────────────────
-include .env
# GNU make keeps the whitespace between a value and an inline `# comment`, so
# `LOG_LEVEL=debug   # debug | info` would export as "debug   ". Strip every var .env defined.
DOTENV_VARS := $(shell sed -nE 's/^([A-Za-z_][A-Za-z0-9_]*)=.*/\1/p' .env 2>/dev/null)
$(foreach v,$(DOTENV_VARS),$(eval $(v) := $(strip $($(v)))))
export

# ── tool versions (bump here; `make setup` installs) ──────────────────────────
OAPI_CODEGEN_VERSION  ?= v2.8.0
MOCKGEN_VERSION       ?= v0.6.0
AIR_VERSION           ?= v1.67.4
GOLANGCI_LINT_VERSION ?= v2.13.2
GOVULNCHECK_VERSION   ?= v1.8.0

# ── project variables ─────────────────────────────────────────────────────────
MODULE         := github.com/bete7512/scaffold
# Falls back to the compose `core` profile Postgres when .env is absent.
DATABASE_URL   ?= postgres://scaffold:scaffold@localhost:5433/scaffold?sslmode=disable
COMPOSE        := docker compose --profile core
COMPOSE_QUEUE  := docker compose --profile queue

# Migrations are Go files compiled into the api binary (migrations/), run through
# `api -m migrate <goose command>` — the same image runs them as a one-off ECS task.
APP ?= api
SVC     := apps/$(APP)
MIGRATE := go run ./$(SVC)/cmd/api -m migrate
# Image build: TARGET picks cmd/<TARGET> of APP; GIT_SHA tags the image and the OCI revision label.
TARGET  ?= api
GIT_SHA ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)

.PHONY: help setup dev run-api run-worker db-up db-down db-reset queue-up queue-down queue-logs \
        lint lint-openapi fmt vet test test-integration test-cover tidy vuln \
        gen gen-openapi gen-proto gen-mocks gen-check build image images \
        migrate-up migrate-down migrate-status

help: ## Show this help
	@awk 'BEGIN { FS = ":.*## " } \
	  /^## / { printf "\n\033[1m%s\033[0m\n", substr($$0, 4); next } \
	  /^[a-zA-Z0-9_-]+:.*## / { printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)
	@echo

## Bootstrap

setup: ## Install pinned Go tools and create .env from .env.example if missing
	go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OAPI_CODEGEN_VERSION)
	go install go.uber.org/mock/mockgen@$(MOCKGEN_VERSION)
	go install github.com/air-verse/air@$(AIR_VERSION)
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	@# Fill absent local keys without replacing a developer's non-empty values.
	go run ./scripts/setup-env

dev: ## Start local infrastructure then run the API
	$(MAKE) db-up
	$(MAKE) run-api

run-api: ## Run the API service from source
	go run ./$(SVC)/cmd/api

run-worker: ## Run the worker from source
	go run ./$(SVC)/cmd/worker

build: ## Build the api and worker binaries of APP into bin/
	go build -o bin/$(APP)/api ./$(SVC)/cmd/api
	go build -o bin/$(APP)/worker ./$(SVC)/cmd/worker

## Local infrastructure

db-up: ## Start Postgres (compose `core` profile) and wait until healthy
	$(COMPOSE) up -d --wait

db-down: ## Stop Postgres (data volume is kept)
	$(COMPOSE) down

db-reset: ## Destroy the Postgres volume and start fresh
	$(COMPOSE) down -v
	$(MAKE) db-up

## Queue

queue-up: ## Start LocalStack (SNS+SQS) and Mailpit (compose `queue` profile) and wait until healthy
	$(COMPOSE_QUEUE) up -d --wait

queue-down: ## Stop LocalStack and Mailpit
	$(COMPOSE_QUEUE) down

queue-logs: ## Follow LocalStack and Mailpit logs
	$(COMPOSE_QUEUE) logs -f

## Quality gates

lint: ## Run golangci-lint (config in .golangci.yml); `make lint-openapi` checks the spec
	golangci-lint run ./...

lint-openapi: ## Validate the OpenAPI document (kin-openapi, follows multi-file $refs)
	go run github.com/getkin/kin-openapi/cmd/validate --ext -- $(SVC)/openapi/openapi.yaml

fmt: ## Format Go code (gofmt + goimports via golangci-lint)
	golangci-lint fmt ./...

vet: ## Run go vet
	go vet ./...

test: ## Unit + handler tests (no Docker needed)
	go test -race -count=1 ./...

test-integration: ## All tests incl. repo/e2e behind the `integration` build tag (needs Docker)
	go test -race -count=1 -tags integration ./...

test-cover: ## Run tests with coverage; writes coverage.out and coverage.html
	go test -race -count=1 -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html
	go tool cover -func=coverage.out | tail -n 1

tidy: ## go mod tidy
	go mod tidy

vuln: ## Scan dependencies with govulncheck
	govulncheck ./...

## Code generation (output under gen/ is committed; never hand-edit it)

gen: gen-openapi gen-proto gen-mocks ## Regenerate all generated code

# Two steps: bundle $(SVC)/openapi/** into one document (oapi-codegen v2.8 cannot generate a
# single package from external path items; see scripts/openapi-bundle), then generate.
gen-openapi: ## Bundle and generate the OpenAPI code of APP
	go run ./scripts/openapi-bundle -in $(SVC)/openapi/openapi.yaml -out $(SVC)/gen/openapi/openapi.bundle.yaml
	oapi-codegen -config $(SVC)/openapi/oapi-codegen.yaml -o $(SVC)/gen/openapi/openapi.gen.go $(SVC)/gen/openapi/openapi.bundle.yaml

gen-proto: ## Generate gRPC code from api/proto with buf (day 20)
	@echo "gen-proto not yet wired (day 4/20)"

gen-mocks: ## Regenerate mockgen mocks (//go:generate directives next to each interface)
	go generate ./...

gen-check: ## Fail if regenerating changes any generated file (no git needed)
	@before=$$(find apps -type f \( -name '*.go' -o -name '*.yaml' \) | sort | xargs sha256sum | sha256sum); \
	$(MAKE) --no-print-directory gen >/dev/null; \
	after=$$(find apps -type f \( -name '*.go' -o -name '*.yaml' \) | sort | xargs sha256sum | sha256sum); \
	if [ "$$before" != "$$after" ]; then echo "generated code was stale and has been regenerated; review the diff" >&2; exit 1; fi

## Migrations (local only)
# In prod, migrations run as a one-off ECS task in the deploy workflow, before the new api
# revision rolls out — never at application boot.

migrate-up: ## Apply pending migrations to DATABASE_URL (api -m migrate up)
	$(MIGRATE) up

migrate-down: ## Roll back the most recent migration
	$(MIGRATE) down

migrate-status: ## Show applied / pending migrations
	$(MIGRATE) status

## Images

image: ## Build the OCI image of APP/TARGET from its own Dockerfile (make image TARGET=worker)
	docker build -f $(SVC)/cmd/$(TARGET)/Dockerfile --build-arg GIT_SHA=$(GIT_SHA) \
		-t scaffold-$(APP)-$(TARGET):$(GIT_SHA) -t scaffold-$(APP)-$(TARGET):latest .

images: ## Build api and worker images for APP
	$(MAKE) image TARGET=api
	$(MAKE) image TARGET=worker

