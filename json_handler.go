package hyperfleetlogger

import (
	"io"
	"log/slog"
	"strings"
)

func newJSONHandler(w io.Writer, level slog.Level, component, version, hostname string) slog.Handler {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) > 0 {
				return a
			}
			switch a.Key {
			case slog.TimeKey:
				a.Key = "timestamp"
			case slog.LevelKey:
				a.Value = slog.StringValue(strings.ToLower(a.Value.String()))
			case slog.MessageKey:
				a.Key = "message"
			}
			return a
		},
	}).WithAttrs([]slog.Attr{
		slog.String(FieldComponent, component),
		slog.String(FieldVersion, version),
		slog.String(FieldHostname, hostname),
	})

	return h
}
