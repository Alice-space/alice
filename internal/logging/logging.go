package logging

import (
	"log"
	"strings"
	"sync/atomic"
)

const (
	levelDebug int32 = iota
	levelInfo
	levelWarn
	levelError
)

var currentLevel atomic.Int32

func init() {
	currentLevel.Store(levelInfo)
}

func SetLevel(level string) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		currentLevel.Store(levelDebug)
	case "warn", "warning":
		currentLevel.Store(levelWarn)
	case "error":
		currentLevel.Store(levelError)
	default:
		currentLevel.Store(levelInfo)
	}
}

func IsDebugEnabled() bool {
	return currentLevel.Load() <= levelDebug
}

func Debugf(format string, args ...any) {
	if !IsDebugEnabled() {
		return
	}
	log.Printf("[DEBUG] "+format, args...)
}
