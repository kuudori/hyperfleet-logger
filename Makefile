GO := go
TOOL_MOD := tools/go.mod
gotool = "$(GO)" tool -modfile="$(TOOL_MOD)" $(1)

.PHONY: help lint fmt gofmt go-vet test install-hooks

.DEFAULT_GOAL := help

help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ { printf "  %-15s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

lint: ## Run golangci-lint
	$(call gotool,golangci-lint) run ./...

fmt: ## Format code with gofmt
	gofmt -s -w .

gofmt: fmt ## Alias for fmt (required by hyperfleet-hooks)

go-vet: ## Run go vet (required by hyperfleet-hooks)
	"$(GO)" vet ./...

test: ## Run unit tests
	"$(GO)" test -race ./...

install-hooks: ## Install pre-commit hooks
	pre-commit install
