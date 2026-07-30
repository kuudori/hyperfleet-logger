package hyperfleetlogger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

// textHandler formats log records as:
//
//	{timestamp} {LEVEL} [{component}] [{version}] [{hostname}] {message} {key=value}...
type textHandler struct {
	w            io.Writer
	mu           *sync.Mutex
	preformatted string
	prefix       string
	component    string
	version      string
	hostname     string
	preStack     []string
	level        slog.Level
	sanitize     bool
}

func newTextHandler(w io.Writer, level slog.Level, component, version, hostname string, sanitize bool) slog.Handler {
	return &textHandler{
		w:         w,
		level:     level,
		mu:        &sync.Mutex{},
		component: component,
		version:   version,
		hostname:  hostname,
		sanitize:  sanitize,
	}
}

func (h *textHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *textHandler) Handle(_ context.Context, r slog.Record) error {
	bp := bufPool.Get()
	defer func() {
		if cap(*bp) <= maxBufReuse {
			*bp = (*bp)[:0]
			bufPool.Put(bp)
		}
	}()

	s := textState{buf: (*bp)[:0], stack: h.preStack}

	if !r.Time.IsZero() {
		s.buf = r.Time.AppendFormat(s.buf, time.RFC3339Nano)
		s.buf = append(s.buf, ' ')
	}
	s.buf = append(s.buf, r.Level.String()...)
	s.buf = append(s.buf, " ["...)
	s.buf = h.appendText(s.buf, h.component)
	s.buf = append(s.buf, "] ["...)
	s.buf = h.appendText(s.buf, h.version)
	s.buf = append(s.buf, "] ["...)
	s.buf = h.appendText(s.buf, h.hostname)
	s.buf = append(s.buf, "] "...)
	s.buf = h.appendText(s.buf, r.Message)

	s.buf = append(s.buf, h.preformatted...)
	r.Attrs(func(a slog.Attr) bool {
		h.appendAttr(&s, h.prefix, a)
		return true
	})
	s.buf = append(s.buf, '\n')

	if len(s.stack) > 0 {
		s.buf = append(s.buf, "  stack_trace:\n"...)
		for _, frame := range s.stack {
			s.buf = append(s.buf, "    "...)
			s.buf = h.appendText(s.buf, frame)
			s.buf = append(s.buf, '\n')
		}
	}
	*bp = s.buf

	h.mu.Lock()
	defer h.mu.Unlock()
	if _, err := h.w.Write(s.buf); err != nil {
		return fmt.Errorf("write text log record: %w", err)
	}
	return nil
}

func (h *textHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	h2 := *h
	s := textState{buf: []byte(h.preformatted), stack: h.preStack}
	for _, a := range attrs {
		h.appendAttr(&s, h.prefix, a)
	}
	h2.preformatted, h2.preStack = string(s.buf), s.stack
	return &h2
}

func (h *textHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	h2 := *h
	h2.prefix = h.prefix + name + "."
	return &h2
}

const maxBufReuse = 16 << 10

var bufPool = newPool(func() *[]byte { b := make([]byte, 0, 512); return &b })

type textState struct {
	buf   []byte
	stack []string
}

func (h *textHandler) appendAttr(s *textState, prefix string, a slog.Attr) {
	a.Value = a.Value.Resolve()
	if a.Equal(slog.Attr{}) {
		return
	}

	if a.Value.Kind() == slog.KindGroup {
		if a.Key != "" {
			prefix += a.Key + "."
		}
		for _, ga := range a.Value.Group() {
			h.appendAttr(s, prefix, ga)
		}
		return
	}
	if a.Key == "" {
		return
	}

	if prefix == "" && a.Key == FieldStackTrace {
		if frames, ok := a.Value.Any().([]string); ok {
			s.stack = frames
			return
		}
	}

	s.buf = append(s.buf, ' ')
	s.buf = h.appendText(s.buf, prefix)
	s.buf = h.appendText(s.buf, a.Key)
	s.buf = append(s.buf, '=')
	s.buf = h.appendTextValue(s.buf, a.Value)
}

func (h *textHandler) appendText(dst []byte, s string) []byte {
	quote := strings.ContainsAny(s, "\n\r")
	if !quote && h.sanitize {
		quote = strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f })
	}
	if quote {
		return strconv.AppendQuote(dst, s)
	}
	return append(dst, s...)
}

func (h *textHandler) appendTextValue(dst []byte, v slog.Value) []byte {
	if v.Kind() == slog.KindAny && v.Any() == nil {
		return append(dst, "null"...)
	}
	str := v.String()
	if needsQuoting(str) {
		return strconv.AppendQuote(dst, str)
	}
	return append(dst, str...)
}

func needsQuoting(s string) bool {
	return s == "" || strings.ContainsFunc(s, func(r rune) bool {
		return r <= ' ' || r == '"' || r == '=' ||
			r == utf8.RuneError || !unicode.IsPrint(r)
	})
}
