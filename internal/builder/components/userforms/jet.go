package userforms

// jet.go — Jet 渲染路径（视图组装）。
//
// 片段地址与降级链接都在这里编好交给模板：让模板自己拼 URL，等于把
// 「片段端点长什么样」散到模板里，端点改名时要满世界找。

import (
	"strings"

	"go_wp/internal/builder/core"
)

// View 访客账号表单视图。
type View struct {
	// Mode 形态（模板用它写 data 属性，便于作者按形态覆写样式）。
	Mode string
	// Title 区块标题。
	Title string
	// ShowTitle 是否显示标题。
	ShowTitle bool
	// FragmentURL 片段地址（进页面即拉）。
	FragmentURL string
	// FallbackURL 无 JS 时的落点（user 模块的内置页面）。
	FallbackURL string
	// FallbackText 降级链接的文案。
	FallbackText string
	// Notice 无法渲染时的提示（缺站点工程 id）。空表示正常。
	Notice string
	// ScriptHint / OpenFallbackText 无 JS 时的降级文案（审计 I18N-010）：
	// 两句都已把表单名（FallbackText）填进占位符，模板直接输出。
	ScriptHint       string
	OpenFallbackText string
}

// CompileCSS 导出样式编译。
func CompileCSS(id string, p *Props, b *core.CSSBuckets) {
	compileCSS(id, p, b)
}

// BuildView 生成视图。
func BuildView(p *Props, projectID, lang string) View {
	mode := effectiveMode(p)
	view := View{
		Mode:         mode,
		Title:        effectiveTitle(p),
		ShowTitle:    p.ShowTitle,
		FallbackText: formSpecs[mode].title,
	}
	if strings.TrimSpace(projectID) == "" {
		// 片段端点按工程定位（槽位互链也要它）。没有工程 id 就给一句可见提示，
		// 而不是渲染一个永远空着的容器。
		view.Notice = "账号表单暂不可用（未取到站点工程）"
		return view
	}
	view.FragmentURL, view.FallbackURL = buildFragmentURL(mode, projectID, lang, p.Next)
	return view
}
