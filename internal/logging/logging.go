package logging

import (
	"fmt"
	"io"
	stdlog "log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	levelDebug int32 = iota
	levelInfo
	levelWarn
	levelError
)

const (
	defaultLogMaxSizeMB  = 20
	defaultLogMaxBackups = 5
	defaultLogMaxAgeDays = 7
)

type Options struct {
	Level      string
	FilePath   string
	MaxSizeMB  int
	MaxBackups int
	MaxAgeDays int
	Compress   bool
}

var currentLevel atomic.Int32

var (
	loggerMu sync.RWMutex
	logger   zerolog.Logger
)

func init() {
	currentLevel.Store(levelInfo)
	zerolog.TimeFieldFormat = time.RFC3339
	logger = zerolog.New(os.Stderr).With().Timestamp().Logger()
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	stdlog.SetFlags(0)
	stdlog.SetOutput(stdLogWriter{})
}

func Configure(opts Options) error {
	writers := make([]io.Writer, 0, 2)
	console := zerolog.ConsoleWriter{
		Out:        os.Stderr,
		TimeFormat: time.RFC3339,
	}
	writers = append(writers, console)

	filePath := strings.TrimSpace(opts.FilePath)
	if filePath != "" {
		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			return fmt.Errorf("create log dir failed: %w", err)
		}
		rotator := &lumberjack.Logger{
			Filename:   filePath,
			MaxSize:    positiveOrDefault(opts.MaxSizeMB, defaultLogMaxSizeMB),
			MaxBackups: positiveOrDefault(opts.MaxBackups, defaultLogMaxBackups),
			MaxAge:     positiveOrDefault(opts.MaxAgeDays, defaultLogMaxAgeDays),
			Compress:   opts.Compress,
		}
		writers = append(writers, zerolog.ConsoleWriter{
			Out:        rotator,
			NoColor:    true,
			TimeFormat: time.RFC3339,
		})
	}

	configured := zerolog.New(io.MultiWriter(writers...)).With().Timestamp().Logger()
	setLogger(configured)
	SetLevel(opts.Level)
	return nil
}

func SetLevel(level string) {
	currentLevel.Store(levelValue(level))
	zerolog.SetGlobalLevel(zerologLevel(level))
}

func IsDebugEnabled() bool {
	return currentLevel.Load() <= levelDebug
}

func Debugf(format string, args ...any) {
	logf(zerolog.DebugLevel, format, args...)
}

func Infof(format string, args ...any) {
	logf(zerolog.InfoLevel, format, args...)
}

func Warnf(format string, args ...any) {
	logf(zerolog.WarnLevel, format, args...)
}

func Errorf(format string, args ...any) {
	logf(zerolog.ErrorLevel, format, args...)
}

func Fatalf(format string, args ...any) {
	current := getLogger()
	current.Fatal().Msgf(format, args...)
}

type AgentTrace struct {
	Provider  string
	Agent     string
	ThreadID  string
	Model     string
	Profile   string
	Input     string
	Output    string
	ToolCalls []string
	Error     string
}

func DebugAgentTrace(trace AgentTrace) {
	if !IsDebugEnabled() {
		return
	}

	sections := []string{
		"# Agent Trace",
		"",
		"- provider: `" + defaultString(trace.Provider, "unknown") + "`",
		"- agent: `" + defaultString(trace.Agent, "assistant") + "`",
		"- thread_id: `" + defaultString(trace.ThreadID, "-") + "`",
		"- model: `" + defaultString(trace.Model, "-") + "`",
		"- profile: `" + defaultString(trace.Profile, "-") + "`",
	}
	if strings.TrimSpace(trace.Error) != "" {
		sections = append(sections,
			"- error: `"+strings.TrimSpace(trace.Error)+"`",
		)
	}
	sections = append(sections,
		"",
		"## Input",
		codeBlock(trace.Input),
		"",
		"## Tool Calls",
	)
	if len(trace.ToolCalls) == 0 {
		sections = append(sections, "- none")
	} else {
		for _, item := range trace.ToolCalls {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			sections = append(sections, "- "+item)
		}
	}
	sections = append(sections,
		"",
		"## Output",
		codeBlock(trace.Output),
	)
	current := getLogger()
	current.Debug().Str("kind", "agent-trace").Msg(strings.Join(sections, "\n"))
}

func logf(level zerolog.Level, format string, args ...any) {
	current := getLogger()
	switch level {
	case zerolog.DebugLevel:
		current.Debug().Msgf(format, args...)
	case zerolog.InfoLevel:
		current.Info().Msgf(format, args...)
	case zerolog.WarnLevel:
		current.Warn().Msgf(format, args...)
	case zerolog.ErrorLevel:
		current.Error().Msgf(format, args...)
	default:
		current.WithLevel(level).Msgf(format, args...)
	}
}

func getLogger() zerolog.Logger {
	loggerMu.RLock()
	defer loggerMu.RUnlock()
	return logger
}

func setLogger(next zerolog.Logger) {
	loggerMu.Lock()
	defer loggerMu.Unlock()
	logger = next
}

func zerologLevel(level string) zerolog.Level {
	switch levelValue(level) {
	case levelDebug:
		return zerolog.DebugLevel
	case levelWarn:
		return zerolog.WarnLevel
	case levelError:
		return zerolog.ErrorLevel
	default:
		return zerolog.InfoLevel
	}
}

func levelValue(level string) int32 {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return levelDebug
	case "warn", "warning":
		return levelWarn
	case "error":
		return levelError
	default:
		return levelInfo
	}
}

func codeBlock(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		trimmed = "(empty)"
	}
	return "```md\n" + trimmed + "\n```"
}

func defaultString(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func positiveOrDefault(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

type stdLogWriter struct{}

func (stdLogWriter) Write(p []byte) (int, error) {
	message := strings.TrimSpace(string(p))
	if message != "" {
		Infof("%s", message)
	}
	return len(p), nil
}
