package templates

import (
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"
	"sync"

	"github.com/CloudyKit/jet/v6"

	"go_wp/internal/builder/source"
)

type PluginFS = source.PluginFS

type compositeLoader struct {
	base    jetLoader
	plugins map[string]fs.FS
}

type jetLoader interface {
	Exists(templatePath string) bool
	Open(templatePath string) (io.ReadCloser, error)
}

const pluginPathPrefix = "plugin/"

const componentsDir = "components"

func newCompositeLoader(base jetLoader, plugins []PluginFS) *compositeLoader {
	m := make(map[string]fs.FS, len(plugins))
	for _, p := range plugins {
		m[p.ID] = p.FS
	}
	return &compositeLoader{base: base, plugins: m}
}

func (l *compositeLoader) route(templatePath string) (fs.FS, string, bool) {
	clean := path.Clean(path.Join("/", templatePath))[1:]
	rel, ok := strings.CutPrefix(clean, pluginPathPrefix)
	if !ok {
		return nil, "", false
	}
	pid, rest, ok := strings.Cut(rel, "/")
	if !ok || pid == "" || rest == "" {
		return nil, "", false
	}
	f, ok := l.plugins[pid]
	if !ok {
		return nil, "", false
	}

	inner := path.Join(componentsDir, path.Clean("/" + rest)[1:])
	if inner == componentsDir || strings.HasPrefix(inner, componentsDir+"/") == false {
		return nil, "", false
	}
	return f, inner, true
}

func (l *compositeLoader) Exists(templatePath string) bool {
	if f, inner, ok := l.route(templatePath); ok {
		st, err := fs.Stat(f, inner)
		return err == nil && !st.IsDir()
	}
	return l.base.Exists(templatePath)
}

func (l *compositeLoader) Open(templatePath string) (io.ReadCloser, error) {
	if f, inner, ok := l.route(templatePath); ok {
		return f.Open(inner)
	}
	return l.base.Open(templatePath)
}

const compositeCacheLimit = 2

var (
	compositeMu    sync.Mutex
	compositeCache = make(map[string]*jet.Set)
	compositeFIFO  []string
)

func NewCompositeSetCached(fingerprint string, plugins []PluginFS) (*jet.Set, error) {
	if fingerprint == "" {
		return NewCompositeSet(plugins)
	}
	compositeMu.Lock()
	if set, ok := compositeCache[fingerprint]; ok {
		compositeMu.Unlock()
		return set, nil
	}
	compositeMu.Unlock()

	set, err := NewCompositeSet(plugins)
	if err != nil {
		return nil, err
	}

	compositeMu.Lock()
	defer compositeMu.Unlock()
	if existing, ok := compositeCache[fingerprint]; ok {
		return existing, nil
	}
	compositeCache[fingerprint] = set
	compositeFIFO = append(compositeFIFO, fingerprint)
	for len(compositeFIFO) > compositeCacheLimit {
		oldest := compositeFIFO[0]
		compositeFIFO = compositeFIFO[1:]
		delete(compositeCache, oldest)
	}
	return set, nil
}

func ResetCompositeSetCache() {
	compositeMu.Lock()
	defer compositeMu.Unlock()
	compositeCache = make(map[string]*jet.Set)
	compositeFIFO = nil
}

func NewCompositeSet(plugins []PluginFS) (*jet.Set, error) {
	base, err := newEmbeddedComponentLoader()
	if err != nil {
		return nil, fmt.Errorf("内置组件模板加载失败: %w", err)
	}
	set := jet.NewSet(
		newCompositeLoader(base, plugins),
		jet.WithTemplateNameExtensions([]string{"", ".jet"}),
	)
	injectGlobals(set)
	return set, nil
}
