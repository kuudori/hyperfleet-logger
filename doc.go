// Package hyperfleetlogger provides shared slog handler configuration and
// context helpers that match the HyperFleet logging specification.
//
// Typical usage:
//
//	handler := hyperfleetlogger.NewHandler("my-service", "v1.2.3")
//	slog.SetDefault(slog.New(handler))
//	ctx := hyperfleetlogger.WithResourceType(context.Background(), "cluster")
//	ctx = hyperfleetlogger.WithResourceID(ctx, "cluster-1")
//	slog.InfoContext(ctx, "processing cluster")
//
// Custom context fields use generic typed keys so values are logged with
// their native types (int, bool, duration, etc.) instead of being stringified:
//
//	var RetryCountKey = hyperfleetlogger.NewKey[int]("retry_count")
//
//	// Store in context
//	ctx = hyperfleetlogger.Set(ctx, RetryCountKey, 3)
//
//	// Register as a context field
//	handler := hyperfleetlogger.NewHandler("my-service", "v1.2.3",
//	    hyperfleetlogger.WithContextFields(
//	        hyperfleetlogger.FieldFromKey(RetryCountKey, slog.IntValue),
//	    ),
//	)
//
// Stack traces on error-level records are opt-in, not automatic - see
// WithStackTrace.
//
// The package intentionally stays thin: it provides handler construction,
// context field helpers, and field name constants while callers use stdlib
// slog directly.
package hyperfleetlogger
