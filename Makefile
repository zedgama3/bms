# Developer entry points. Each target is one check, so check-runner can run them one at a time.
GO       ?= go
GOLANGCI_LINT ?= $(shell command -v golangci-lint || echo $(HOME)/go/bin/golangci-lint)
COMPOSE  ?= docker compose -f deploy/compose/docker-compose.yml --env-file deploy/compose/.env
PNPM     ?= pnpm
# Shared by `bin` and `web-build`: the binary and the web bundle report the same version.
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
# The development environment: the compose .env, loaded the way an operator would.
DEVENV   = set -a; . deploy/compose/.env; set +a;

.PHONY: help fmt vet build test test-integration lint imports web-check web-build openapi-check bin run migrate test-e2e check check-go up observe down ps

help:             ## list the targets; check-go is the Go-only subset of check
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | sed -E 's/^([a-zA-Z_-]+):[^#]*## /  \1\t/'

fmt:              ## fails if any file needs gofmt
	@out=$$(gofmt -l $$(git ls-files -co --exclude-standard '*.go')); if [ -n "$$out" ]; then echo "$$out"; exit 1; fi

vet:              ## go vet
	$(GO) vet ./...

build:            ## go build ./... (needs no Node)
	$(GO) build ./...

test:             ## unit tests: no infrastructure needed
	$(GO) test -race ./...

test-integration: ## needs `make up`
	$(GO) test -race -tags integration ./...

lint:             ## golangci-lint
	$(GOLANGCI_LINT) run ./...

imports:          ## a module never imports another module (Go and web)
	./scripts/check-module-imports.sh
	./scripts/check-web-module-imports.sh

web-check:        ## every web check (needs Node and pnpm)
	$(PNPM) -C web check

web-build:        ## writes web/dist, including version.txt
	BMS_VERSION=$(VERSION) $(PNPM) -C web build

openapi-check:    ## the committed OpenAPI document and its generated types match the binary
	$(GO) run ./cmd/bms openapi > web/openapi.json.new || { rm -f web/openapi.json.new; exit 1; }
	mv web/openapi.json.new web/openapi.json
	$(PNPM) -C web generate:api
	git diff --exit-code web/openapi.json web/src/api/schema.d.ts

bin: web-build   ## the release binary, web bundle inside
	$(GO) build -ldflags "-X github.com/zedgama3/bms/internal/app.version=$(VERSION)" -o bin/bms ./cmd/bms

run:              ## development: serve with the compose .env
	$(DEVENV) $(GO) run ./cmd/bms serve

migrate:          ## development: apply migrations with the compose .env
	$(DEVENV) $(GO) run ./cmd/bms migrate

test-e2e: bin     ## Playwright against bin/bms serve in each mode (needs `make up`, Node, Playwright browsers)
	./scripts/e2e.sh
	BMS_E2E_MODE=hello-disabled ./scripts/e2e.sh
	BMS_E2E_MODE=readonly ./scripts/e2e.sh

check: fmt vet build test lint imports web-check openapi-check   ## everything; needs Node (use check-go without it)

check-go: fmt vet build test lint imports   ## the Go-only subset, for a machine without Node

up:
	$(COMPOSE) up -d

observe:
	$(COMPOSE) --profile observability up -d

down:
	$(COMPOSE) --profile observability --profile oidc down

ps:
	$(COMPOSE) ps
