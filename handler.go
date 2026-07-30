package hyperfleetlogger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
)

// Format selects the handler output format.
type Format int

const (
	// FormatJSON emits structured JSON logs.
	FormatJSON Format = iota
	// FormatText emits human-readable text logs.
	FormatText
)

// Option configures handler construction.
type Option func(*config)

type config struct {
	output             io.Writer
	hostname           string
	extraContextFields []ContextField
	level              slog.Level
	format             Format
	sanitize           bool
}

// WithLevel sets the minimum enabled log level.
func WithLevel(level slog.Level) Option {
	return func(cfg *config) { cfg.level = level }
}

// WithFormat selects the handler output format.
func WithFormat(format Format) Option {
	return func(cfg *config) { cfg.format = format }
}

// WithOutput directs log output to w. A nil writer is replaced with io.Discard.
func WithOutput(w io.Writer) Option {
	return func(cfg *config) {
		if w == nil {
			w = io.Discard
		}
		cfg.output = w
	}
}

// WithHostname overrides the hostname field added to every record.
func WithHostname(hostname string) Option {
	return func(cfg *config) { cfg.hostname = hostname }
}

// WithContextFields adds extra context-extracted fields alongside the
// package defaults, e.g. a request_id or op_id field for one component.
func WithContextFields(fields ...ContextField) Option {
	return func(cfg *config) {
		cfg.extraContextFields = append(cfg.extraContextFields, fields...)
	}
}

// WithSanitize enables control-character escaping in text output.
func WithSanitize() Option {
	return func(cfg *config) { cfg.sanitize = true }
}

// NewHandler returns a slog.Handler that adds component, version, and
// hostname to every record, extracts registered context fields, and
// attaches stack traces to error-level logs.
func NewHandler(component, version string, opts ...Option) slog.Handler {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	var enriched slog.Handler
	switch cfg.format {
	case FormatJSON:
		enriched = newJSONHandler(cfg.output, cfg.level, component, version, cfg.hostname)
	case FormatText:
		enriched = newTextHandler(cfg.output, cfg.level, component, version, cfg.hostname, cfg.sanitize)
	default:
		panic(fmt.Sprintf("hyperfleetlogger: unknown format: %d", cfg.format))
	}

	return &hyperfleetHandler{
		inner:         enriched,
		contextFields: deduplicateContextFields(defaultContextFields, cfg.extraContextFields),
	}
}

type groupedAttrs struct {
	groups []string
	attrs  []slog.Attr
}

type hyperfleetHandler struct {
	inner         slog.Handler
	groups        []string
	preAttrs      []groupedAttrs
	contextFields []ContextField
}

func defaultConfig() config {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	return config{
		output:   os.Stdout,
		level:    slog.LevelInfo,
		format:   FormatJSON,
		hostname: hostname,
	}
}

func (h *hyperfleetHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *hyperfleetHandler) Handle(ctx context.Context, r slog.Record) error {
	nr := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)

	for i := range h.contextFields {
		f := &h.contextFields[i]
		if f.Getter == nil || f.Name == "" {
			continue
		}
		if v, ok := f.Getter(ctx); ok {
			nr.AddAttrs(slog.Attr{Key: f.Name, Value: v})
		}
	}
	if r.Level >= slog.LevelError {
		nr.AddAttrs(slog.Any(FieldStackTrace, captureStackTrace()))
	}

	recordAttrs := make([]slog.Attr, 0, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		recordAttrs = append(recordAttrs, a)
		return true
	})

	nr.AddAttrs(h.mergedAttrs(recordAttrs)...)

	if err := h.inner.Handle(ctx, nr); err != nil {
		return fmt.Errorf("handle log record: %w", err)
	}
	return nil
}

func (h *hyperfleetHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	return &hyperfleetHandler{
		inner:         h.inner,
		groups:        h.groups,
		preAttrs:      append(slices.Clone(h.preAttrs), groupedAttrs{groups: h.groups, attrs: attrs}),
		contextFields: h.contextFields,
	}
}

func (h *hyperfleetHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &hyperfleetHandler{
		inner:         h.inner,
		groups:        append(slices.Clone(h.groups), name),
		preAttrs:      h.preAttrs,
		contextFields: h.contextFields,
	}
}

func (h *hyperfleetHandler) mergedAttrs(recordAttrs []slog.Attr) []slog.Attr {
	if len(h.preAttrs) == 0 {
		return wrapInGroups(h.groups, recordAttrs)
	}

	byDepth := make([][]slog.Attr, len(h.groups)+1)
	for _, ga := range h.preAttrs {
		byDepth[len(ga.groups)] = append(byDepth[len(ga.groups)], ga.attrs...)
	}
	byDepth[len(h.groups)] = append(byDepth[len(h.groups)], recordAttrs...)

	var nested []slog.Attr
	for d := len(h.groups); d >= 1; d-- {
		combined := slices.Concat(byDepth[d], nested)
		if len(combined) == 0 {
			nested = nil
			continue
		}
		nested = []slog.Attr{{Key: h.groups[d-1], Value: slog.GroupValue(combined...)}}
	}
	return append(byDepth[0], nested...)
}

func wrapInGroups(groups []string, attrs []slog.Attr) []slog.Attr {
	if len(groups) == 0 || len(attrs) == 0 {
		return attrs
	}
	for i := len(groups) - 1; i >= 0; i-- {
		attrs = []slog.Attr{{Key: groups[i], Value: slog.GroupValue(attrs...)}}
	}
	return attrs
}

func deduplicateContextFields(defaults, extras []ContextField) []ContextField {
	if len(extras) == 0 {
		return defaults
	}
	seen := make(map[string]struct{}, len(extras))
	for _, f := range extras {
		seen[f.Name] = struct{}{}
	}
	result := make([]ContextField, 0, len(defaults)+len(extras))
	for _, f := range defaults {
		if _, dup := seen[f.Name]; !dup {
			result = append(result, f)
		}
	}
	return append(result, extras...)
}

type pool[T any] struct{ p sync.Pool }

func newPool[T any](fn func() T) pool[T] {
	return pool[T]{p: sync.Pool{New: func() any { return fn() }}}
}

func (p *pool[T]) Get() T  { return p.p.Get().(T) }
func (p *pool[T]) Put(v T) { p.p.Put(v) }

var pcsPool = newPool(func() *[]uintptr {
	buf := make([]uintptr, 64)
	return &buf
})

func captureStackTrace() []string {
	const (
		maxFrames  = 15
		callerSkip = 3
	)
	bp := pcsPool.Get()
	pcs := *bp
	n := runtime.Callers(callerSkip, pcs)
	frames := runtime.CallersFrames(pcs[:n])

	trace := make([]string, 0, maxFrames)
	for {
		frame, more := frames.Next()
		if !strings.HasPrefix(frame.Function, "runtime.") &&
			!strings.HasPrefix(frame.Function, "testing.") &&
			!strings.HasPrefix(frame.Function, "log/slog.") {
			trace = append(trace, fmt.Sprintf("%s() %s:%d", frame.Function, filepath.Base(frame.File), frame.Line))
		}
		if !more || len(trace) == maxFrames {
			break
		}
	}
	pcsPool.Put(bp)
	return trace
}
