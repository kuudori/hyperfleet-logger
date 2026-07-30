package hyperfleetlogger

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/slogtest"
	"time"
)

const (
	testComponent      = "component-a"
	testRejectsUnknown = "rejects unknown input"
)

var requestIDKey = NewKey[string]("request_id")

// newTestLogger is a helper that returns a buffer and logger for test assertions.
func newTestLogger(opts ...Option) (*bytes.Buffer, *slog.Logger) {
	var buf bytes.Buffer
	defaults := []Option{
		WithOutput(&buf),
		WithHostname("test-host"),
	}
	defaults = append(defaults, opts...)
	return &buf, slog.New(NewHandler(testComponent, "v1.2.3", defaults...))
}

// parseJSONLog parses the first JSON line from the buffer.
func parseJSONLog(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	output := strings.TrimSpace(buf.String())
	if output == "" {
		t.Fatal("expected log output, got none")
	}
	var entry map[string]any
	if err := json.Unmarshal([]byte(output), &entry); err != nil {
		t.Fatalf("failed to parse JSON: %v\nraw=%s", err, output)
	}
	return entry
}

// --- Context helpers round-trip ---

func TestContextHelpersRoundTrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		with  func(context.Context, string) context.Context
		from  func(context.Context) (string, bool)
		value string
	}{
		{"trace id", WithTraceID, TraceIDFromContext, "trace-123"},
		{"span id", WithSpanID, SpanIDFromContext, "span-456"},
		{"resource type", WithResourceType, ResourceTypeFromContext, "managed-cluster"},
		{"resource id", WithResourceID, ResourceIDFromContext, "resource-abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.with(context.Background(), tt.value)
			got, ok := tt.from(ctx)
			if !ok {
				t.Fatal("expected value in context")
			}
			if got != tt.value {
				t.Fatalf("want %q, got %q", tt.value, got)
			}
		})
	}
}

func TestContextFromUnset(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	if _, ok := TraceIDFromContext(ctx); ok {
		t.Fatal("expected no trace_id in empty context")
	}
}

func TestKeySameNameSharesContextSlot(t *testing.T) {
	t.Parallel()
	key1 := NewKey[string]("op_id")
	key2 := NewKey[string]("op_id")

	ctx := Set(context.Background(), key1, "abc")
	got, ok := Get(ctx, key2)
	if !ok || got != "abc" {
		t.Fatalf("same-name keys should share context slot: ok=%v got=%q", ok, got)
	}
}

// --- Parse functions ---

func TestParseLevel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    slog.Level
		wantErr bool
	}{
		{levelDebug, levelDebug, slog.LevelDebug, false},
		{levelInfo, "INFO", slog.LevelInfo, false},
		{levelWarn, levelWarning, slog.LevelWarn, false},
		{levelError, levelError, slog.LevelError, false},
		{"defaults to info", "", slog.LevelInfo, false},
		{testRejectsUnknown, "verbose", slog.LevelInfo, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseLevel(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("want %v, got %v", tt.want, got)
			}
		})
	}
}

func TestParseFormat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    Format
		wantErr bool
	}{
		{formatJSON, formatJSON, FormatJSON, false},
		{formatText, "TEXT", FormatText, false},
		{"defaults to json", "", FormatJSON, false},
		{testRejectsUnknown, "yaml", FormatJSON, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseFormat(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("want %v, got %v", tt.want, got)
			}
		})
	}
}

func TestParseOutput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		want    io.Writer
		name    string
		input   string
		wantErr bool
	}{
		{os.Stdout, outputStdout, outputStdout, false},
		{os.Stderr, outputStderr, outputStderr, false},
		{os.Stdout, "defaults to stdout", "", false},
		{os.Stdout, testRejectsUnknown, "file", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseOutput(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("want %v, got %v", tt.want, got)
			}
		})
	}
}

// --- JSON output ---

func TestJSONOutput(t *testing.T) {
	t.Parallel()
	buf, logger := newTestLogger(
		WithLevel(slog.LevelDebug),
		WithContextFields(StringField(requestIDKey)),
	)

	ctx := context.Background()
	ctx = WithTraceID(ctx, "trace-123")
	ctx = WithSpanID(ctx, "span-456")
	ctx = WithResourceType(ctx, "cluster")
	ctx = WithResourceID(ctx, "cluster-789")
	ctx = Set(ctx, requestIDKey, "req-abc")

	logger.With("attempt", 3).InfoContext(ctx, "processing cluster")
	entry := parseJSONLog(t, buf)

	// Required fields present.
	for _, key := range []string{"timestamp", "level", "message", FieldComponent, FieldVersion, FieldHostname} {
		if _, ok := entry[key]; !ok {
			t.Errorf("missing required field %q", key)
		}
	}

	ts, ok := entry["timestamp"].(string)
	if !ok {
		t.Fatalf("timestamp field missing or wrong type: %v", entry["timestamp"])
	}
	if _, err := time.Parse(time.RFC3339Nano, ts); err != nil {
		t.Errorf("bad timestamp: %v", err)
	}

	// Key renames per spec.
	if v := entry["level"]; v != "info" {
		t.Errorf("level: want info, got %v", v)
	}
	if v := entry["message"]; v != "processing cluster" {
		t.Errorf("message: want processing cluster, got %v", v)
	}

	// HyperFleet base fields.
	if v := entry[FieldComponent]; v != testComponent {
		t.Errorf("component: want component-a, got %v", v)
	}
	if v := entry[FieldVersion]; v != "v1.2.3" {
		t.Errorf("version: want v1.2.3, got %v", v)
	}
	if v := entry[FieldHostname]; v != "test-host" {
		t.Errorf("hostname: want test-host, got %v", v)
	}

	// Context fields.
	if v := entry[FieldTraceID]; v != "trace-123" {
		t.Errorf("trace_id: want trace-123, got %v", v)
	}
	if v := entry[FieldSpanID]; v != "span-456" {
		t.Errorf("span_id: want span-456, got %v", v)
	}
	if v := entry[FieldResourceType]; v != "cluster" {
		t.Errorf("resource_type: want cluster, got %v", v)
	}
	if v := entry[FieldResourceID]; v != "cluster-789" {
		t.Errorf("resource_id: want cluster-789, got %v", v)
	}
	if v := entry["request_id"]; v != "req-abc" {
		t.Errorf("request_id: want req-abc, got %v", v)
	}

	// Extra With attr.
	if v := entry["attempt"]; v != float64(3) {
		t.Errorf("attempt: want 3, got %v", v)
	}
}

// --- Text output ---

func TestTextOutput(t *testing.T) {
	t.Parallel()
	buf, logger := newTestLogger(WithFormat(FormatText))

	ctx := WithResourceType(context.Background(), "managed-cluster")
	ctx = WithResourceID(ctx, "resource-123")
	logger.With("status", "ready").InfoContext(ctx, "processing cluster")

	output := strings.TrimSpace(buf.String())
	if output == "" {
		t.Fatal("expected output")
	}

	// Verify format: {timestamp} {LEVEL} [{component}] [{version}] [{hostname}] {message}
	pattern := regexp.MustCompile(
		`^\d{4}-\d{2}-\d{2}T\S+ INFO \[component-a\] \[v1\.2\.3\] \[test-host\] processing cluster`,
	)
	if !pattern.MatchString(output) {
		t.Fatalf("text format mismatch: %s", output)
	}
	for _, want := range []string{"resource_type=managed-cluster", "resource_id=resource-123", "status=ready"} {
		if !strings.Contains(output, want) {
			t.Errorf("missing %q in text output", want)
		}
	}
}

// --- Stack traces ---

func TestErrorStackTrace(t *testing.T) {
	t.Parallel()
	buf, logger := newTestLogger(WithLevel(slog.LevelDebug))
	logger.ErrorContext(context.Background(), "something broke")
	entry := parseJSONLog(t, buf)

	raw, ok := entry[FieldStackTrace]
	if !ok {
		t.Fatal("expected stack_trace on error log")
	}
	frames, ok := raw.([]any)
	if !ok {
		t.Fatalf("stack_trace type: want []any, got %T", raw)
	}
	if len(frames) == 0 {
		t.Fatal("expected at least one frame")
	}
	if len(frames) > 15 {
		t.Fatalf("expected at most 15 frames, got %d", len(frames))
	}
	for _, f := range frames {
		s, ok := f.(string)
		if !ok {
			t.Fatalf("frame type: want string, got %T", f)
		}
		if strings.Contains(s, "runtime.") || strings.Contains(s, "testing.") || strings.Contains(s, "log/slog.") {
			t.Errorf("filtered frame leaked: %s", s)
		}
	}
}

func TestInfoNoStackTrace(t *testing.T) {
	t.Parallel()
	buf, logger := newTestLogger()
	logger.InfoContext(context.Background(), "hello")
	entry := parseJSONLog(t, buf)
	if _, ok := entry[FieldStackTrace]; ok {
		t.Error("stack_trace should be absent on info log")
	}
}

func TestTextErrorStackTrace(t *testing.T) {
	t.Parallel()
	buf, logger := newTestLogger(WithFormat(FormatText), WithLevel(slog.LevelError))
	logger.ErrorContext(context.Background(), "fail")

	output := buf.String()
	if !strings.Contains(output, "stack_trace:") {
		t.Fatal("expected stack_trace section in text output")
	}
}

// --- Level filtering ---

func TestLevelFiltering(t *testing.T) {
	t.Parallel()
	buf, logger := newTestLogger() // default info
	logger.DebugContext(context.Background(), "suppressed")
	if buf.Len() != 0 {
		t.Fatalf("debug should be filtered at info level, got %s", buf.String())
	}
}

func TestLevelFilteringWithOption(t *testing.T) {
	t.Parallel()
	buf, logger := newTestLogger(WithLevel(slog.LevelWarn))
	logger.InfoContext(context.Background(), "suppressed")
	if buf.Len() != 0 {
		t.Fatal("info should be filtered at warn level")
	}
	logger.WarnContext(context.Background(), "visible")
	if buf.Len() == 0 {
		t.Fatal("warn should be emitted at warn level")
	}
}

// --- WithAttrs / WithGroup ---

func TestWithAttrsPreservesFields(t *testing.T) {
	t.Parallel()
	buf, logger := newTestLogger()
	logger.With("key", "value").InfoContext(context.Background(), "test")
	entry := parseJSONLog(t, buf)
	if v := entry["key"]; v != "value" {
		t.Fatalf("want value, got %v", v)
	}
	// Base fields still present.
	if v := entry[FieldComponent]; v != testComponent {
		t.Fatalf("component lost after WithAttrs")
	}
}

func TestWithGroupJSON(t *testing.T) {
	t.Parallel()
	buf, logger := newTestLogger()
	ctx := WithTraceID(context.Background(), "trace-grp")
	logger.WithGroup("http").InfoContext(ctx, "done", slog.Int("status", 201))
	entry := parseJSONLog(t, buf)

	// User attrs nested under group.
	group, ok := entry["http"].(map[string]any)
	if !ok {
		t.Fatalf("expected http group, got %T", entry["http"])
	}
	if v := group["status"]; v != float64(201) {
		t.Fatalf("want 201, got %v", v)
	}

	// Enrichment fields must stay at root level, NOT inside the group.
	if v := entry[FieldComponent]; v != testComponent {
		t.Fatalf("component should be at root, got %v", v)
	}
	if v := entry[FieldVersion]; v != "v1.2.3" {
		t.Fatalf("version should be at root, got %v", v)
	}
	if v := entry[FieldHostname]; v != "test-host" {
		t.Fatalf("hostname should be at root, got %v", v)
	}
	// Context fields must also stay at root.
	if v := entry[FieldTraceID]; v != "trace-grp" {
		t.Fatalf("trace_id should be at root, got %v", v)
	}
	// Enrichment must not appear inside the group.
	if _, ok := group[FieldComponent]; ok {
		t.Fatal("component must not be nested inside group")
	}
}

// --- Options ---

func TestWithHostname(t *testing.T) {
	t.Parallel()
	buf, logger := newTestLogger(WithHostname("custom-host"))
	logger.InfoContext(context.Background(), "test")
	entry := parseJSONLog(t, buf)
	if v := entry[FieldHostname]; v != "custom-host" {
		t.Fatalf("want custom-host, got %v", v)
	}
}

func TestDefaultHostname(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(NewHandler("c", "v", WithOutput(&buf)))
	logger.InfoContext(context.Background(), "test")
	entry := parseJSONLog(t, &buf)
	hostname, ok := entry[FieldHostname].(string)
	if !ok || hostname == "" {
		t.Fatal("expected non-empty hostname from os.Hostname()")
	}
}

// --- ContextField extensibility ---

func TestWithContextFieldsExtension(t *testing.T) {
	t.Parallel()
	buf, logger := newTestLogger(
		WithContextFields(StringField(requestIDKey)),
	)

	ctx := Set(context.Background(), requestIDKey, "req-999")
	logger.InfoContext(ctx, "test")
	entry := parseJSONLog(t, buf)

	if v := entry["request_id"]; v != "req-999" {
		t.Fatalf("want req-999, got %v", v)
	}
}

// --- WithAttrs / WithGroup interleaving ---

func TestWithGroupAttrsInterleavingJSON(t *testing.T) {
	t.Parallel()
	// logger.With("k1","v1").WithGroup("g").With("k2","v2").Info(...)
	// Per slog contract: k1 at root, k2 under "g".
	buf, logger := newTestLogger()
	logger.With("k1", "v1").WithGroup("g").With("k2", "v2").
		InfoContext(context.Background(), "test")
	entry := parseJSONLog(t, buf)

	// k1 must be at root.
	if v := entry["k1"]; v != "v1" {
		t.Fatalf("k1: want v1, got %v", v)
	}
	// k2 must be under group "g".
	group, ok := entry["g"].(map[string]any)
	if !ok {
		t.Fatalf("expected group g, got %T", entry["g"])
	}
	if v := group["k2"]; v != "v2" {
		t.Fatalf("g.k2: want v2, got %v", v)
	}
	// k1 must NOT be inside group.
	if _, ok := group["k1"]; ok {
		t.Fatal("k1 must not be nested inside group g")
	}
	// Enrichment at root.
	if v := entry[FieldComponent]; v != testComponent {
		t.Fatalf("component should be at root, got %v", v)
	}
}

func TestWithGroupAttrsInterleavingText(t *testing.T) {
	t.Parallel()
	buf, logger := newTestLogger(WithFormat(FormatText))
	logger.With("k1", "v1").WithGroup("g").With("k2", "v2").
		InfoContext(context.Background(), "test")
	output := buf.String()

	// k1 at root level (no dot prefix).
	if !strings.Contains(output, "k1=v1") {
		t.Fatalf("missing k1=v1 in output: %s", output)
	}
	// k2 under group g.
	if !strings.Contains(output, "g.k2=v2") {
		t.Fatalf("missing g.k2=v2 in output: %s", output)
	}
	// k1 must NOT be prefixed with g.
	if strings.Contains(output, "g.k1=") {
		t.Fatalf("k1 must not be nested under g: %s", output)
	}
}

// --- Error path tests ---

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) {
	return 0, errors.New("disk full")
}

func TestJSONHandlerWriteError(t *testing.T) {
	t.Parallel()
	h := NewHandler("c", "v", WithOutput(failWriter{}))
	err := h.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "test", 0))
	if err == nil {
		t.Fatal("expected error from failing writer")
	}
	if !strings.Contains(err.Error(), "handle log record") {
		t.Fatalf("expected wrapped error, got: %v", err)
	}
}

func TestTextHandlerWriteError(t *testing.T) {
	t.Parallel()
	h := NewHandler("c", "v", WithOutput(failWriter{}), WithFormat(FormatText))
	err := h.Handle(context.Background(), slog.NewRecord(time.Now(), slog.LevelInfo, "test", 0))
	if err == nil {
		t.Fatal("expected error from failing writer")
	}
	if !strings.Contains(err.Error(), "write text log record") {
		t.Fatalf("expected wrapped error, got: %v", err)
	}
}

func TestTextHandlerConcurrentWrites(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := slog.New(NewHandler("c", "v",
		WithOutput(&buf),
		WithFormat(FormatText),
		WithHostname("test-host"),
	))

	const goroutines = 10
	const logsPerGoroutine = 50
	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			for range logsPerGoroutine {
				logger.InfoContext(context.Background(), "concurrent")
			}
		})
	}
	wg.Wait()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	want := goroutines * logsPerGoroutine
	if len(lines) != want {
		t.Fatalf("expected %d lines, got %d", want, len(lines))
	}
}

// --- Sanitization ---

func TestSanitizeTextControlChars(t *testing.T) {
	t.Parallel()
	buf, logger := newTestLogger(WithFormat(FormatText))
	logger.InfoContext(context.Background(), "test", "val", "a\x00b\x07c\x0bd\x0ce")
	output := buf.String()

	for _, want := range []string{`\x00`, `\a`, `\v`, `\f`} {
		if !strings.Contains(output, want) {
			t.Errorf("missing escaped %q in output: %s", want, output)
		}
	}
	for _, raw := range []string{"\x00", "\x07", "\x0b", "\x0c"} {
		if strings.Contains(output, raw) {
			t.Errorf("raw control char %q not escaped in output", raw)
		}
	}
}

func TestTextNewlineEscapedInMessage(t *testing.T) {
	t.Parallel()
	buf, logger := newTestLogger(WithFormat(FormatText))
	logger.InfoContext(context.Background(), "line1\nline2\rline3")
	output := buf.String()

	// The log record must be a single line (plus trailing newline).
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 log line, got %d:\n%s", len(lines), output)
	}
	if !strings.Contains(output, `\n`) || !strings.Contains(output, `\r`) {
		t.Fatalf("expected escaped newlines in output: %s", output)
	}
}

func TestTextStackTraceSanitization(t *testing.T) {
	t.Parallel()
	buf, logger := newTestLogger(WithFormat(FormatText), WithLevel(slog.LevelError))
	logger.ErrorContext(context.Background(), "fail")
	output := buf.String()

	if !strings.Contains(output, "stack_trace:") {
		t.Fatal("expected stack_trace section")
	}
	// Real stack frames shouldn't contain raw control characters.
	// Count lines in the stack_trace section - each frame should be on its own line.
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		if strings.Contains(line, "\x00") || strings.Contains(line, "\x1b") {
			t.Errorf("line %d contains unsanitized control char: %q", i, line)
		}
	}
}

// --- ReplaceAttr groups ---

func TestReplaceAttrRespectsGroups(t *testing.T) {
	t.Parallel()
	buf, logger := newTestLogger()
	// User attrs with slog built-in key names inside a group must NOT be transformed.
	logger.WithGroup("data").InfoContext(context.Background(), "test",
		"level", "HIGH",
		"msg", "hello",
	)
	entry := parseJSONLog(t, buf)

	group, ok := entry["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data group, got %T", entry["data"])
	}
	// "level" inside group must NOT be lowercased.
	if v := group["level"]; v != "HIGH" {
		t.Fatalf("data.level: want HIGH, got %v", v)
	}
	// "msg" inside group must NOT be renamed to "message".
	if v := group["msg"]; v != "hello" {
		t.Fatalf("data.msg: want hello, got %v", v)
	}
	if _, ok := group["message"]; ok {
		t.Fatal("data.msg was incorrectly renamed to data.message")
	}
	// Root-level transformations still work.
	if v := entry["level"]; v != "info" {
		t.Fatalf("root level: want info, got %v", v)
	}
	if _, ok := entry["message"]; !ok {
		t.Fatal("root msg was not renamed to message")
	}
}

// --- Format validation ---

func TestUnknownFormatPanics(t *testing.T) {
	t.Parallel()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic for unknown format")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "unknown format") {
			t.Fatalf("unexpected panic value: %v", r)
		}
	}()
	NewHandler("c", "v", WithFormat(Format(42)))
}

// --- Context field deduplication ---

func TestWithContextFieldsDeduplicate(t *testing.T) {
	t.Parallel()
	customGetter := func(ctx context.Context) (slog.Value, bool) {
		return slog.StringValue("custom-trace"), true
	}
	buf, logger := newTestLogger(
		WithContextFields(ContextField{Name: FieldTraceID, Getter: customGetter}),
	)

	ctx := WithTraceID(context.Background(), "default-trace")
	logger.InfoContext(ctx, "test")
	entry := parseJSONLog(t, buf)

	if v := entry[FieldTraceID]; v != "custom-trace" {
		t.Fatalf("trace_id: want custom-trace (custom override), got %v", v)
	}
}

// --- Duplicate group keys ---

func TestDuplicateGroupKeysJSON(t *testing.T) {
	t.Parallel()
	buf, logger := newTestLogger()
	// WithGroup("g").With("k1","v1").Info("msg","k2","v2")
	// Must produce a single "g" key with both k1 and k2 inside.
	logger.WithGroup("g").With("k1", "v1").
		InfoContext(context.Background(), "test", "k2", "v2")
	entry := parseJSONLog(t, buf)

	g, ok := entry["g"].(map[string]any)
	if !ok {
		t.Fatalf("expected group g, got %T: %v", entry["g"], entry)
	}
	if v := g["k1"]; v != "v1" {
		t.Fatalf("g.k1: want v1, got %v", v)
	}
	if v := g["k2"]; v != "v2" {
		t.Fatalf("g.k2: want v2, got %v", v)
	}
}

func TestDuplicateGroupKeysComplex(t *testing.T) {
	t.Parallel()
	buf, logger := newTestLogger()
	// With("root","r").WithGroup("g").With("k1","v1").Info("msg","k2","v2")
	// "root" at root, both k1 and k2 under single "g" key.
	logger.With("root", "r").WithGroup("g").With("k1", "v1").
		InfoContext(context.Background(), "test", "k2", "v2")
	entry := parseJSONLog(t, buf)

	if v := entry["root"]; v != "r" {
		t.Fatalf("root: want r, got %v", v)
	}
	g, ok := entry["g"].(map[string]any)
	if !ok {
		t.Fatalf("expected group g, got %T", entry["g"])
	}
	if v := g["k1"]; v != "v1" {
		t.Fatalf("g.k1: want v1, got %v", v)
	}
	if v := g["k2"]; v != "v2" {
		t.Fatalf("g.k2: want v2, got %v", v)
	}
}

func TestNestedGroupAfterAttrs(t *testing.T) {
	t.Parallel()
	buf, logger := newTestLogger()
	// WithGroup("g1").With("a",1).WithGroup("g2").With("b",2).Info("msg","c",3)
	// Must produce a single "g1" key containing both "a" and nested "g2".
	logger.WithGroup("g1").With("a", 1).WithGroup("g2").With("b", 2).
		InfoContext(context.Background(), "test", "c", 3)
	entry := parseJSONLog(t, buf)

	g1, ok := entry["g1"].(map[string]any)
	if !ok {
		t.Fatalf("expected group g1, got %T: %v", entry["g1"], entry)
	}
	if v := g1["a"]; v != float64(1) {
		t.Fatalf("g1.a: want 1, got %v", v)
	}
	g2, ok := g1["g2"].(map[string]any)
	if !ok {
		t.Fatalf("expected g1.g2, got %T: %v", g1["g2"], g1)
	}
	if v := g2["b"]; v != float64(2) {
		t.Fatalf("g1.g2.b: want 2, got %v", v)
	}
	if v := g2["c"]; v != float64(3) {
		t.Fatalf("g1.g2.c: want 3, got %v", v)
	}
}

// --- Nested groups ---

func TestNestedWithGroupJSON(t *testing.T) {
	t.Parallel()
	buf, logger := newTestLogger()
	logger.WithGroup("a").WithGroup("b").
		InfoContext(context.Background(), "nested", slog.String("k", "v"))
	entry := parseJSONLog(t, buf)

	// k must be nested under a.b.
	a, ok := entry["a"].(map[string]any)
	if !ok {
		t.Fatalf("expected group a, got %T", entry["a"])
	}
	b, ok := a["b"].(map[string]any)
	if !ok {
		t.Fatalf("expected group a.b, got %T", a["b"])
	}
	if v := b["k"]; v != "v" {
		t.Fatalf("a.b.k: want v, got %v", v)
	}
	// Enrichment at root.
	if v := entry[FieldComponent]; v != testComponent {
		t.Fatalf("component should be at root, got %v", v)
	}
}

// --- slogtest conformance ---

func TestSlogtestConformance(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	slogtest.Run(t,
		func(_ *testing.T) slog.Handler {
			buf.Reset()
			return NewHandler("c", "v", WithOutput(&buf), WithHostname("h"), WithLevel(slog.LevelDebug))
		},
		func(t *testing.T) map[string]any {
			t.Helper()
			line := buf.String()
			if line == "" {
				return nil
			}
			var m map[string]any
			if err := json.Unmarshal([]byte(line), &m); err != nil {
				t.Fatalf("parse JSON: %v\nraw=%s", err, line)
			}
			if v, ok := m["timestamp"]; ok {
				m[slog.TimeKey] = v
				delete(m, "timestamp")
			}
			if v, ok := m["message"]; ok {
				m[slog.MessageKey] = v
				delete(m, "message")
			}
			return m
		},
	)
}
