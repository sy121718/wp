// Package templates — 插件模板的复合加载器（docs/06-plugin-system.md §7）。
//
// 命名空间合并（终态设计，强于 overlay 回退）：内置模板与插件模板路径空间
// 不相交——内置为 "{name}.jet"，插件为 "plugin/{pluginID}/{file}.jet"——
// 插件永远不可能覆盖内置模板（比"先到先得"更强的安全基线）。
// CompositeLoader 持有内置 embed loader + 启用插件的 fs.FS 映射，
// 按路径前缀路由：plugin/{pid}/{rest} → 插件包根下 components/{rest}。
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

// PluginFS 单个启用插件的模板文件系统视图。
//
// 形状定义已下沉到 internal/builder/source（共享形状包，见该包 plugin.go 的说明）：
// 插件契约包（internal/module/plugin/contract）要用它声明 Assembly.PluginFS，而契约包
// 不得反向依赖 builder 内核 —— 本包依赖 builder/core（组件自带模板注册表），契约经本包
// 就把 core 间接拖了进来。别名指向同一份定义，本包与消费方写法不变。
type PluginFS = source.PluginFS

// compositeLoader 命名空间复合加载器：内置 embed + 插件路由。
type compositeLoader struct {
	base    jetLoader
	plugins map[string]fs.FS
}

// jetLoader 最小加载器接口（embedLoader 实现同一形态）。
type jetLoader interface {
	Exists(templatePath string) bool
	Open(templatePath string) (io.ReadCloser, error)
}

// pluginPathPrefix 插件模板命名空间前缀。
const pluginPathPrefix = "plugin/"

// componentsDir 插件包内组件模板目录。
const componentsDir = "components"

// newCompositeLoader 构建复合加载器（base 为内置 embed loader）。
func newCompositeLoader(base jetLoader, plugins []PluginFS) *compositeLoader {
	m := make(map[string]fs.FS, len(plugins))
	for _, p := range plugins {
		m[p.ID] = p.FS
	}
	return &compositeLoader{base: base, plugins: m}
}

// route 插件命名空间路径 → 目标 fs 与包内相对路径。
// "plugin/marketing/card.jet" → (marketing 的 FS, "components/card.jet")。
// jet 传入的模板路径可能带前导斜杠（path.Join 产物），先规范化（与
// embedLoader.normalize 一致：去前导斜杠）。
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
	// rest 已由白名单文件名约束（plugincomp.templateFileRe），
	// 此处再清路径防穿越兜底（Clean 后必须仍在 components 下）。
	inner := path.Join(componentsDir, path.Clean("/" + rest)[1:])
	if inner == componentsDir || strings.HasPrefix(inner, componentsDir+"/") == false {
		return nil, "", false
	}
	return f, inner, true
}

// Exists 见 jet.Loader。
func (l *compositeLoader) Exists(templatePath string) bool {
	if f, inner, ok := l.route(templatePath); ok {
		st, err := fs.Stat(f, inner)
		return err == nil && !st.IsDir()
	}
	return l.base.Exists(templatePath)
}

// Open 见 jet.Loader。
func (l *compositeLoader) Open(templatePath string) (io.ReadCloser, error) {
	if f, inner, ok := l.route(templatePath); ok {
		return f.Open(inner)
	}
	return l.base.Open(templatePath)
}

// compositeCacheLimit 缓存的 CompositeSet 份数上限。
//
// 留 2 份而不是 1 份：启用集切换（装插件 / 启停）前后各一份，切回来时还能命中，
// 避免「来回切换 = 每次重新解析全部插件模板」。再多就没有意义了 ——
// 一份 Set 持有全部插件模板的解析结果，属于该省内存的地方。
const compositeCacheLimit = 2

var (
	compositeMu    sync.Mutex
	compositeCache = make(map[string]*jet.Set)
	compositeFIFO  []string
)

// NewCompositeSetCached 按指纹复用 CompositeSet（审计 PERF-006）。
//
// 指纹为空时退化为 NewCompositeSet：空串不是合法版本，把它当 key 会让所有
// 「没提供指纹」的调用方共享同一个 Set —— 那是错的（不同插件集共用一份模板）。
//
// 并发安全：构建（jet.Set 解析模板）在锁外进行，只在登记缓存时取锁；
// 两个 goroutine 同时构建同一指纹时，先到者胜，后到者丢弃自己那份并复用已登记的
// （保证同一指纹始终对应同一指针，调用方可以据此判断「是否需要重建」）。
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

// ResetCompositeSetCache 清空 CompositeSet 缓存（测试用）。
func ResetCompositeSetCache() {
	compositeMu.Lock()
	defer compositeMu.Unlock()
	compositeCache = make(map[string]*jet.Set)
	compositeFIFO = nil
}

// NewCompositeSet 构建含插件模板的组件模板 Set（内置 embed + 启用插件）。
//
// plugins 为当前启用的插件集（装配层按 registry 构建）；直接调用时每次产生独立 Set
// （插件模板按任务快照，确定性：同一插件版本集 → 同一模板内容）。
// 构建路径请优先用 NewCompositeSetCached —— 重复解析插件模板是纯浪费（PERF-006）。
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
