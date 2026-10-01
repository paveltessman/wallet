.DEFAULT_GOAL := help

BIN := bin/wallet
SQLC_OUT := internal/postgres/internal/sqlc

# .setenv gives TEST_DATABASE_URL to the integration tests on the host. CI sets it itself.
-include .setenv

# The service on the host reads config.env, but it reaches the database on localhost, not on the Compose host db.
HOST_ENV := set -a && . ./config.env && set +a && POSTGRES_HOST=localhost

.PHONY: help generate build run check fmt tidy up down psql clean

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

up: ## Start the database and the service with Docker Compose.
	test -f config.env || cp config.env.example config.env
	docker compose up -d --wait

down: ## Stop the stack. The volumes stay.
	docker compose down

psql: ## Open psql on the dev database.
	docker compose exec db sh -c 'psql -U "$$POSTGRES_USER" -d "$$POSTGRES_DB"'

clean: ## Remove the build output and the generated files.
	rm -rf bin $(SQLC_OUT)
