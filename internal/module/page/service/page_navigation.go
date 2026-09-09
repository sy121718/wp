package pageservice

// page_navigation.go — 导航装配：navigation 契约 → builder 的 core.NavigationResolver。
// core.nav 节点绑定菜单位置（header/footer）时，构建期经此把导航记录解析为静态
// 菜单项树；产物仍是纯静态 HTML（访客请求零查库，docs/01 §1 不变量）。
// 与 core.globalref 的 blockResolverAdapter 同源：跨模块只依赖 contract，
// 本模块负责把契约数据适配成内核接口。

import (
	"context"
	"strings"

	"go_wp/internal/builder/core"
	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
)

// navigationResolverAdapter 适配 navigation 契约为 builder 的 NavigationResolver。
// cache 为单次编译内的解析缓存（键：工程 ID + 位置），同一页面多个导航节点只查一次库。
type navigationResolverAdapter struct {
	svc navigationcontract.NavigationService
	// s 装配服务：菜单项站内 URL 需按站点语言 URL 方案本地化（s.localizeMenuURL）。
	s   *Service
	ctx context.Context
	// lang 本次构建语言（完整码）：菜单项站内 URL 按它映射访问路径
	//（多语言 P2/P3，docs/06-D §4.1 第 6 项）。
	lang  string
	cache map[string][]core.NavigationItem
}

// ResolveMenu 按工程 + 位置返回菜单项树（navigation 模块负责树组装与排序）。
func (a navigationResolverAdapter) ResolveMenu(projectID, kind string) (items []core.NavigationItem, err error) {
	key := projectID + "|" + kind
	if a.cache != nil {
		if cached, ok := a.cache[key]; ok {
			return cached, nil
		}
	}
	nodes, err := a.svc.Tree(a.ctx, projectID, kind)
	if err != nil {
		return nil, err
	}
	items = a.itemsOf(nodes, projectID)
	if a.cache != nil {
		a.cache[key] = items
	}
	return items, nil
}

// itemsOf 树节点 → 构建期菜单项（递归展开子菜单）。
// 站内链接按「工程 + 构建语言」本地化（默认语言无前缀、非默认语言短码前缀；
// off 方案下等价原样输出）。
func (a navigationResolverAdapter) itemsOf(nodes []*navigationdto.NavigationNode, projectID string) []core.NavigationItem {
	out := make([]core.NavigationItem, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, core.NavigationItem{
			Label:    n.Title,
			URL:      a.s.localizeMenuURL(a.ctx, projectID, a.lang, n.Path),
			Target:   n.Target,
			Children: a.itemsOf(n.Children, projectID),
		})
	}
	return out
}

// pageContextOf 按页面 ID 取所属站点工程 ID 与该语言的「逻辑访问路径」。
// 工程 ID 用于导航解析（缺失时绑定菜单位置的导航节点编译期显式报错）；
// 逻辑路径用于导航「当前项」高亮与 hreflang 互指——调用方会再经 sitePath 加语言前缀。
//
// 多语言 P3 修正：此前取 pages.active_path（可能已带语言前缀），再经 highlightPath
// 加一次前缀会得到 /en/en/about，导航高亮永远匹配不上；现在按语言取
// page_publications 的行并用同一 LangURLRule 剥掉语言前缀，未发布回退草稿路径
//（草稿路径本就是逻辑路径）。
func (s *Service) pageContextOf(ctx context.Context, pageID, lang string) (projectID, logicalPath string) {
	if strings.TrimSpace(pageID) == "" {
		return "", ""
	}
	page, err := s.model.GetByID(ctx, pageID)
	if err != nil || page == nil {
		return "", ""
	}
	logicalPath = page.DraftPath
	if pub, perr := s.model.GetPublication(ctx, pageID, buildLang(lang)); perr == nil && pub != nil && pub.ActivePath != "" {
		logicalPath = s.langURLRuleOf(ctx, page.ProjectID).Strip(buildLang(lang), pub.ActivePath)
	}
	return page.ProjectID, logicalPath
}
