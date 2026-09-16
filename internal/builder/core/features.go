package core

// features.go — 渲染期运行时特征登记（审计 PERF-014）。
//
// 背景：产物要注入哪些脚本（htmx / 控件基座 / 组件增强块）此前是**渲染完成后**对整页
// HTML 跑一遍 tokenizer 得出的（builder/ui_script.go 的 collectHTMLScan）。它能工作，
// 但代价是每页多一遍 O(字节数) 的解析，整站重建时累积。
//
// 改为渲染期上报：组件在渲染时把自己**真实输出**的属性 / class 登记到 RenderContext，
// 产物组装层直接读登记结果，不再扫 HTML。
//
// 为什么放在 core：特征的两端跨包 —— 组件（components/*）登记，builder 的产物组装层
// （ui_script.go）读取，core 是两者共同依赖的最低层。
//
// 登记语义（写错的症状都是静默的，见下面两条）：
//   - 登记的是**产物 HTML 里真实出现的属性名 / class 名**（小写、与输出逐字一致），
//     不是「组件用了什么能力」；
//   - 漏登记 → 那条交互在产物里失效（htmx 或控件脚本不注入，页面照常打开、点了没反应）；
//   - 多登记 → 白送字节（没用到 htmx 的页面背上 50KB 内联脚本）。
//
// 这两条由 builder 包的交叉验证测试钉住：同一份编译结果，登记路径与 tokenize 路径
// 必须得出完全相同的注入结果（ui_feature_crosscheck_test.go）。

import "sync"

// FeatureSet 单次编译的运行时特征集合（属性名 / class 名，均小写）。
//
// 零值不可用：用 NewFeatureSet 构造。并发安全（登记分散在各组件的渲染调用里，
// 将来若出现并发渲染子树也不会踩坏 map）。
type FeatureSet struct {
	mu      sync.RWMutex
	attrs   map[string]struct{}
	classes map[string]struct{}
}

// NewFeatureSet 新建空特征集合。
func NewFeatureSet() *FeatureSet {
	return &FeatureSet{
		attrs:   make(map[string]struct{}),
		classes: make(map[string]struct{}),
	}
}

// UseAttr 登记产物里出现的属性名（小写精确名，如 "hx-get" / "data-cart-icon-panel"）。
// 空串与纯空白被忽略：它们不可能出现在合法 HTML 属性位上，登记进去只会污染比对。
func (f *FeatureSet) UseAttr(names ...string) {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.attrs == nil {
		f.attrs = make(map[string]struct{})
	}
	for _, name := range names {
		if name = normalizeFeatureName(name); name != "" {
			f.attrs[name] = struct{}{}
		}
	}
}

// UseClass 登记产物里出现的 class 名（小写）。
func (f *FeatureSet) UseClass(names ...string) {
	if f == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.classes == nil {
		f.classes = make(map[string]struct{})
	}
	for _, name := range names {
		if name = normalizeFeatureName(name); name != "" {
			f.classes[name] = struct{}{}
		}
	}
}

// Attrs 返回属性集合的副本（调用方改动不影响本集合）。
func (f *FeatureSet) Attrs() map[string]struct{} { return f.snapshot(true) }

// Classes 返回 class 集合的副本。
func (f *FeatureSet) Classes() map[string]struct{} { return f.snapshot(false) }

func (f *FeatureSet) snapshot(attrs bool) map[string]struct{} {
	out := map[string]struct{}{}
	if f == nil {
		return out
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	src := f.classes
	if attrs {
		src = f.attrs
	}
	for k := range src {
		out[k] = struct{}{}
	}
	return out
}

// Len 返回属性与 class 的登记总数（诊断用）。
func (f *FeatureSet) Len() int {
	if f == nil {
		return 0
	}
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.attrs) + len(f.classes)
}

// normalizeFeatureName 统一小写并去空白（HTML 属性名与 class 名大小写不敏感，
// tokenize 路径也是按小写收集的 —— 两边口径必须一致）。
func normalizeFeatureName(name string) string {
	out := make([]rune, 0, len(name))
	for _, r := range name {
		switch {
		case r >= 'A' && r <= 'Z':
			out = append(out, r+('a'-'A'))
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			// 丢掉空白
		default:
			out = append(out, r)
		}
	}
	return string(out)
}

// ViewFeatureDeclarer 可选接口：组件视图声明本次渲染**真实输出**的运行时特征（PERF-014）。
//
// 为什么挂在「视图」上而不是「节点」上：模板渲染读的就是视图字段（.V.XXX），
// 由视图自己声明「我这些字段会让模板输出哪些属性」——判定与模板同源，
// 组件改分支时两边一起改，不存在「判定条件与模板分叉」这类漂移。
// 若改为渲染层按节点重新解析 props 判断，同一个分支就有了第二份实现。
//
// 调用时机：各渲染分支在 BuildView 之后、写入 nodeView 之前（builder/jetview.go 的
// declareViewFeatures），因此容器子节点、全局块展开的节点、任何经 nodeViewOf 的路径
// 都会被收集；未实现本接口的组件（纯内容型）自动跳过 —— 「没用就零字节注入」由此成立。
//
// 声明内容必须是**模板真的会输出的属性 / class 名**（小写精确名）：
//   - 漏一个 → 那条交互在产物里静默失效（脚本不注入，页面照常打开、点了没反应）；
//   - 多一个 → 白送字节（没用到 htmx 的页面背上 50KB 内联脚本）。
//
// 两种情况都由交叉验证测试钉住：同一份编译结果，登记路径与 tokenize 路径必须
// 得出完全相同的注入结果（internal/builder/ui_feature_crosscheck_test.go）。
type ViewFeatureDeclarer interface {
	// DeclareFeatures 返回本次渲染输出的属性名与 class 名（均小写）。
	// 返回空切片表示「这次什么都没输出」（条件分支未命中），等价于不实现本接口。
	DeclareFeatures() (attrs, classes []string)
}

// UseAttr 登记本次渲染真实输出的属性名（小写）。ctx 或 Features 为 nil 时是空操作：
// 片段渲染（RenderNodeHTML）与单测直连组件都不需要收集特征。
func (c *RenderContext) UseAttr(names ...string) {
	if c == nil || c.Features == nil {
		return
	}
	c.Features.UseAttr(names...)
}

// UseClass 登记本次渲染真实输出的 class 名（小写）。语义同 UseAttr。
func (c *RenderContext) UseClass(names ...string) {
	if c == nil || c.Features == nil {
		return
	}
	c.Features.UseClass(names...)
}
