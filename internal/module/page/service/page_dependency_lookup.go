package pageservice

// page_dependency_lookup.go — 按依赖键**只读**反查页面的 service 面（反查面）。
//
// 与 page_dependency.go 的分工：那个文件是写侧（落依赖行 + 按依赖键把命中的页面标记 stale），
// 本文件只读 —— 回答「谁声明过这条依赖」，全程没有一条 UPDATE，尤其不动 pages.stale。
//
// 为什么要把「只读反查」做成独立入口而不是顺手调 MarkStaleByDependency：
//   · 语义相反：写路径返回「这次被标记的页面」，反查要的是「现在声明着这条依赖的页面」；
//     在只读调用点上复用写路径，等于看一眼影响面就把全站相关页面标成待重建；
//   · 契约层因此也分成两个形状（pagecontract.PageDependencyLookup 与 PageService 上的
//     MarkStaleByDependency）。
//
// 本文件只做三件事：入参归一化、调 model、把行映射成契约投影并**按 id 去重排序**。
// 去重不是装饰：同一页面的活跃与暂存产物可能都声明了这条依赖（两个 artifact_id 各一行），
// 不去重时同一个页面会在影响面清单里出现两次 —— 而清单长度正是运营判断「影响几处」的依据。

import (
	"context"
	"sort"
	"strings"

	pagecontract "go_wp/internal/module/page/contract"
	pagemodel "go_wp/internal/module/page/model"
)

// FindPagesByDependency 实现 pagecontract.PageDependencyLookup：按依赖键只读反查页面。
//
// 命中口径（与写路径 MarkStaleByDependency 逐字一致）由 model 负责：该页面的活跃或暂存
// 产物在 page_dependencies 里声明了这条依赖。本层不重新解释它 —— 两个口径分叉的表现是
// 「按依赖标记的页面」与「按依赖反查的页面」不是同一批，而删除保护正是按后者放行 / 拦截。
//
// 失败即失败（不降级成空集合）：反查读不到与「没有引用」是两件事 —— 降级会让删除保护
// 把一次读取失败当成「没人引用」然后放行删模板。
func (s *Service) FindPagesByDependency(ctx context.Context, projectID, dependencyKind, dependencyKey string) (
	refs []pagecontract.DependencyPageRef, err error) {
	if s == nil || s.model == nil {
		return nil, ErrProjectRequired
	}
	pid, kind, key, err := normalizeDependencyLookup(projectID, dependencyKind, dependencyKey)
	if err != nil {
		return nil, err
	}
	if kind == "" || key == "" {
		// 空依赖键不是错误，但也不去查：拿空键查等于把「依赖键缺失」变成
		// 「全站页面都引用了它」（model 侧同样早退）。
		return nil, nil
	}
	rows, lerr := s.model.ListPagesByDependency(ctx, pid, kind, key)
	if lerr != nil {
		return nil, lerr
	}
	return dependencyRefsOf(rows), nil
}

// normalizeDependencyLookup 归一化反查入参（纯函数，便于单测）。
//
// projectID 必填：pages 带 FORCE 策略，**空工程 id 一律拒绝而不是退化成「不限工程」**——
// 后者会把别的工程的页面混进这次删除保护的影响面里，表现为「拦了一个不相干的页面」，
// 而拦截型错误的代价是运营去解绑一个根本无关的页面。
//
// kind / key 归一化后可能为空：这是**合法**的（返回空集合），由调用方决定是早退还是查询。
func normalizeDependencyLookup(projectID, dependencyKind, dependencyKey string) (pid, kind, key string, err error) {
	pid = strings.TrimSpace(projectID)
	if pid == "" {
		return "", "", "", ErrProjectRequired
	}
	// 依赖键原样 TrimSpace：page_dependencies 的写入侧（dependencyRows）也是 TrimSpace 后落库，
	// 两侧不一致时带空白的键永远查不到任何页面（表现为「明明有引用，反查却是空的」）。
	return pid, strings.TrimSpace(dependencyKind), strings.TrimSpace(dependencyKey), nil
}

// dependencyRefsOf 行 → 契约投影：按页面 id 去重、跳过空白 id、按 id 排序（纯函数，便于单测）。
//
// 去重的合并规则：同一页面出现多行时取**首次出现的标题**（同一页面的标题来自同一条
// draft_document，各行必然相同）。
//
// 排序是必需的：SQL 不排序（次序口径只留一份，见 model 的注释），调用方（影响面清单 /
// 日志与错误明细）拿到的是稳定次序 —— 不排的话同一个工程两次渲染的行序可能不同。
func dependencyRefsOf(rows []pagemodel.DependencyRefRow) []pagecontract.DependencyPageRef {
	if len(rows) == 0 {
		return nil
	}
	out := make([]pagecontract.DependencyPageRef, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for i := range rows {
		id := strings.TrimSpace(rows[i].ID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, pagecontract.DependencyPageRef{
			ID: id, Title: strings.TrimSpace(rows[i].Title),
		})
	}
	if len(out) == 0 {
		return nil
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
