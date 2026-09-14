package pageservice

// page_navigation.go — 页面构建上下文（路径 / 工程 ID 解析）。

import (
	"context"
	"strings"

	pagecontract "go_wp/internal/module/page/contract"
)

// pageContextOf 按页面 ID 取所属站点工程 ID 与该语言的「逻辑访问路径」。
// 工程 ID 用于导航解析（缺失时绑定菜单位置的导航节点编译期显式报错）；
// 逻辑路径用于导航「当前项」高亮与 hreflang 互指——调用方会再经 sitePath 加语言前缀。
//
// 多语言 P3 修正：此前取 pages.active_path（可能已带语言前缀），再经 highlightPath
// 加一次前缀会得到 /en/en/about，导航高亮永远匹配不上；现在按语言取
// page_publications 的行并用同一 LangURLRule 剥掉语言前缀，未发布回退草稿路径
// （草稿路径本就是逻辑路径）。
func (s *Service) pageContextOf(ctx context.Context, pageID, lang string) (projectID, logicalPath string) {
	if strings.TrimSpace(pageID) == "" {
		return "", ""
	}
	page, err := s.model.GetByID(ctx, pageID, "")
	if err != nil || page == nil {
		return "", ""
	}
	logicalPath = page.DraftPath
	if pub, perr := s.model.GetPublication(ctx, pageID, buildLang(lang)); perr == nil && pub != nil && pub.ActivePath != "" {
		logicalPath = s.langURLRuleOf(ctx, page.ProjectID).Strip(buildLang(lang), pub.ActivePath)
	}
	return page.ProjectID, logicalPath
}

var _ pagecontract.SitePageResolver = (*Service)(nil)
