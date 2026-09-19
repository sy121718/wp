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

	"gorm.io/gorm"

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
// 两段，顺序刻意如此（2026-09 收口；原实现是三段各自提交）：
//
//  1. **先清访问面符号链接**（deactivatePaths）：跨系统动作，不能进数据库事务 ——
//     事务回滚撤不掉已经删掉的符号链接，把它塞进事务只是换一种半截状态
//     （DB 回滚了、访问面却已经下线）。它自身幂等（删不存在的链接不报错），
//     所以「链接已删、后面的事务失败」是可重跑收敛的：page_publications 的指针还在，
//     下一次 RetireLocale 仍能从它重新算出这批路径。
//  2. **再在一个事务里做全部数据库写入**：publication 的 page_routes 占用解除
//     （契约里的 DeactivateTx）+ page 自己的发布 / 暂存指针删除（两个 …Tx 变体）。
//     这两半此前各自提交，中途失败会留下「页面已退役但路径仍占用」或「路径已释放
//     但指针还在（后台仍显示已发布）」；现在要么都生效、要么都不生效。
//
// 为什么符号链接必须排在事务**之前**而不是提交之后：待清理的路径是从 page_publications
// 反查出来的（localeActiveRoutes），指针一旦提交删除就再也没有入口能算出该清哪些链接 ——
// 那时若符号链接删除失败（权限 / IO），残留链接会永久留在访问面上且无人认领，重跑也找不到它。
// 反过来（先删链接、事务再失败）数据库保持完整，重跑即收敛，方向是 fail closed。
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
	// 只删 DB 行会让 /site 继续输出旧产物（符号链接才是访问面的真源），
	// 且此后没有任何入口能查到该清哪个链接 —— 与页面删除同一根因。
	paths := make([]string, 0, len(routes))
	seenPath := map[string]bool{}
	for _, r := range routes {
		if seenPath[r.Path] {
			continue
		}
		seenPath[r.Path] = true
		paths = append(paths, r.Path)
	}
	if err = s.deactivatePaths(paths); err != nil {
		return 0, err
	}
	// 事务：page_routes 占用解除 + page 的发布 / 暂存指针，两半同进同出。
	// 作用域由 TransactionScoped 设（不要在这里再调 rls.InProjectScope：它自带事务边界，
	// 会在事务里另开一个 —— 见 pkg/rls.ScopeTx 的论证）。
	if terr := s.model.TransactionScoped(ctx, projectID, func(tx *gorm.DB) error {
		for _, r := range routes {
			if derr := s.routes.DeactivateTx(ctx, tx, &pubcontract.DeactivateReq{
				ProjectID: projectID, Path: r.Path,
			}); derr != nil {
				return derr
			}
		}
		// 发布 / 暂存指针：**只删该语言** —— 另一语言还在服务，整页删会让它失去已发布状态。
		seenPage := map[string]bool{}
		for _, r := range routes {
			if seenPage[r.PageID] {
				continue
			}
			seenPage[r.PageID] = true
			if perr := s.model.DeletePublicationsByLangTx(ctx, tx, projectID, r.PageID, lang); perr != nil {
				return perr
			}
			if serr := s.model.DeleteStagingsByLangTx(ctx, tx, projectID, r.PageID, lang); serr != nil {
				return serr
			}
		}
		return nil
	}); terr != nil {
		// 事务回滚 ⇒ 一条路由都没退役：返回 0，而不是把事务里数到一半的计数当成功报出去。
		return 0, terr
	}
	return len(routes), nil
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
