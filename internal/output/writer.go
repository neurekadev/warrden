// Package output renders wArrden's stdout-only tree log contract.
package output

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// Level is the minimum emitted log severity.
type Level int

const (
	Debug   Level = iota // Debug emits every configured level.
	Info                 // Info emits informational, warning, and error output.
	Warning              // Warning emits warning and error output.
	Error                // Error emits error output only.
)

type reporter interface {
	Capture(error, string, string)
}

type clock func() time.Time

// Writer serializes all log output onto one stream.
type Writer struct {
	mu       sync.Mutex
	out      io.Writer
	level    Level
	location *time.Location
	now      clock
	reporter reporter
}

// New constructs an output writer.
func New(out io.Writer, level Level, location *time.Location, r reporter) *Writer {
	if out == nil {
		out = io.Discard
	}
	if location == nil {
		location = time.UTC
	}
	return &Writer{out: out, level: level, location: location, now: time.Now, reporter: r}
}

// ParseLevel converts the public configuration spelling to a Level.
func ParseLevel(value string) Level {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return Debug
	case "warning":
		return Warning
	case "error":
		return Error
	default:
		return Info
	}
}

// Debug writes a debug message.
func (w *Writer) Debug(context, message string, detail ...string) {
	if w.level > Debug {
		return
	}
	w.log("DEBUG", context, message, first(detail))
}

// Warn writes a warning message.
func (w *Writer) Warn(context, message string, detail ...string) {
	if w.level > Warning {
		return
	}
	w.log("WARN", context, message, first(detail))
}

// ErrorText writes an error that must not be sent to telemetry.
func (w *Writer) ErrorText(context, message string, detail ...string) {
	if w.level > Error {
		return
	}
	w.log("ERROR", context, message, first(detail))
}

// Error writes and reports an unexpected application error.
func (w *Writer) Error(context, message string, err error) {
	if w.reporter != nil && err != nil {
		w.reporter.Capture(err, context, message)
	}
	if w.level > Error {
		return
	}
	detail := ""
	if err != nil {
		detail = legacyError(err)
	}
	w.log("ERROR", context, message, detail)
}

func (w *Writer) log(level, context, message, detail string) {
	var buffer bytes.Buffer
	color := colorFor(level)
	fmt.Fprintf(&buffer, "%s[%s %s] [%s]\x1b[0m\n", color, w.timestamp(), level, context)
	if detail == "" {
		fmt.Fprintf(&buffer, " └─ %s\n\n", message)
	} else {
		fmt.Fprintf(&buffer, " ├─ %s\n └─ %s\n\n", message, detail)
	}
	w.write(buffer.Bytes())
}

func (w *Writer) timestamp() string { return w.now().In(w.location).Format("15:04:05") }

func (w *Writer) write(data []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, _ = w.out.Write(data)
}

func colorFor(level string) string {
	switch level {
	case "DEBUG":
		return "\x1b[90m"
	case "WARN":
		return "\x1b[33m"
	case "ERROR":
		return "\x1b[31m"
	default:
		return ""
	}
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func legacyError(err error) string {
	type legacy interface{ LegacyError() string }
	var value legacy
	if errors.As(err, &value) {
		return value.LegacyError()
	}
	return fmt.Sprintf("%T: %s", err, err)
}
