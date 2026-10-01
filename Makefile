.DEFAULT_GOAL := help

BIN := bin/wallet

.PHONY: help build run check fmt tidy clean

help: ## List the targets.
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "} {printf "  %-16s %s\n", $$1, $$2}'

build: ## Build the binary into ./bin.
	go build -o $(BIN) ./cmd/wallet

run: ## Run the service on the host.
	go run ./cmd/wallet

check: ## Run all the CI checks: go mod tidy -diff, go vet, the tests, the lint.
	go mod tidy -diff
	go vet ./...
	go tool gotestsum -- -race ./...
	go tool golangci-lint run

fmt: ## Format the Go code.
	go tool golangci-lint fmt

tidy: ## Tidy go.mod and go.sum.
	go mod tidy

clean: ## Remove the build output.
	rm -rf bin
