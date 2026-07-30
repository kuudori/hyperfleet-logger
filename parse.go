package hyperfleetlogger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

const (
	levelDebug   = "debug"
	levelInfo    = "info"
	levelWarn    = "warn"
	levelWarning = "warning"
	levelError   = "error"

	formatJSON = "json"
	formatText = "text"

	outputStdout = "stdout"
	outputStderr = "stderr"
)

// ParseLevel converts a level string to slog.Level.
// Accepts debug, info, warn, warning, error (case-insensitive).
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case levelDebug:
		return slog.LevelDebug, nil
	case levelInfo, "":
		return slog.LevelInfo, nil
	case levelWarn, levelWarning:
		return slog.LevelWarn, nil
	case levelError:
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unknown log level: %q (valid: debug, info, warn, warning, error)", s)
	}
}

// ParseFormat converts a format string to a Format value.
// Accepts json, text (case-insensitive). Empty defaults to JSON.
func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case formatJSON, "":
		return FormatJSON, nil
	case formatText:
		return FormatText, nil
	default:
		return FormatJSON, fmt.Errorf("unknown log format: %q (valid: json, text)", s)
	}
}

// ParseOutput converts an output string to an io.Writer.
// Accepts stdout, stderr (case-insensitive). Empty defaults to stdout.
func ParseOutput(s string) (io.Writer, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", outputStdout:
		return os.Stdout, nil
	case outputStderr:
		return os.Stderr, nil
	default:
		return os.Stdout, fmt.Errorf("unknown log output: %q (valid: stdout, stderr)", s)
	}
}
