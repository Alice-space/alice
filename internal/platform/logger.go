package platform

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Logger is the structured logger interface used throughout Alice.
type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
	Fatal(msg string, args ...any)
	WithComponent(name string) Logger
	WithContext(ctx context.Context) Logger
}

// zerologLogger implements Logger using zerolog + lumberjack.
type zerologLogger struct {
	logger    zerolog.Logger
	component string
	config    LoggingConfig
	attrs     map[string]any
}

var (
	rootLogger      zerolog.Logger
	componentMu     sync.RWMutex
	rootLevel       zerolog.Level
	componentLevels map[string]zerolog.Level
)

func init() {
	// Default console output for early boot
	rootLogger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: zerolog.TimeFieldFormat}).
		With().Timestamp().Logger()
	rootLevel = zerolog.InfoLevel
	componentLevels = make(map[string]zerolog.Level)
}

// NewLoggerFromConfig creates a new logger from config using zerolog + lumberjack.
func NewLoggerFromConfig(cfg LoggingConfig) (Logger, error) {
	componentMu.Lock()
	defer componentMu.Unlock()

	rootLevel = parseZerologLevel(cfg.Level)
	componentLevels = make(map[string]zerolog.Level)
	for comp, level := range cfg.Components {
		componentLevels[comp] = parseZerologLevel(level)
	}

	var writers []io.Writer

	// Console output
	if cfg.Console {
		if cfg.Format == "json" {
			writers = append(writers, os.Stdout)
		} else {
			writers = append(writers, zerolog.ConsoleWriter{
				Out:        os.Stdout,
				TimeFormat: "15:04:05",
				NoColor:    false,
			})
		}
	}

	// File output with rotation (lumberjack)
	if cfg.File != nil && cfg.File.Path != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.File.Path), 0755); err != nil {
			return nil, err
		}

		fileWriter := &lumberjack.Logger{
			Filename:   cfg.File.Path,
			MaxSize:    cfg.File.MaxSizeMB,
			MaxBackups: cfg.File.MaxBackups,
			MaxAge:     cfg.File.MaxAgeDays,
			Compress:   cfg.File.Compress,
		}
		writers = append(writers, fileWriter)
	}

	var output io.Writer
	if len(writers) == 0 {
		output = io.Discard
	} else if len(writers) == 1 {
		output = writers[0]
	} else {
		output = zerolog.MultiLevelWriter(writers...)
	}

	rootLogger = zerolog.New(output).
		Level(rootLevel).
		With().Timestamp().Logger()

	return &zerologLogger{
		logger: rootLogger,
		config: cfg,
		attrs:  make(map[string]any),
	}, nil
}

// NewDefaultLogger creates a simple console logger for bootstrapping.
func NewDefaultLogger() Logger {
	return &zerologLogger{
		logger: rootLogger,
		config: LoggingConfig{Level: "info"},
		attrs:  make(map[string]any),
	}
}

// noopLogger is a no-op logger for testing.
type noopLogger struct{}

func (n *noopLogger) Debug(msg string, args ...any)          {}
func (n *noopLogger) Info(msg string, args ...any)           {}
func (n *noopLogger) Warn(msg string, args ...any)           {}
func (n *noopLogger) Error(msg string, args ...any)          {}
func (n *noopLogger) Fatal(msg string, args ...any)          {}
func (n *noopLogger) WithComponent(name string) Logger       { return n }
func (n *noopLogger) WithContext(ctx context.Context) Logger { return n }

// NewNoopLogger creates a no-op logger for testing.
func NewNoopLogger() Logger {
	return &noopLogger{}
}

func (l *zerologLogger) log(level zerolog.Level, msg string, args []any) {
	// Check component level
	componentMu.RLock()
	compLevel := rootLevel
	if l.component != "" {
		if level, ok := componentLevels[l.component]; ok {
			compLevel = level
		}
	}
	componentMu.RUnlock()

	if level < compLevel {
		return
	}

	event := l.logger.WithLevel(level)

	// Add component
	if l.component != "" {
		event = event.Str("component", l.component)
	}

	// Add stored attrs
	for k, v := range l.attrs {
		event = event.Interface(k, v)
	}

	// Add call args
	for i := 0; i < len(args); i += 2 {
		if i+1 < len(args) {
			key, ok := args[i].(string)
			if !ok {
				key = string(rune('a' + i))
			}
			event = event.Interface(key, args[i+1])
		}
	}

	event.Msg(msg)
}

func (l *zerologLogger) Debug(msg string, args ...any) { l.log(zerolog.DebugLevel, msg, args) }
func (l *zerologLogger) Info(msg string, args ...any)  { l.log(zerolog.InfoLevel, msg, args) }
func (l *zerologLogger) Warn(msg string, args ...any)  { l.log(zerolog.WarnLevel, msg, args) }
func (l *zerologLogger) Error(msg string, args ...any) { l.log(zerolog.ErrorLevel, msg, args) }
func (l *zerologLogger) Fatal(msg string, args ...any) { l.log(zerolog.FatalLevel, msg, args) }

func (l *zerologLogger) WithComponent(name string) Logger {
	newAttrs := make(map[string]any, len(l.attrs))
	for k, v := range l.attrs {
		newAttrs[k] = v
	}
	return &zerologLogger{
		logger:    l.logger,
		component: name,
		config:    l.config,
		attrs:     newAttrs,
	}
}

func (l *zerologLogger) WithContext(ctx context.Context) Logger {
	newAttrs := make(map[string]any, len(l.attrs))
	for k, v := range l.attrs {
		newAttrs[k] = v
	}

	// Extract trace/request/task IDs from context
	if traceID, ok := ctx.Value(traceIDKey).(string); ok && traceID != "" {
		newAttrs["trace_id"] = traceID
	}
	if requestID, ok := ctx.Value(requestIDKey).(string); ok && requestID != "" {
		newAttrs["request_id"] = requestID
	}
	if taskID, ok := ctx.Value(taskIDKey).(string); ok && taskID != "" {
		newAttrs["task_id"] = taskID
	}
	if eventID, ok := ctx.Value(eventIDKey).(string); ok && eventID != "" {
		newAttrs["event_id"] = eventID
	}

	return &zerologLogger{
		logger:    l.logger,
		component: l.component,
		config:    l.config,
		attrs:     newAttrs,
	}
}

func parseZerologLevel(s string) zerolog.Level {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "debug":
		return zerolog.DebugLevel
	case "info":
		return zerolog.InfoLevel
	case "warn", "warning":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	case "fatal":
		return zerolog.FatalLevel
	case "trace":
		return zerolog.TraceLevel
	default:
		return zerolog.InfoLevel
	}
}
