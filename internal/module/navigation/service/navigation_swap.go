// navigation_swap.go — 同级排序的原子交换（SwapMove）。
//
// 背景（历史 P1）：页面 handler 曾自己算同级邻位，然后连调两次 Update —— 各自一个事务。
// 第二次失败时第一条**已经提交**：同级出现两个相同的 sort_order，而第一条的失效也早已
// 派发出去，已发布页面按「半交换」的中间态重建。调用方看到的是一次「失败」，既不会重试
// 也不会回滚，只能靠人发现。
//
// 本文件把这些收进一次原子调用：读树快照 → 选邻位 → **一个事务**里两条条件 UPDATE
// （model.SwapSortOrders）→ 提交成功后才派发失效。任何一步不成立都零写零失效：
//
//	· 已在首/末位 → Moved=false（与修复前的行为一致，页面照旧提示「已完成」）；
//	· 同级 sort_order 并列 → ErrSortTied（交换取值等于没换，显式说「未产生位移」）；
//	· 任一行版本已变 / 行没了 → 整事务回滚，冲突行按既有乐观锁文案打回给人。
package navigationservice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"go_wp/internal/module/navigation/dto"
	"go_wp/internal/module/navigation/enums"
	"go_wp/internal/module/navigation/model"
)

// 移动方向（页面 move 表单的两个取值，与前端按钮一一对应）。
const (
	dirUp   = "up"
	dirDown = "down"
)

// swapReasonBoundary 未位移的原因：该项已在同级首/末位。
const swapReasonBoundary = "boundary"

// SwapMove 同级上移/下移：把该项与相邻项（同工程、同位置、同父级）的 sort_order 换过来。
//
// 流程与每步的理由：
//  1. locateNavigation 逐工程定位归属（DB-009）：只带 id 的入口不按 id 直查，
//     否则换非超级角色后是静默的 ErrRecordNotFound；
//  2. **工程作用域比对**：调用方给的 ProjectID（页面表单里的那个）必须就是该项真实所属，
//     不一致即拒 —— 移动是同一棵树内部的相对位置写入，跨工程没有语义；
//  3. Tree 取该工程该位置的树 → 取同级（父级空即顶级）：邻位只能从同一份快照里选，
//     不能按全局 sort_order 排序去猜（同级之外的行与本次写入无关）；
//  4. 边界早退（零写）；并列早退（零写，ErrSortTied）；
//  5. 两行的版本 token 取**本次快照**（节点上的 UpdatedAt，RFC3339Nano）→ 一次原子交换；
//  6. 只有 model 返回成功（= 两条 UPDATE 都命中且已提交）才派发失效：
//     menu:{projectID}:{kind} + 父项（顺序变了的是父项子树）+ 被交换的两项。
func (s *Service) SwapMove(ctx context.Context, req *navigationdto.SwapMoveReq) (res *navigationdto.SwapMoveResp, err error) {
	if req == nil {
		return nil, navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	id := strings.TrimSpace(req.ID)
	dir := strings.TrimSpace(req.Dir)
	if id == "" || (dir != dirUp && dir != dirDown) {
		return nil, navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	item, err := s.locateNavigation(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, navigationenums.Err(navigationenums.ErrNotFound)
		}
		return nil, err
	}
	if want := strings.TrimSpace(req.ProjectID); want != "" && want != item.ProjectID {
		// 跨工程一律拒：调用方手上的工程与该项真实归属不一致时，改的不是它以为的那棵树。
		return nil, navigationenums.Err(navigationenums.ErrInvalidParam)
	}
	nodes, err := s.Tree(ctx, item.ProjectID, item.Kind)
	if err != nil {
		return nil, err
	}
	siblings := navSiblingsOf(nodes, item.ParentID)
	idx := -1
	for i, node := range siblings {
		if node.ID == item.ID {
			idx = i
			break
		}
	}
	if idx < 0 {
		// 定位到了行、却不在它自己的同级列表里：父子关系或作用域已经不一致（异常数据）。
		return nil, navigationenums.Err(navigationenums.ErrNotFound)
	}
	next := idx - 1
	if dir == dirDown {
		next = idx + 1
	}
	if next < 0 || next >= len(siblings) {
		// 已到边界（按钮在首/末项上本就是禁用态，这里兜手工请求）：列表顺序未改动。
		return &navigationdto.SwapMoveResp{Moved: false, Reason: swapReasonBoundary}, nil
	}
	a, b := siblings[idx], siblings[next]
	if a.SortOrder == b.SortOrder {
		// 并列：交换取值不产生位移（并列由 id ASC 决胜）。零写，明确回业务文案。
		return nil, navigationenums.Err(navigationenums.ErrSortTied)
	}
	expA, err := parseSnapshotToken(a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	expB, err := parseSnapshotToken(b.UpdatedAt)
	if err != nil {
		return nil, err
	}
	err = s.m.SwapSortOrders(ctx, item.ProjectID,
		navigationmodel.SwapRow{ID: a.ID, SortOrder: b.SortOrder, Expected: expA},
		navigationmodel.SwapRow{ID: b.ID, SortOrder: a.SortOrder, Expected: expB})
	if err != nil {
		var conflict *navigationmodel.SwapConflictError
		if errors.As(err, &conflict) {
			return nil, staleVersionMessage(s.staleTitle(ctx,
				&navigationmodel.NavigationEntity{ProjectID: item.ProjectID, ID: conflict.ID,
					Title: snapshotTitle(nodes, conflict.ID)}))
		}
		return nil, err
	}
	s.invalidateMenu(ctx, item.ProjectID, item.Kind)
	if item.ParentID != nil && strings.TrimSpace(*item.ParentID) != "" {
		// 顺序变了的是**父项**的子树：按项引用父项的页面/实例必须重建。
		// 顶层没有父项 —— 那里由上面 menu 键覆盖。
		s.invalidateNavigation(ctx, item.ProjectID, strings.TrimSpace(*item.ParentID))
	}
	s.invalidateNavigation(ctx, item.ProjectID, a.ID)
	s.invalidateNavigation(ctx, item.ProjectID, b.ID)
	return &navigationdto.SwapMoveResp{Moved: true}, nil
}

// parseSnapshotToken 把树节点上的乐观锁 token 解析回时刻。
//
// token 由服务端自己写在节点上（buildNode 的 n.UpdatedAt.Format(time.RFC3339Nano)），
// 所以解析失败不是「调用方参数错」，而是本模块内部不一致 —— 归口成 ErrInternal，
// 不套用 parseExpectedUpdatedAt 的 ErrInvalidParam（那是给外部回带值用的口径）。
func parseSnapshotToken(raw string) (time.Time, error) {
	t, err := time.ParseInLocation(time.RFC3339Nano, strings.TrimSpace(raw), time.Local)
	if err != nil {
		// 内部不一致（服务端自己写下的 token 解析不回来）→ 归口成真内部错误：
		// 不是业务文案，出口给 500 + 通用提示，原文进日志。
		return time.Time{}, fmt.Errorf("导航树快照 token 解析失败: %w", err)
	}
	return t, nil
}

// navSiblingsOf 返回指定父级下的同级节点列表（父级为空即顶级）。
//
// 与页面侧原先的 navSiblings 同一读法；搬迁的理由：邻位选择是**业务规则**（本地表里
// 只能在同一份树快照的同级内选），不该由页面 handler 各自实现一份。
func navSiblingsOf(nodes []*navigationdto.NavigationNode, parentID *string) []*navigationdto.NavigationNode {
	if parentID == nil || *parentID == "" {
		return nodes
	}
	for _, n := range nodes {
		if n.ID == *parentID {
			return n.Children
		}
		if found := navSiblingsOf(n.Children, parentID); found != nil {
			return found
		}
	}
	return nil
}

// snapshotTitle 在树里按 id 找节点标题（冲突提示的兜底定位信息）。
//
// 为什么要有兜底：staleTitle 优先取库内**当前**标题，但冲突行可能刚被删掉 ——
// 那时只能用手上这份快照里的标题，总比给用户一句没有定位的「请刷新」强。
func snapshotTitle(nodes []*navigationdto.NavigationNode, id string) string {
	for _, n := range nodes {
		if n.ID == id {
			return n.Title
		}
		if t := snapshotTitle(n.Children, id); t != "" {
			return t
		}
	}
	return ""
}
