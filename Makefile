GO := go
TOOL_MOD := tools/go.mod
gotool = "$(GO)" tool -modfile="$(TOOL_MOD)" $(1)

.PHONY: lint fmt gofmt go-vet test install-hooks

# Run linter
lint:
	$(call gotool,golangci-lint) run ./...

# Format code
fmt:
	gofmt -s -w .

# Alias for fmt (required by hyperfleet-hooks)
gofmt: fmt

# Run go vet (required by hyperfleet-hooks)
go-vet:
	$(GO) vet ./...

# Run unit tests
test:
	$(GO) test -race ./...

# Install pre-commit hooks
install-hooks:
	pre-commit install
