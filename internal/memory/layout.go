package memory

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	GlobalDirName         = "global"
	ResourceDirName       = "resources"
	ScopeRootDirName      = "scopes"
	LayoutVersionFileName = "layout_version"
	ScopedLayoutVersion   = "3"
)

type scopePaths struct {
	Key          string
	Dir          string
	LongTermPath string
	DailyDir     string
}

func ensureLayoutDirs(root string) error {
	root = strings.TrimSpace(root)
	if root == "" {
		return errors.New("memory dir is empty")
	}
	if err := os.MkdirAll(filepath.Join(root, GlobalDirName, ShortTermDirName), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(root, ScopeRootDirName), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(root, ResourceDirName, GlobalDirName), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(root, ResourceDirName, ScopeRootDirName), 0o755); err != nil {
		return err
	}
	return ensureFileExists(globalLongTermPath(root), 0o644)
}

func ensureScopeDirs(paths scopePaths) error {
	if err := os.MkdirAll(paths.DailyDir, 0o755); err != nil {
		return err
	}
	return ensureFileExists(paths.LongTermPath, 0o644)
}

func globalLongTermPath(root string) string {
	return filepath.Join(strings.TrimSpace(root), GlobalDirName, LongTermFileName)
}

func globalDailyDir(root string) string {
	return filepath.Join(strings.TrimSpace(root), GlobalDirName, ShortTermDirName)
}

func layoutVersionPath(root string) string {
	return filepath.Join(strings.TrimSpace(root), LayoutVersionFileName)
}

func resourceBaseDir(root string) string {
	return filepath.Join(strings.TrimSpace(root), ResourceDirName)
}

func globalResourceDir(root string) string {
	return filepath.Join(resourceBaseDir(root), GlobalDirName)
}

func resolveScopePaths(root, memoryScopeKey string) (scopePaths, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return scopePaths{}, errors.New("memory dir is empty")
	}

	key := normalizeMemoryScopeKey(memoryScopeKey)
	scopeType, scopeID := splitMemoryScopeKey(key)
	dir := filepath.Join(
		root,
		ScopeRootDirName,
		sanitizeScopePathSegment(scopeType),
		sanitizeScopePathSegment(scopeID),
	)
	return scopePaths{
		Key:          key,
		Dir:          dir,
		LongTermPath: filepath.Join(dir, LongTermFileName),
		DailyDir:     filepath.Join(dir, ShortTermDirName),
	}, nil
}

func ResolveScopedResourceRoot(baseResourceDir, memoryScopeKey string) string {
	baseResourceDir = strings.TrimSpace(baseResourceDir)
	if baseResourceDir == "" {
		return ""
	}

	key := normalizeMemoryScopeKey(memoryScopeKey)
	scopeType, scopeID := splitMemoryScopeKey(key)
	return filepath.Join(
		baseResourceDir,
		ScopeRootDirName,
		sanitizeScopePathSegment(scopeType),
		sanitizeScopePathSegment(scopeID),
	)
}

func normalizeMemoryScopeKey(memoryScopeKey string) string {
	memoryScopeKey = strings.TrimSpace(memoryScopeKey)
	if memoryScopeKey == "" {
		return "unknown:unknown"
	}
	if strings.Contains(memoryScopeKey, ":") {
		return memoryScopeKey
	}
	return "unknown:" + memoryScopeKey
}

func splitMemoryScopeKey(memoryScopeKey string) (string, string) {
	key := normalizeMemoryScopeKey(memoryScopeKey)
	scopeType, scopeID, found := strings.Cut(key, ":")
	if !found {
		return "unknown", sanitizeScopePathSegment(key)
	}
	scopeType = strings.TrimSpace(scopeType)
	scopeID = strings.TrimSpace(scopeID)
	if scopeType == "" {
		scopeType = "unknown"
	}
	if scopeID == "" {
		scopeID = "unknown"
	}
	return scopeType, scopeID
}

func sanitizeScopePathSegment(segment string) string {
	segment = strings.TrimSpace(segment)
	if segment == "" {
		return "unknown"
	}

	var builder strings.Builder
	builder.Grow(len(segment))
	for _, r := range segment {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '.', r == '_', r == '-':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	if builder.Len() == 0 {
		return "unknown"
	}
	return builder.String()
}
