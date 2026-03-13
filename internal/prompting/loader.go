package prompting

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"
	"github.com/cespare/xxhash/v2"
)

type Loader struct {
	root  string
	mu    sync.RWMutex
	cache map[string]*template.Template
}

func NewLoader(root string) *Loader {
	return &Loader{
		root:  strings.TrimSpace(root),
		cache: make(map[string]*template.Template),
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
	path, err := l.resolvePath(name)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read prompt template %q failed: %w", path, err)
	}
	return l.render(path, string(raw), data)
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
	clean := filepath.Clean(strings.TrimSpace(name))
	if clean == "." || clean == "" {
		return "", fmt.Errorf("prompt template name is empty")
	}

	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve prompt root failed: %w", err)
	}
	path := filepath.Join(rootAbs, clean)
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve prompt template %q failed: %w", clean, err)
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil {
		return "", fmt.Errorf("resolve prompt template %q failed: %w", clean, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("prompt template %q escapes root %q", clean, rootAbs)
	}
	return pathAbs, nil
}

func templateFuncMap() template.FuncMap {
	funcMap := sprig.TxtFuncMap()
	funcMap["now"] = func() string {
		return time.Now().UTC().Format(time.RFC3339)
	}
	funcMap["date"] = func() string {
		return time.Now().UTC().Format("2006-01-02")
	}
	funcMap["time"] = func() string {
		return time.Now().UTC().Format("15:04:05")
	}
	funcMap["unix"] = func() int64 {
		return time.Now().UTC().Unix()
	}
	return funcMap
}
