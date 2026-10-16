# Developer entry points. Each target is one check, so check-runner can run them one at a time.
GO       ?= go
GOLANGCI_LINT ?= $(shell command -v golangci-lint || echo $(HOME)/go/bin/golangci-lint)
COMPOSE  ?= docker compose -f deploy/compose/docker-compose.yml --env-file deploy/compose/.env

.PHONY: fmt vet build test test-integration lint imports check up down ps

fmt:              ## fails if any file needs gofmt
	@out=$$(gofmt -l $$(git ls-files -co --exclude-standard '*.go')); if [ -n "$$out" ]; then echo "$$out"; exit 1; fi

vet:
	$(GO) vet ./...

build:
	$(GO) build ./...

test:             ## unit tests: no infrastructure needed
	$(GO) test -race ./...

test-integration: ## needs `make up`
	$(GO) test -race -tags integration ./...

lint:
	$(GOLANGCI_LINT) run ./...

imports:          ## a module never imports another module
	./scripts/check-module-imports.sh

check: fmt vet build test lint imports

up:
	$(COMPOSE) up -d

down:
	$(COMPOSE) down

ps:
	$(COMPOSE) ps
