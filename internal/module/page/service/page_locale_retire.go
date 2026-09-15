package pageservice

// page_locale_retire.go — 禁用语言后下线该语言的路由（审计 I18N-017）。
//
// 语言从清单里移除后，该语言的已激活路由就成了**无人认领**的状态：访问面还在服务
// /en/…，而后台没有任何入口能改它或下掉它，只能人工登机器删符号链接。
//
// 本文件实现 project 模块声明的 LocaleRetirePort，由装配期注入 —— 方向是
// project → 端口 → page。不能反过来（page 依赖 project，反向成环）。
//
// 语言**直接查库**（page_publications.lang），不从路径前缀反推：路径前缀是展示规则、
// 语言是事实，用规则反推会在改过前缀配置的站点上删错语言（前缀改成 /eng 之后，
// 按 /en 反推的代码要么删不到、要么删到别的语言）。
//
// 不做 301：把旧路径指向默认语言的同名路径需要**生成一份重定向产物**
//（publication 的 Redirect 只接受 ArtifactID / PageID，没有「指向任意路径」的形态），
// 那是另一个量级的工作。审计里这条本就是可选项，默认走下线。

import (
	"context"
	"strings"

	projectcontract "go_wp/internal/module/project/contract"
	pubcontract "go_wp/internal/module/publication/contract"
)

// localeActiveRoute 该语言的一条已激活路由。
type localeActiveRoute struct {
	PageID string
	Path   string
}

// LocaleRetireImpact 该语言当前的已激活路径数（审计 I18N-017，确认前给运营看代价）。
func (s *Service) LocaleRetireImpact(ctx context.Context, projectID, lang string) (affected int, err error) {
	routes, err := s.localeActiveRoutes(ctx, projectID, lang)
	if err != nil {
		return 0, err
	}
	return len(routes), nil
}

// RetireLocale 下线该语言的全部已激活路由，并清理它的发布 / 暂存指针。
//
// 顺序：先 DB 路由占用、再访问面符号链接、最后发布指针。
// 访问面必须在发布指针之前清 —— 指针没了就会有人在后台看到「这个页面未发布」，
// 而访问面上产物还在服务；反过来（先清指针）留下的窗口里，符号链接是唯一线索。
func (s *Service) RetireLocale(ctx context.Context, projectID, lang string) (retired int, err error) {
	routes, err := s.localeActiveRoutes(ctx, projectID, lang)
	if err != nil {
		return 0, err
	}
	if len(routes) == 0 {
		return 0, nil
	}
	if s.routes == nil {
		return 0, nil // 降级装配（单测 / 精简部署）：没有路由契约就没有可下线的路径
	}
	paths := make([]string, 0, len(routes))
	for _, r := range routes {
		if derr := s.routes.Deactivate(ctx, &pubcontract.DeactivateReq{
			ProjectID: projectID, Path: r.Path,
		}); derr != nil {
			return retired, derr
		}
		paths = append(paths, r.Path)
		retired++
	}
	// 只删 DB 行会让 /site 继续输出旧产物（符号链接才是访问面的真源），
	// 且此后没有任何入口能查到该清哪个链接 —— 与页面删除同一根因。
	if err = s.deactivatePaths(paths); err != nil {
		return retired, err
	}
	// 发布 / 暂存指针：**只删该语言** —— 另一语言还在服务，整页删会让它失去已发布状态。
	seen := map[string]bool{}
	for _, r := range routes {
		if seen[r.PageID] {
			continue
		}
		seen[r.PageID] = true
		if perr := s.model.DeletePublicationsByLang(ctx, r.PageID, lang); perr != nil && err == nil {
			err = perr
		}
		if serr := s.model.DeleteStagingsByLang(ctx, r.PageID, lang); serr != nil && err == nil {
			err = serr
		}
	}
	return retired, err
}

// localeActiveRoutes 该语言在本工程的全部已激活路由。
func (s *Service) localeActiveRoutes(ctx context.Context, projectID, lang string) (out []localeActiveRoute, err error) {
	if s.routes == nil || strings.TrimSpace(projectID) == "" || strings.TrimSpace(lang) == "" {
		return nil, nil
	}
	pages, err := s.model.ListAll(ctx, projectID, "")
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(pages))
	for i := range pages {
		ids = append(ids, pages[i].ID)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	pubs, err := s.model.ListPublicationsByPages(ctx, ids, lang)
	if err != nil {
		return nil, err
	}
	for i := range pubs {
		if strings.TrimSpace(pubs[i].ActivePath) == "" {
			continue
		}
		out = append(out, localeActiveRoute{PageID: pubs[i].PageID, Path: pubs[i].ActivePath})
	}
	return out, nil
}

// 编译期断言：page 模块实现 project 声明语言下线端口。
var _ projectcontract.LocaleRetirePort = (*Service)(nil)
