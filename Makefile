.DEFAULT_GOAL := help

BIN := bin/wallet
SQLC_OUT := internal/postgres/internal/sqlc

# .setenv gives TEST_DATABASE_URL to the integration tests on the host. CI sets it itself.
-include .setenv

# The service on the host reads config.env, but it reaches the database on localhost, not on the Compose host db.
HOST_ENV := set -a && . ./config.env && set +a && POSTGRES_HOST=localhost

# Compose reads config.env for the interpolation in docker-compose.yml, not only for the containers.
COMPOSE := docker compose --env-file config.env

# make load seeds this wallet and sends the load to it.
LOAD_WALLET_ID := 00000000-0000-4000-8000-000000000001

# make load-wallets seeds this number of wallets and sends the load to random wallets.
LOAD_WALLET_COUNT := 10000

.PHONY: help generate build run check fmt tidy migrate migrate-status up down logs psql load load-wallets clean

help: ## List the targets.
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "} {printf "  %-16s %s\n", $$1, $$2}'

generate: ## Run sqlc generate.
	go tool sqlc generate

build: ## Build the binary into ./bin.
	go build -o $(BIN) ./cmd/wallet

run: ## Run the service on the host.
	$(HOST_ENV) go run ./cmd/wallet

check: ## Run all the CI checks: go mod tidy -diff, go vet, the tests, the lint.
	go mod tidy -diff
	go vet ./...
	go tool gotestsum -- -race ./...
	go tool golangci-lint run

fmt: ## Format the Go code.
	go tool golangci-lint fmt

tidy: ## Tidy go.mod and go.sum.
	go mod tidy

migrate: ## Apply the pending migrations.
	$(HOST_ENV) go run ./cmd/wallet migrate up

migrate-status: ## Show the applied migrations.
	$(HOST_ENV) go run ./cmd/wallet migrate status

up: ## Build and start the database and the service with Docker Compose.
	test -f config.env || cp config.env.example config.env
	$(COMPOSE) up -d --wait

down: ## Stop the stack. The volumes stay.
	$(COMPOSE) down

logs: ## Follow the service logs.
	$(COMPOSE) logs -f app

psql: ## Open psql on the dev database.
	$(COMPOSE) exec db sh -c 'psql -U "$$POSTGRES_USER" -d "$$POSTGRES_DB"'

load: up ## Run the k6 load test against the local stack.
	$(COMPOSE) exec -T db sh -c 'psql -v ON_ERROR_STOP=1 -v id=$(LOAD_WALLET_ID) -U "$$POSTGRES_USER" -d "$$POSTGRES_DB"' < load/seed.sql
	$(COMPOSE) run --rm -e WALLET_ID=$(LOAD_WALLET_ID) k6

load-wallets: up ## Run the k6 load test on random wallets against the local stack.
	$(COMPOSE) exec -T db sh -c 'psql -v ON_ERROR_STOP=1 -v count=$(LOAD_WALLET_COUNT) -U "$$POSTGRES_USER" -d "$$POSTGRES_DB"' < load/seed-wallets.sql
	$(COMPOSE) run --rm -e WALLET_COUNT=$(LOAD_WALLET_COUNT) k6 run /scripts/wallets.js

clean: ## Remove the build output and the generated files.
	rm -rf bin $(SQLC_OUT)
