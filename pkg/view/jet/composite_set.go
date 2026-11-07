package jet

import (
	"io"
	"io/fs"
	"path"
	"strings"
	"sync"

	"github.com/CloudyKit/jet/v6"
)

// PluginFS 是一个插件的模板源：ID 是命名路由段，FS 的根对应插件的 components 目录。
//
// 形状与 pkg/builder/source.PluginFS 一致，但这里只表达「模板方如何找到它」；
// 插件允许范围、工程绑定与 fingerprint 仍归插件业务与应用编译装配。
type PluginFS struct {
	ID string
	FS fs.FS
}

// 插件模板的逻辑键固定为 plugin/<id>/<components 下的相对路径>。
const (
	pluginPathPrefix = "plugin/"
	componentsDir    = "components"
)

type compositeLoader struct {
	base    Loader
	plugins map[string]fs.FS
}

// NewCompositeLoader 把应用提供的基础 loader 与插件模板源合成一个查找器。
//
// plugin/<id>/... 只在对应插件源里找；未命中不回落到基础 loader，
// 避免「插件缺资源时被同名应用模板顶替」这种静默错误。
func NewCompositeLoader(base Loader, plugins []PluginFS) Loader {
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
	inner := path.Join(componentsDir, path.Clean("/"+rest)[1:])
	if inner != componentsDir && !strings.HasPrefix(inner, componentsDir+"/") {
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

// compositeCacheLimit 是有界缓存的容量：插件装配按工程/版本切换，
// 只保留最近两代，避免进程内无界增长。
const compositeCacheLimit = 2

var (
	compositeMu    sync.Mutex
	compositeCache = make(map[string]*jet.Set)
	compositeFIFO  []string
)

// NewCompositeSetCached 按 fingerprint 复用已构建的 Set；fingerprint 为空表示不复用。
//
// fingerprint 必须覆盖实际模板源、插件集合与版本 —— 只按插件数或名字做键
// 会在同一工程换版后复用到旧字节。缓存有界（最近两代），切换工程/版本时淘汰最旧一代。
func NewCompositeSetCached(fingerprint string, base Loader, plugins []PluginFS, exts []string) (*jet.Set, error) {
	if fingerprint == "" {
		return NewJetSetFromLoader(NewCompositeLoader(base, plugins), false, exts), nil
	}
	compositeMu.Lock()
	if set, ok := compositeCache[fingerprint]; ok {
		compositeMu.Unlock()
		return set, nil
	}
	compositeMu.Unlock()

	set := NewJetSetFromLoader(NewCompositeLoader(base, plugins), false, exts)

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

// ResetCompositeSetCache 清空缓存（测试与装配重置用）。
func ResetCompositeSetCache() {
	compositeMu.Lock()
	defer compositeMu.Unlock()
	compositeCache = make(map[string]*jet.Set)
	compositeFIFO = nil
}
