package prompting

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Alice-space/alice/internal/logging"
)

var (
	defaultLoaderOnce sync.Once
	defaultLoader     *Loader
)

func DefaultLoader() *Loader {
	defaultLoaderOnce.Do(func() {
		root := findDefaultPromptRoot()
		if root == "" {
			logging.Warnf("prompt root not found; default loader will rely on embedded prompts")
		}
		defaultLoader = NewLoader(root)
	})
	return defaultLoader
}

func findDefaultPromptRoot() string {
	candidates := []string{}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, wd)
	}
	if executablePath, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Dir(executablePath))
	}

	for _, start := range candidates {
		if root := findPromptRoot(start); root != "" {
			return root
		}
	}
	return ""
}

func findPromptRoot(start string) string {
	dir := strings.TrimSpace(start)
	if dir == "" {
		return ""
	}
	for {
		candidate := filepath.Join(dir, "prompts")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
