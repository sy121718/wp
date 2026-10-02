package pageservice

// page_media_stale.go — page 侧实现 media 索要的换图失效通知端口
//（mediacontract.StaleMarker，见 media/contract/media_service.go 的「索要的端口」）。
//
// 为什么由 page 实现：引用集（谁引用了这张图）在 media 的表里，而「页面怎么算待重建」
// 在 pages 表里 —— 跨模块表访问是禁止的（AGENTS.md §表隔离），所以 media 只声明端口。
//
// 为什么放在 service 同包而不是 outbound/media/：实现用的是本模块自己的 model，
// 签名直接对得上、没有任何形状翻译（CLAUDE.md：这类「顺手满足对方端口」留在 service
// 同包 + 编译期断言）。多包一层反而要把 model 暴露出去。
//
// 与本模块其它标记入口的关系：MarkStaleForTheme / MarkStaleByDependency 按**依赖键**
// 命中，本入口按**显式 id 集合**命中 —— 后者与 MarkStaleByRegistryVersion 同形
// （调用方已经算出精确集合，不做任何全站标记）。

import (
	"context"
	"strings"
	"time"

	mediacontract "go_wp/internal/module/media/contract"
)

var _ mediacontract.StaleMarker = (*Service)(nil)

// RefKinds 本实现认领的引用方类型：媒体引用集里 kind = page 的那些。
func (s *Service) RefKinds() []string { return []string{mediacontract.RefKindPage} }

// MarkStaleByMediaRefs 把「产物里引用了这张图」的页面标记为待重建。
//
// 语义要点：
//   - 只处理 kind = page 的项（media 已按 RefKinds() 过滤，这里再判一次是为了
//     实现自身可独立成立 —— 端口语义不该依赖调用方守规矩）；
//   - 逐工程独立作用域执行（DB-009）：pages 带 FORCE 策略，未设 app.project_id 的
//     UPDATE 在非超级角色下静默 0 行 —— 表现是「换图后页面不更新」且日志无异常，
//     与端口未接入完全同形。非本工程的 id 由 project_id 条件 + 策略双重拦下；
//   - 返回 RETURNING id 回读集合（真正命中的页面），不是入参回显：入参里可能混着
//     已删除 / 不存在的 id，回显会让换图日志的影响面虚高；
//   - 失败即失败：任一个工程的事务失败就整体返回错误，不吞（media 侧据此把
//     「换图成功但引用方不会更新」透出给操作者）。
func (s *Service) MarkStaleByMediaRefs(ctx context.Context, refs []mediacontract.MediaRef) (marked []string, err error) {
	if s == nil || s.model == nil {
		return nil, ErrProjectRequired
	}
	ids := make([]string, 0, len(refs))
	seen := map[string]bool{}
	for i := range refs {
		if strings.TrimSpace(refs[i].Kind) != mediacontract.RefKindPage {
			continue
		}
		id := strings.TrimSpace(refs[i].ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	projectIDs, err := s.fanoutProjectIDs(ctx)
	if err != nil {
		return nil, err
	}
	at := time.Now()
	collector := &staleIDCollector{}
	for _, projectID := range projectIDs {
		if ctx.Err() != nil {
			return collector.list(), ctx.Err()
		}
		hit, merr := s.model.MarkStaleByIDs(ctx, projectID, ids, at)
		if merr != nil {
			return nil, merr
		}
		collector.add(hit)
	}
	// 影响面留痕走本模块既有那份实现（page_stale_overview.go）：理由与其它标记入口
	// 一致 —— 同一个 id 集合在任何标注里都该显示同一个名字。
	s.logStaleImpact(ctx, "media_replace", collector.list())
	return collector.list(), nil
}
