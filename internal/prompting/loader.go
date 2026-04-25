package prompting

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"
	"github.com/cespare/xxhash/v2"
	securejoin "github.com/cyphar/filepath-securejoin"

	aliceassets "github.com/Alice-space/alice"
)

type Loader struct {
	root     string
	embedded fs.FS
	mu       sync.RWMutex
	cache    map[string]*template.Template
}

func NewLoader(root string) *Loader {
	return &Loader{
		root:     strings.TrimSpace(root),
		embedded: aliceassets.PromptFS,
		cache:    make(map[string]*template.Template),
	}
}

func (l *Loader) Root() string {
	if l == nil {
		return ""
	}
	return l.root
}

func (l *Loader) RenderFile(name string, data any) (string, error) {
	if l == nil {
		return "", fmt.Errorf("prompt loader is nil")
	}
	cleanName, err := normalizeTemplateName(name)
	if err != nil {
		return "", err
	}

	diskPath := ""
	if strings.TrimSpace(l.root) != "" {
		diskPath, err = l.resolvePath(cleanName)
		if err != nil {
			return "", err
		}
		raw, err := os.ReadFile(diskPath)
		if err == nil {
			return l.render(diskPath, string(raw), data)
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("read prompt template %q failed: %w", diskPath, err)
		}
	}

	raw, err := l.readEmbedded(cleanName)
	if err != nil {
		if diskPath != "" {
			return "", fmt.Errorf("read prompt template %q failed: %w", diskPath, fs.ErrNotExist)
		}
		return "", fmt.Errorf("read prompt template %q failed: %w", cleanName, err)
	}
	return l.render(cleanName, string(raw), data)
}

func (l *Loader) RenderString(name, raw string, data any) (string, error) {
	if l == nil {
		return "", fmt.Errorf("prompt loader is nil")
	}
	key := strings.TrimSpace(name)
	if key == "" {
		key = "inline"
	}
	return l.render(key, raw, data)
}

func (l *Loader) render(name, raw string, data any) (string, error) {
	compiled, err := l.compile(name, raw)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := compiled.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render prompt template %q failed: %w", name, err)
	}
	return strings.TrimSpace(buf.String()), nil
}

func (l *Loader) compile(name, raw string) (*template.Template, error) {
	hash := xxhash.Sum64String(raw)
	cacheKey := name + ":" + strconv.FormatUint(hash, 16)

	l.mu.RLock()
	if cached := l.cache[cacheKey]; cached != nil {
		l.mu.RUnlock()
		return cached, nil
	}
	l.mu.RUnlock()

	compiled, err := template.New(filepath.Base(name)).
		Funcs(templateFuncMap()).
		Option("missingkey=error").
		Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse prompt template %q failed: %w", name, err)
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	l.cache[cacheKey] = compiled
	return compiled, nil
}

func (l *Loader) resolvePath(name string) (string, error) {
	root := strings.TrimSpace(l.root)
	if root == "" {
		return "", fmt.Errorf("prompt root is empty")
	}

	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve prompt root failed: %w", err)
	}
	pathAbs, err := securejoin.SecureJoin(rootAbs, filepath.FromSlash(name))
	if err != nil {
		return "", fmt.Errorf("resolve prompt template %q failed: %w", name, err)
	}
	return filepath.Clean(pathAbs), nil
}

func normalizeTemplateName(name string) (string, error) {
	clean := path.Clean(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	if clean == "." || clean == "" {
		return "", fmt.Errorf("prompt template name is empty")
	}
	if clean == ".." || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return "", fmt.Errorf("prompt template %q escapes embedded prompt root", clean)
	}
	return clean, nil
}

func (l *Loader) readEmbedded(name string) ([]byte, error) {
	if l == nil || l.embedded == nil {
		return nil, fs.ErrNotExist
	}
	return fs.ReadFile(l.embedded, name)
}

func templateFuncMap() template.FuncMap {
	funcMap := sprig.TxtFuncMap()
	// Security: remove functions that can access environment variables,
	// filesystem paths, or perform network lookups. Automation tasks and
	// prompt rendering should not be able to exfiltrate secrets or probe
	// the host system.
	delete(funcMap, "env")
	delete(funcMap, "expandenv")
	delete(funcMap, "getHostByName")
	delete(funcMap, "osBase")
	delete(funcMap, "osDir")
	delete(funcMap, "osExt")
	delete(funcMap, "osIsAbs")
	delete(funcMap, "osClean")
	funcMap["now"] = func() string {
		return time.Now().Local().Format(time.RFC3339)
	}
	funcMap["date"] = func() string {
		return time.Now().Local().Format("2006-01-02")
	}
	funcMap["time"] = func() string {
		return time.Now().Local().Format("15:04:05")
	}
	funcMap["unix"] = func() int64 {
		return time.Now().Local().Unix()
	}
	return funcMap
}
