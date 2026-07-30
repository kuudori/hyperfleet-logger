# HyperFleet Logger

Shared `log/slog` handler and context helpers for all HyperFleet Go components. Provides a single `NewHandler` call that enforces the [HyperFleet Logging Specification](https://github.com/openshift-hyperfleet/architecture/blob/main/hyperfleet/standards/logging-specification.md) - structured JSON (or human-readable text) with automatic enrichment fields, context-propagated trace/resource IDs, and stack traces on errors.

HyperFleet components (API, Sentinel, Adapter) adopt this library to keep logs uniform across the platform. Callers use stdlib `slog` directly; this package only configures the handler.

## Features

- **Automatic enrichment** - `component`, `version`, and `hostname` on every record, at root level regardless of `WithGroup` nesting
- **Context field extraction** - `trace_id`, `span_id`, `resource_type`, `resource_id` pulled from `context.Context` automatically
- **Extensible context fields** - register component-specific fields (e.g. `request_id`, `event_id`, `cluster_id`) via `WithContextFields`
- **Stack traces on errors** - `ERROR`-level and above automatically include a filtered Go stack trace; use `WARN` for expected/handled errors that don't need traces
- **Dual format** - JSON (default) for production, human-readable text for local development
- **Zero dependencies** - stdlib only (`log/slog`, `os`, `io`, `context`)
- **Environment-driven config** - `ParseLevel`, `ParseFormat`, `ParseOutput` parse `HYPERFLEET_LOG_LEVEL`, `HYPERFLEET_LOG_FORMAT`, `HYPERFLEET_LOG_OUTPUT` strings

## Installation

```bash
go get github.com/openshift-hyperfleet/hyperfleet-logger
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log/slog"
    "os"

    hfl "github.com/openshift-hyperfleet/hyperfleet-logger"
)

func main() {
    level, err := hfl.ParseLevel(os.Getenv("HYPERFLEET_LOG_LEVEL"))
    if err != nil {
        fmt.Fprintf(os.Stderr, "invalid HYPERFLEET_LOG_LEVEL: %v\n", err)
    }
    format, err := hfl.ParseFormat(os.Getenv("HYPERFLEET_LOG_FORMAT"))
    if err != nil {
        fmt.Fprintf(os.Stderr, "invalid HYPERFLEET_LOG_FORMAT: %v\n", err)
    }
    output, err := hfl.ParseOutput(os.Getenv("HYPERFLEET_LOG_OUTPUT"))
    if err != nil {
        fmt.Fprintf(os.Stderr, "invalid HYPERFLEET_LOG_OUTPUT: %v\n", err)
    }

    handler := hfl.NewHandler("sentinel", "v1.2.3",
        hfl.WithLevel(level),
        hfl.WithFormat(format),
        hfl.WithOutput(output),
    )
    slog.SetDefault(slog.New(handler))

    ctx := hfl.WithResourceType(context.Background(), "cluster")
    ctx = hfl.WithResourceID(ctx, "cls-abc-123")
    ctx = hfl.WithTraceID(ctx, "4bf92f3577b34da6a3ce929d0e0e4736")

    slog.InfoContext(ctx, "reconciling cluster")
}
```

**JSON output:**

```json
{
  "timestamp": "2025-01-15T10:30:00.000Z",
  "level": "info",
  "message": "reconciling cluster",
  "component": "sentinel",
  "version": "v1.2.3",
  "hostname": "pod-abc",
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
  "resource_type": "cluster",
  "resource_id": "cls-abc-123"
}
```

**Text output** (`HYPERFLEET_LOG_FORMAT=text`):

```text
2025-01-15T10:30:00.000Z INFO [sentinel] [v1.2.3] [pod-abc] reconciling cluster trace_id=4bf92f3577b34da6a3ce929d0e0e4736 resource_type=cluster resource_id=cls-abc-123
```

## API Reference

### Handler Construction

```go
func NewHandler(component, version string, opts ...Option) slog.Handler
```

Creates a `slog.Handler` with automatic enrichment. Options:

| Option | Default | Description |
|--------|---------|-------------|
| `WithLevel(slog.Level)` | `slog.LevelInfo` | Minimum enabled log level |
| `WithFormat(Format)` | `FormatJSON` | Output format (`FormatJSON`, `FormatText`) |
| `WithOutput(io.Writer)` | `os.Stdout` | Log output destination |
| `WithHostname(string)` | `os.Hostname()` | Override the `hostname` field |
| `WithContextFields(...ContextField)` | built-in set | Register additional context-extracted fields |

### Context Helpers

Set fields on `context.Context` for automatic extraction by the handler:

```go
ctx = hfl.WithTraceID(ctx, "4bf92f3577b34da6a3ce929d0e0e4736")
ctx = hfl.WithSpanID(ctx, "00f067aa0ba902b7")
ctx = hfl.WithResourceType(ctx, "cluster")
ctx = hfl.WithResourceID(ctx, "cls-abc-123")
```

Corresponding getters (`TraceIDFromContext`, `SpanIDFromContext`, etc.) are available for use outside logging.

### Custom Context Fields

Register component-specific fields that are automatically extracted from the context:

```go
var reqIDKey = hfl.NewKey[string]("request_id")

handler := hfl.NewHandler("api", "v1.4.0",
    hfl.WithContextFields(
        hfl.StringField(reqIDKey),
    ),
)

ctx = hfl.Set(ctx, reqIDKey, "req-123")
```

### Parsers

Parse environment variable strings into typed values. All return a sensible default on empty input.

```go
func ParseLevel(s string) (slog.Level, error)    // debug, info, warn, error
func ParseFormat(s string) (Format, error)        // json, text
func ParseOutput(s string) (io.Writer, error)     // stdout, stderr
```

### Field Name Constants

Exported constants for the standard field names, useful for tests or log aggregation queries:

```go
hfl.FieldComponent    // "component"
hfl.FieldVersion      // "version"
hfl.FieldHostname     // "hostname"
hfl.FieldTraceID      // "trace_id"
hfl.FieldSpanID       // "span_id"
hfl.FieldResourceType // "resource_type"
hfl.FieldResourceID   // "resource_id"
hfl.FieldStackTrace   // "stack_trace"
```

## Error Stack Traces

`ERROR`-level and above automatically include a `stack_trace` field with a filtered call stack (slog/runtime/testing internals excluded). For expected or handled errors (validation failures, 404s, retries) use `WARN` level to avoid the stack trace overhead:

```json
{
  "level": "error",
  "message": "failed to update cluster",
  "component": "api",
  "stack_trace": [
    "main.handleRequest() server.go:142",
    "main.main() main.go:28"
  ]
}
```

## License

Apache 2.0 - see [LICENSE](LICENSE) for details.
