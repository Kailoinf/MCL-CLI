// Package logx provides the terminal + file logger used by mcli.
//
// Rules of the house:
//   - user-facing results go to stdout (Printf)
//   - diagnostics go to stderr and/or the log file, gated by level
//   - nothing is written to the log file unless a level says so
package logx

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Level is a log severity.
type Level int

// Levels, from most to least verbose.
const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelOff
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	default:
		return "off"
	}
}

// ParseLevel converts a config string into a Level.
func ParseLevel(s string) (Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug", "trace":
		return LevelDebug, nil
	case "info", "":
		return LevelInfo, nil
	case "warn", "warning":
		return LevelWarn, nil
	case "error":
		return LevelError, nil
	case "off", "none", "silent":
		return LevelOff, nil
	default:
		return LevelInfo, fmt.Errorf("unknown log level %q (want debug|info|warn|error|off)", s)
	}
}

// Logger writes leveled diagnostics to stderr and, optionally, to a file.
type Logger struct {
	mu      sync.Mutex
	term    io.Writer
	termLvl Level
	file    *os.File
	fileLvl Level
	color   bool
}

// New builds a logger. filePath may be empty to disable file logging.
// Passing level "off" for a sink disables it.
func New(termLevel, fileLevel Level, filePath string) (*Logger, error) {
	l := &Logger{
		term:    os.Stderr,
		termLvl: termLevel,
		fileLvl: fileLevel,
		color:   isTTY(os.Stderr),
	}
	if filePath != "" && fileLevel != LevelOff {
		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			return nil, fmt.Errorf("create log dir: %w", err)
		}
		f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return nil, fmt.Errorf("open log file: %w", err)
		}
		l.file = f
	}
	return l, nil
}

// Discard returns a logger that throws everything away (handy in tests).
func Discard() *Logger {
	return &Logger{term: io.Discard, termLvl: LevelOff, fileLvl: LevelOff}
}

// Close flushes and closes the log file.
func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
	}
}

// IsTTY reports whether stderr is an interactive terminal (progress bars and
// colors only make sense then).
func (l *Logger) IsTTY() bool { return l.color }

func isTTY(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// Printf writes a line to stdout unconditionally. This is the only way the
// program talks to the user about results, as opposed to diagnostics.
func (l *Logger) Printf(format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(os.Stdout, format+"\n", args...)
}

// Raw writes to stdout without appending a newline.
func (l *Logger) Raw(format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(os.Stdout, format, args...)
}

func (l *Logger) log(lvl Level, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil && lvl >= l.fileLvl {
		ts := time.Now().Format("2006-01-02 15:04:05.000")
		fmt.Fprintf(l.file, "%s [%s] %s\n", ts, strings.ToUpper(lvl.String()), msg)
	}
	if l.termLvl != LevelOff && lvl >= l.termLvl {
		label := strings.ToUpper(lvl.String())
		if l.color {
			label = levelColor(lvl) + label + "\x1b[0m"
		}
		fmt.Fprintf(l.term, "%-5s %s\n", label, msg)
	}
}

func levelColor(l Level) string {
	switch l {
	case LevelDebug:
		return "\x1b[90m" // grey
	case LevelInfo:
		return "\x1b[36m" // cyan
	case LevelWarn:
		return "\x1b[33m" // yellow
	default:
		return "\x1b[31m" // red
	}
}

// Debugf logs at debug level.
func (l *Logger) Debugf(format string, args ...interface{}) { l.log(LevelDebug, format, args...) }

// Infof logs at info level.
func (l *Logger) Infof(format string, args ...interface{}) { l.log(LevelInfo, format, args...) }

// Warnf logs at warn level.
func (l *Logger) Warnf(format string, args ...interface{}) { l.log(LevelWarn, format, args...) }

// Errorf logs at error level.
func (l *Logger) Errorf(format string, args ...interface{}) { l.log(LevelError, format, args...) }
