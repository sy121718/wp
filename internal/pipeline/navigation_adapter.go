package pipeline

// navigation_adapter.go — navigation 契约 → builder.NavigationResolver（EDT-003）。

import (
	"context"

	"go_wp/internal/builder/core"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/i18n"
)

// NavigationAdapter 构建期导航菜单解析（单次编译内缓存）。
type NavigationAdapter struct {
	Svc       navigationcontract.NavigationService
	Project   projectcontract.ProjectService
	Ctx       context.Context
	ProjectID string
	Lang      string
	// ContentStore 内容译文存储（可空：空则用 pkg/i18n 的默认存储）。
	// 留成字段而不是直接调默认存储，是为了让「标签确实经译文回填」这件事可单测 ——
	// 否则这条链只能靠起库的集成测试覆盖。
	ContentStore i18n.ContentStore
	cache        map[string][]core.NavigationItem
}

// ResolveMenu 按工程 + 位置返回菜单项树。
func (a *NavigationAdapter) ResolveMenu(projectID, kind string) (items []core.NavigationItem, err error) {
	key := projectID + "|" + kind
	if a.cache != nil {
		if cached, ok := a.cache[key]; ok {
			return cached, nil
		}
	}
	nodes, err := a.Svc.Tree(a.Ctx, projectID, kind)
	if err != nil {
		return nil, err
	}
	items = a.itemsOf(nodes, projectID)
	a.translateLabels(items, projectID)
	if a.cache == nil {
		a.cache = map[string][]core.NavigationItem{}
	}
	a.cache[key] = items
	return items, nil
}

// translateLabels 用 sys_translation 回填菜单标签（审计 I18N-007 / I18N-018）。
//
// 为什么必须在这里做：**菜单标签不在页面文档里**，而在 navigation 模块的节点上 ——
// 页面翻译工作台看不见它（Props 里根本没有这个值），组件侧声明的 Translatable
// 白名单对它同样无效。两条模式的差异因此在代码里说清楚了：
//   - Menu 模式（header/footer）：label 来自 navigation，译文由 resolver 在这里补；
//   - 自定义菜单模式：label 在页面文档里，走 Translatable → 页面翻译工作台。
//
// 批量：先收集全部原文算 hash，一次查询建取词器，再回填 —— 逐个查会按菜单项数
// 放大成 N 次 SQL，而构建期**每个页面**都会走这条路。
func (a *NavigationAdapter) translateLabels(items []core.NavigationItem, projectID string) {
	if a.Lang == "" || len(items) == 0 {
		return
	}
	var sources []string
	var collect func(list []core.NavigationItem)
	collect = func(list []core.NavigationItem) {
		for i := range list {
			if i18n.ShouldTranslateContent(list[i].Label) {
				sources = append(sources, list[i].Label)
			}
			collect(list[i].Children)
		}
	}
	collect(items)
	if len(sources) == 0 {
		return // 全是空标签 / 纯符号：本就不该进译文表，也不必建取词器
	}
	hashes := make([]string, 0, len(sources))
	for _, s := range sources {
		hashes = append(hashes, i18n.ContentHash(s))
	}
	// 工程作用域（审计 I18N-009）：菜单标签的译文与页面正文同一套隔离规则，
	// 工程 id 直接用 ResolveMenu 的入参（适配器已按工程缓存菜单，作用域必须一致）。
	tr := i18n.NewContentTranslatorScoped(a.Ctx, projectID, a.ContentStore, a.Lang, hashes)
	contextName := i18n.ContentContext("navigation", "label")
	var apply func(list []core.NavigationItem)
	apply = func(list []core.NavigationItem) {
		for i := range list {
			list[i].Label = tr.TranslateContent(list[i].Label, contextName)
			apply(list[i].Children)
		}
	}
	apply(items)
}

func (a *NavigationAdapter) itemsOf(nodes []*navigationdto.NavigationNode, projectID string) []core.NavigationItem {
	out := make([]core.NavigationItem, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, core.NavigationItem{
			Label:    n.Title,
			URL:      LocalizeMenuURL(a.Ctx, a.Project, projectID, a.Lang, n.Path),
			Target:   n.Target,
			Children: a.itemsOf(n.Children, projectID),
		})
	}
	return out
}
