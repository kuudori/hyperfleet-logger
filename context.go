package hyperfleetlogger

import (
	"context"
	"log/slog"
)

// Key is a type-safe context key. Use NewKey to create one and
// Set/Get to store and retrieve values without type assertions.
type Key[T any] struct{ name string }

// NewKey creates a named context key for values of type T.
func NewKey[T any](name string) Key[T] { return Key[T]{name: name} }

// Set stores val in ctx under key.
func Set[T any](ctx context.Context, key Key[T], val T) context.Context {
	return context.WithValue(ctx, key, val)
}

// Get retrieves the value stored under key, if present.
func Get[T any](ctx context.Context, key Key[T]) (T, bool) {
	v, ok := ctx.Value(key).(T)
	return v, ok
}

// Exported typed keys for the standard HyperFleet context fields.
var (
	TraceIDKey      = NewKey[string](FieldTraceID)
	SpanIDKey       = NewKey[string](FieldSpanID)
	ResourceTypeKey = NewKey[string](FieldResourceType)
	ResourceIDKey   = NewKey[string](FieldResourceID)
)

// ContextField maps a log field name to a function that extracts its
// slog.Value from a context. Register extra fields via WithContextFields.
type ContextField struct {
	Getter func(context.Context) (slog.Value, bool)
	Name   string
}

// FieldFromKey builds a ContextField from a typed Key, using toValue
// to convert the stored value to an slog.Value.
func FieldFromKey[T any](key Key[T], toValue func(T) slog.Value) ContextField {
	return ContextField{
		Name: key.name,
		Getter: func(ctx context.Context) (slog.Value, bool) {
			v, ok := Get(ctx, key)
			if !ok {
				return slog.Value{}, false
			}
			return toValue(v), true
		},
	}
}

// StringField builds a ContextField from a Key[string].
func StringField(key Key[string]) ContextField {
	return FieldFromKey(key, slog.StringValue)
}

var defaultContextFields = []ContextField{
	StringField(TraceIDKey),
	StringField(SpanIDKey),
	StringField(ResourceTypeKey),
	StringField(ResourceIDKey),
}

// WithTraceID returns a context carrying the given trace ID.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return Set(ctx, TraceIDKey, traceID)
}

// TraceIDFromContext extracts the trace ID, if present.
func TraceIDFromContext(ctx context.Context) (string, bool) {
	return Get(ctx, TraceIDKey)
}

// WithSpanID returns a context carrying the given span ID.
func WithSpanID(ctx context.Context, spanID string) context.Context {
	return Set(ctx, SpanIDKey, spanID)
}

// SpanIDFromContext extracts the span ID, if present.
func SpanIDFromContext(ctx context.Context) (string, bool) {
	return Get(ctx, SpanIDKey)
}

// WithResourceType returns a context carrying the given resource type.
func WithResourceType(ctx context.Context, resourceType string) context.Context {
	return Set(ctx, ResourceTypeKey, resourceType)
}

// ResourceTypeFromContext extracts the resource type, if present.
func ResourceTypeFromContext(ctx context.Context) (string, bool) {
	return Get(ctx, ResourceTypeKey)
}

// WithResourceID returns a context carrying the given resource ID.
func WithResourceID(ctx context.Context, resourceID string) context.Context {
	return Set(ctx, ResourceIDKey, resourceID)
}

// ResourceIDFromContext extracts the resource ID, if present.
func ResourceIDFromContext(ctx context.Context) (string, bool) {
	return Get(ctx, ResourceIDKey)
}
