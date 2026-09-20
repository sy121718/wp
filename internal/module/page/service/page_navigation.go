package pageservice

// page_navigation.go — 页面构建上下文（路径 / 工程 ID 解析）。

import (
	"context"
	"strings"

	pagecontract "go_wp/internal/module/page/contract"
	"go_wp/internal/pipeline"
)

// pageContextOf 按页面 ID 取所属站点工程 ID 与该语言的「逻辑访问路径」。
// 工程 ID 用于导航解析（缺失时绑定菜单位置的导航节点编译期显式报错）；
// 逻辑路径用于导航「当前项」高亮与 hreflang 互指——调用方会再经 sitePath 加语言前缀。
//
// 多语言 P3 修正：此前取 pages.active_path（可能已带语言前缀），再经 highlightPath
// 加一次前缀会得到 /en/en/about，导航高亮永远匹配不上；现在按语言取
// page_publications 的行并用同一 LangURLRule 剥掉语言前缀，未发布回退草稿路径
// （草稿路径本就是逻辑路径）。
//
// 默认语言现场解析。构建 / 重建口径请走 pageContextOfWithDefault ——「哪个语言不带前缀」
// 由默认语言决定，现场解析会让改过 is_default 之后的重建把 /en/about 当成「默认语言路径」
// 原样留下，再加一次前缀就变成 /en/en/about（产物互指指向不存在的地址，且没有任何报错）。
func (s *Service) pageContextOf(ctx context.Context, pageID, lang string) (projectID, logicalPath string) {
	return s.pageContextOfWithDefault(ctx, pageID, lang, "")
}

// pageContextOfWithDefault 用**给定**默认语言剥语言前缀（审计 I18N-01 冻结口径）。
//
// defaultLang 为空 = 现场解析（预览 / 后台口径，与改造前逐字一致）。
func (s *Service) pageContextOfWithDefault(ctx context.Context, pageID, lang, defaultLang string) (projectID, logicalPath string) {
	if strings.TrimSpace(pageID) == "" {
		return "", ""
	}
	// 逐工程定位（DB-009 第四批）：调用方只有 pageId；不带作用域的直查在换非超级角色后
	// 一律失败 —— 这里的失败是**静默降级**（导航高亮悄悄消失），比报错更难发现。
	page, err := s.locatePageInProjects(ctx, pageID)
	if err != nil || page == nil {
		return "", ""
	}
	logicalPath = page.DraftPath
	if pub, perr := s.model.GetPublication(ctx, pageID, buildLang(lang)); perr == nil && pub != nil && pub.ActivePath != "" {
		rule := s.langURLRuleOf(ctx, page.ProjectID)
		if dl := strings.TrimSpace(defaultLang); dl != "" {
			rule = pipeline.LangURLRuleForProjectWithDefault(ctx, s.project, page.ProjectID, dl)
		}
		logicalPath = rule.Strip(buildLang(lang), pub.ActivePath)
	}
	return page.ProjectID, logicalPath
}

var _ pagecontract.SitePageResolver = (*Service)(nil)
