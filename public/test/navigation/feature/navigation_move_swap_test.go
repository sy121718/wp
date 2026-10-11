// navigation_move_swap_test.go — 同级上移/下移的原子交换（task-84）。
//
// 修复前的形态（历史 P1）：页面 handler 自己算同级邻位，然后连调两次 Update —— 各自一个
// 事务。第二次失败时第一条已经提交：同级出现两个相同的 sort_order，而第一条的 stale 也
// 已经派发出去，已发布页面按半交换的中间态重建。本文件从服务端链路（真实 PG）验四件事：
//
//   - 正常路径：一次调用两行交换 + 提交后**一次**派发（menu + 父项 + 两项）；
//   - 首/末位 与 同级排序并列：零写零派发（后者回业务文案 ErrSortTied，不回「已完成」）；
//   - 第二条冲突（Y 窗口）：整事务回滚、dispatcher **0 次** —— 这是本卡的主断言；
//   - 并发（Z2）：终态只能是「交换前」或「交换后」，绝不出现半交换。
//
// 为什么锁窗用例值得这么写：要证伪「半交换」必须让第二条稳定地失败。拿「先用另一个连接
// 改掉版本、再调 SwapMove」是不成立的 —— 服务端会先读树快照，读到的是**新版**，
// 那样两条都会成功。所以窗口必须钉在「快照已读、第一条 UPDATE 已执行」之间：
// holder 只锁住**会先被写的那一行**（model 按 id 升序执行），让 swap 阻塞在那里，
// 再在 holder 的同一个事务里推走另一行的版本并提交。观测用 pg_blocking_pids 定点，
// 不靠 sleep 撞运气。
package feature

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
	navigationenums "go_wp/internal/module/navigation/enums"
)

// —— 计数用失效派发桩 ——

// countingNavDispatcher 记录失效派发次数（swap 在失败档必须零派发，成功档恰好一次）。
//
// 为什么不用生产装配里的 pipeline.NewMenuStaleAdapter：那个把 stale 落进依赖表，
// 读到的是「哪些键被标脏」，而本卡要断的是**失败时一次都没派发**，计数桩最直接。
type countingNavDispatcher struct {
	mu   sync.Mutex
	menu []string
	nav  []string
}

func (d *countingNavDispatcher) InvalidateMenu(_ context.Context, projectID, kind string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.menu = append(d.menu, projectID+":"+kind)
	return nil
}

func (d *countingNavDispatcher) InvalidateNavigation(_ context.Context, projectID, navigationID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.nav = append(d.nav, projectID+":"+navigationID)
	return nil
}

func (d *countingNavDispatcher) counts() (menu, nav int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.menu), len(d.nav)
}

// navDispatchBase 一次「本步」派发的基线：建项（Create）自己也会派发，断言必须看增量，
// 否则「零派发」会被建项阶段的次数蒙过去。
type navDispatchBase struct{ menu, nav int }

func dispatchBase(env *navSwapEnv) navDispatchBase {
	menu, nav := env.Dispatcher.counts()
	return navDispatchBase{menu: menu, nav: nav}
}

func assertDispatchDelta(t *testing.T, env *navSwapEnv, base navDispatchBase, wantMenu, wantNav int, step string) {
	t.Helper()
	menu, nav := env.Dispatcher.counts()
	if menu-base.menu != wantMenu || nav-base.nav != wantNav {
		t.Fatalf("%s：本步应派发 menu×%d + nav×%d，实际 menu×%d + nav×%d（累计 menu=%d nav=%d；建项等前置已派发 menu×%d nav×%d）",
			step, wantMenu, wantNav, menu-base.menu, nav-base.nav, menu, nav, base.menu, base.nav)
	}
}

// navSwapEnv 复用既有导航用例的装配（真实迁移建表 + 真实工程行），并接上计数派发桩。
type navSwapEnv struct {
	DB         *gorm.DB
	Nav        navigationcontract.NavigationService
	ProjectID  string
	Dispatcher *countingNavDispatcher
}

func newNavSwapEnv(t *testing.T) *navSwapEnv {
	t.Helper()
	db, _, navSvc, projectID := newNavigationEnv(t)
	dispatcher := &countingNavDispatcher{}
	navSwapAttachDispatcher(t, navSvc, dispatcher)
	t.Cleanup(func() { navSwapAttachDispatcher(t, navSvc, nil) })
	return &navSwapEnv{DB: db, Nav: navSvc, ProjectID: projectID, Dispatcher: dispatcher}
}

// navSwapAttachDispatcher 把计数桩挂到真实 service 上。
//
// 端口注入点（SetMenuStaleDispatcher）**不在** contract 里 —— 它只在装配期被调用，
// 所以这里用匿名接口断言（与同包 navStaleSpyAttach 同一手法）：注入点改名或挪走时
// 会立刻红，而不是静默不计数。
func navSwapAttachDispatcher(t *testing.T, svc navigationcontract.NavigationService, dispatcher navigationcontract.MenuStaleDispatcher) {
	t.Helper()
	setter, ok := svc.(interface {
		SetMenuStaleDispatcher(navigationcontract.MenuStaleDispatcher)
	})
	if !ok {
		t.Fatalf("NavSvc 未暴露 SetMenuStaleDispatcher：端口注入点变了，本用例失去计数能力")
	}
	setter.SetMenuStaleDispatcher(dispatcher)
}

// navSwapRowState 一行的持久化状态（排序值 + 版本 token）。
type navSwapRowState struct {
	sort    int
	updated time.Time
}

// readNavSwapState 直接读库（不经过 service），基线比对用。
func readNavSwapState(t *testing.T, db *gorm.DB, id string) navSwapRowState {
	t.Helper()
	var state navSwapRowState
	if err := db.Raw("SELECT sort_order, update_time FROM navigations WHERE id = ?", id).
		Row().Scan(&state.sort, &state.updated); err != nil {
		t.Fatalf("读回导航项 %s 失败: %v", id, err)
	}
	return state
}

// sortedIDs 把两个 id 按**字节序升序**摆好：model 的 SwapSortOrders 就是按这个顺序写。
func sortedIDs(a, b string) (first, second string) {
	if strings.Compare(a, b) < 0 {
		return a, b
	}
	return b, a
}

// swapPairIDs 取「上移/下移」那一对（与 handler 同算法：同级内的相邻两项）。
func swapPairIDs(t *testing.T, env *navSwapEnv, id, dir string) (moved, neighbour string) {
	t.Helper()
	item := getNavItem(t, env, id)
	nodes, err := env.Nav.Tree(context.Background(), item.ProjectID, item.Kind)
	if err != nil {
		t.Fatalf("读取导航树失败: %v", err)
	}
	siblings := flatSiblings(nodes, item.ParentID)
	idx := -1
	for i, n := range siblings {
		if n.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("同级里找不到 %s", id)
	}
	next := idx - 1
	if dir == "down" {
		next = idx + 1
	}
	if next < 0 || next >= len(siblings) {
		t.Fatalf("选了边界项（%s / %s），用例需要能交换的一对", id, dir)
	}
	return siblings[idx].ID, siblings[next].ID
}

// flatSiblings 返回指定父级下的同级节点（父级为空即顶级）。
func flatSiblings(nodes []*navigationdto.NavigationNode, parentID *string) []*navigationdto.NavigationNode {
	if parentID == nil || *parentID == "" {
		return nodes
	}
	for _, n := range nodes {
		if n.ID == *parentID {
			return n.Children
		}
		if found := flatSiblings(n.Children, parentID); found != nil {
			return found
		}
	}
	return nil
}

// siblingsOfSvc 取某项的同级列表（同一份树快照口径）。
func siblingsOfSvc(t *testing.T, svc navigationcontract.NavigationService, id string) []*navigationdto.NavigationNode {
	t.Helper()
	item, err := svc.Get(context.Background(), &navigationdto.GetReq{ID: id})
	if err != nil {
		t.Fatalf("读取导航项 %s 失败: %v", id, err)
	}
	nodes, err := svc.Tree(context.Background(), item.ProjectID, item.Kind)
	if err != nil {
		t.Fatalf("读取导航树失败: %v", err)
	}
	siblings := flatSiblings(nodes, item.ParentID)
	if len(siblings) < 2 {
		t.Fatalf("同级至少要有两项才能交换，实际 %d 项", len(siblings))
	}
	return siblings
}

// dirAwayFromEdgeSvc 取一个**不会**撞到边界的移动方向。
//
// 同级顺序由 (sort_order, id) 决定，而 id 是随机 UUID —— 写死 "down" 会有一半概率落在末项上
// （那时调用走的是边界档 Moved=false），用例就成了掷骰子。方向必须先按实际次序算出来。
func dirAwayFromEdgeSvc(t *testing.T, svc navigationcontract.NavigationService, id string) string {
	t.Helper()
	siblings := siblingsOfSvc(t, svc, id)
	for i, n := range siblings {
		if n.ID == id {
			if i+1 < len(siblings) {
				return "down"
			}
			return "up"
		}
	}
	t.Fatalf("同级里找不到 %s", id)
	return ""
}

// edgeSiblingIDSvc 取同级首项（dir=up）或末项（dir=down），该方向的移动必然越界。
func edgeSiblingIDSvc(t *testing.T, svc navigationcontract.NavigationService, id, dir string) string {
	t.Helper()
	siblings := siblingsOfSvc(t, svc, id)
	if dir == "up" {
		return siblings[0].ID
	}
	return siblings[len(siblings)-1].ID
}

func dirAwayFromEdge(t *testing.T, env *navSwapEnv, id string) string {
	t.Helper()
	return dirAwayFromEdgeSvc(t, env.Nav, id)
}

func edgeSiblingID(t *testing.T, env *navSwapEnv, id, dir string) string {
	t.Helper()
	return edgeSiblingIDSvc(t, env.Nav, id, dir)
}

// getNavItem 取一项的归属信息（工程 / 位置）。
func getNavItem(t *testing.T, env *navSwapEnv, id string) *navigationdto.NavigationResp {
	t.Helper()
	item, err := env.Nav.Get(context.Background(), &navigationdto.GetReq{ID: id})
	if err != nil {
		t.Fatalf("读取导航项 %s 失败: %v", id, err)
	}
	return item
}

// moveReq 组一次交换请求（ProjectID 与页面表单同源）。
func moveReq(env *navSwapEnv, id, dir string) *navigationdto.SwapMoveReq {
	return &navigationdto.SwapMoveReq{ProjectID: env.ProjectID, ID: id, Dir: dir}
}

// TestNavigationSwapMoveSwapsNeighbourAndDispatchesOnce 正常路径：
// 两行取值互换、两行版本都刷新，且失效只派发一次（menu×1 + 两项×2；顶级无父项）。
func TestNavigationSwapMoveSwapsNeighbourAndDispatchesOnce(t *testing.T) {
	env := newNavSwapEnv(t)
	first := addNavItem(t, env.Nav, env.ProjectID, "首页", "/", nil, "self")
	second := addNavItem(t, env.Nav, env.ProjectID, "新品", "/new", nil, "self")
	beforeFirst := readNavSwapState(t, env.DB, first)
	beforeSecond := readNavSwapState(t, env.DB, second)
	base := dispatchBase(env)

	res, err := env.Nav.SwapMove(context.Background(), moveReq(env, first, dirAwayFromEdge(t, env, first)))
	if err != nil {
		t.Fatalf("下移应成功，实际: %v", err)
	}
	if res == nil || !res.Moved {
		t.Fatalf("应报告已交换，实际: %+v", res)
	}
	afterFirst := readNavSwapState(t, env.DB, first)
	afterSecond := readNavSwapState(t, env.DB, second)
	if afterFirst.sort != beforeSecond.sort || afterSecond.sort != beforeFirst.sort {
		t.Fatalf("两行排序值应互换：first %d→%d（期望 %d）、second %d→%d（期望 %d）",
			beforeFirst.sort, afterFirst.sort, beforeSecond.sort,
			beforeSecond.sort, afterSecond.sort, beforeFirst.sort)
	}
	if !afterFirst.updated.After(beforeFirst.updated) || !afterSecond.updated.After(beforeSecond.updated) {
		t.Fatalf("两行版本都应刷新：first %v→%v、second %v→%v",
			beforeFirst.updated, afterFirst.updated, beforeSecond.updated, afterSecond.updated)
	}
	assertDispatchDelta(t, env, base, 1, 2, "交换成功")
}

// TestNavigationSwapMoveBoundaryWritesNothing 首/末位：报告未位移、零写、零派发。
func TestNavigationSwapMoveBoundaryWritesNothing(t *testing.T) {
	env := newNavSwapEnv(t)
	first := addNavItem(t, env.Nav, env.ProjectID, "首页", "/", nil, "self")
	second := addNavItem(t, env.Nav, env.ProjectID, "新品", "/new", nil, "self")
	baselineFirst := readNavSwapState(t, env.DB, first)
	baselineSecond := readNavSwapState(t, env.DB, second)
	base := dispatchBase(env)

	// 取同级首项（顺序由 (sort_order, id) 决定，不是「创建顺序」），上移必然越界。
	edge := edgeSiblingID(t, env, first, "up")
	res, err := env.Nav.SwapMove(context.Background(), moveReq(env, edge, "up"))
	if err != nil {
		t.Fatalf("边界项应零写返回，实际报错: %v", err)
	}
	if res == nil || res.Moved || res.Reason != "boundary" {
		t.Fatalf("边界项应返回 Moved=false/Reason=boundary，实际: %+v", res)
	}
	if got := readNavSwapState(t, env.DB, first); got != baselineFirst {
		t.Fatalf("边界项不得写入：%+v → %+v", baselineFirst, got)
	}
	if got := readNavSwapState(t, env.DB, second); got != baselineSecond {
		t.Fatalf("邻居不得被写入：%+v → %+v", baselineSecond, got)
	}
	assertDispatchDelta(t, env, base, 0, 0, "边界零写")
}

// TestNavigationSwapMoveTiedIsRefusedWithoutWrite 同级排序值并列：
// 交换取值等于没换（并列由 id ASC 决胜），必须回业务文案 ErrSortTied、零写零派发。
func TestNavigationSwapMoveTiedIsRefusedWithoutWrite(t *testing.T) {
	env := newNavSwapEnv(t)
	first := addNavItem(t, env.Nav, env.ProjectID, "首页", "/", nil, "self")
	second := addNavItem(t, env.Nav, env.ProjectID, "新品", "/new", nil, "self")
	// 造并列：外部脚本按 /api/navigation/update 裸置位就是这种形态。
	if err := env.DB.Exec("UPDATE navigations SET sort_order = 7 WHERE id IN (?, ?)", first, second).Error; err != nil {
		t.Fatalf("造并列排序值失败: %v", err)
	}
	baselineFirst := readNavSwapState(t, env.DB, first)
	baselineSecond := readNavSwapState(t, env.DB, second)
	base := dispatchBase(env)

	_, err := env.Nav.SwapMove(context.Background(), moveReq(env, first, dirAwayFromEdge(t, env, first)))
	if err == nil {
		t.Fatal("并列时应拒绝，不能回「已完成」")
	}
	if !strings.HasPrefix(err.Error(), navigationenums.ErrSortTied) {
		t.Fatalf("并列应回 %s，实际: %v", navigationenums.ErrSortTied, err)
	}
	if got := readNavSwapState(t, env.DB, first); got != baselineFirst {
		t.Fatalf("并列不得写入：%+v → %+v", baselineFirst, got)
	}
	if got := readNavSwapState(t, env.DB, second); got != baselineSecond {
		t.Fatalf("并列不得写入：%+v → %+v", baselineSecond, got)
	}
	assertDispatchDelta(t, env, base, 0, 0, "并列拒绝")
}

// TestNavigationSwapMoveRejectsForeignProject 工程作用域比对：调用方给的工程不是该项归属即拒。
func TestNavigationSwapMoveRejectsForeignProject(t *testing.T) {
	env := newNavSwapEnv(t)
	first := addNavItem(t, env.Nav, env.ProjectID, "首页", "/", nil, "self")
	addNavItem(t, env.Nav, env.ProjectID, "新品", "/new", nil, "self")
	baselineFirst := readNavSwapState(t, env.DB, first)
	base := dispatchBase(env)

	_, err := env.Nav.SwapMove(context.Background(), &navigationdto.SwapMoveReq{
		ProjectID: "00000000-0000-0000-0000-000000000000", ID: first, Dir: "down",
	})
	if err == nil {
		t.Fatal("跨工程请求应被拒")
	}
	if !strings.HasPrefix(err.Error(), navigationenums.ErrInvalidParam) {
		t.Fatalf("跨工程应回 %s，实际: %v", navigationenums.ErrInvalidParam, err)
	}
	if got := readNavSwapState(t, env.DB, first); got != baselineFirst {
		t.Fatalf("被拒时不得写入：%+v → %+v", baselineFirst, got)
	}
	assertDispatchDelta(t, env, base, 0, 0, "跨工程被拒")
}

// TestNavigationSwapMoveNestedTouchesOnlyThatLevel 嵌套：
// 交换某一级的子项只动该级，另一个父级（以及父项自身）逐字段不变。
func TestNavigationSwapMoveNestedTouchesOnlyThatLevel(t *testing.T) {
	env := newNavSwapEnv(t)
	parentA := addNavItem(t, env.Nav, env.ProjectID, "产品", "/products", nil, "self")
	childA1 := addNavItem(t, env.Nav, env.ProjectID, "全部产品", "/products/all", &parentA, "self")
	childA2 := addNavItem(t, env.Nav, env.ProjectID, "新品", "/products/new", &parentA, "self")
	parentB := addNavItem(t, env.Nav, env.ProjectID, "关于", "/about", nil, "self")
	childB1 := addNavItem(t, env.Nav, env.ProjectID, "联系方式", "/about/contact", &parentB, "self")

	untouched := map[string]navSwapRowState{
		parentA: readNavSwapState(t, env.DB, parentA),
		parentB: readNavSwapState(t, env.DB, parentB),
		childB1: readNavSwapState(t, env.DB, childB1),
	}
	beforeA1 := readNavSwapState(t, env.DB, childA1)
	beforeA2 := readNavSwapState(t, env.DB, childA2)
	base := dispatchBase(env)

	if _, err := env.Nav.SwapMove(context.Background(), moveReq(env, childA1, dirAwayFromEdge(t, env, childA1))); err != nil {
		t.Fatalf("同级子项交换应成功，实际: %v", err)
	}
	if got := readNavSwapState(t, env.DB, childA1); got.sort != beforeA2.sort {
		t.Fatalf("子项排序应互换：childA1 %d → %d（期望 %d）", beforeA1.sort, got.sort, beforeA2.sort)
	}
	if got := readNavSwapState(t, env.DB, childA2); got.sort != beforeA1.sort {
		t.Fatalf("子项排序应互换：childA2 %d → %d（期望 %d）", beforeA2.sort, got.sort, beforeA1.sort)
	}
	for id, want := range untouched {
		if got := readNavSwapState(t, env.DB, id); got != want {
			t.Fatalf("同级之外的行不得被触碰：%s %+v → %+v", id, want, got)
		}
	}
	// 有父项时：menu×1 + 父项 + 两项 = 1 + 3。
	assertDispatchDelta(t, env, base, 1, 3, "嵌套同级交换")
}

// TestNavigationSwapMoveRollsBackWhenSecondRowConflicts（Y 窗口 / S1）：
// 第二条的版本在「快照已读、第一条已写」之后被推走 → 整事务回滚、dispatcher 0 次。
func TestNavigationSwapMoveRollsBackWhenSecondRowConflicts(t *testing.T) {
	env := newNavSwapEnv(t)
	movedID := addNavItem(t, env.Nav, env.ProjectID, "首页", "/", nil, "self")
	neighbourID := addNavItem(t, env.Nav, env.ProjectID, "新品", "/new", nil, "self")
	dir := dirAwayFromEdge(t, env, movedID)
	movedPair, neighbourPair := swapPairIDs(t, env, movedID, dir)
	if movedPair != movedID || neighbourPair != neighbourID {
		t.Fatalf("用例前提：这一对就是 (首页, 新品)，实际 (%s, %s)", movedPair, neighbourPair)
	}
	firstID, secondID := sortedIDs(movedID, neighbourID)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	holder := newNavSwapHolder(t, env)
	holder.pid = holder.backendPID(t)
	holder.begin(t)
	holder.lockRow(t, firstID) // 只锁会**先**被写的那一行（model 按 id 升序）

	base := dispatchBase(env)
	done := make(chan error, 1)
	go func() {
		_, err := env.Nav.SwapMove(ctx, moveReq(env, movedID, dir))
		done <- err
	}()
	waitForSwapGoroutineToFinish(t, holder, done)

	waitForSwapBlockedByHolder(t, env, holder.pid)
	// 基线 = 外部干预**提交之后**的状态。
	bumped := holder.bumpRowVersion(t, secondID)
	baselineFirst := readNavSwapState(t, env.DB, firstID)

	err := <-done
	if err == nil {
		t.Fatal("第二条版本已变时应失败")
	}
	if !strings.HasPrefix(err.Error(), navigationenums.ErrStaleVersion) {
		t.Fatalf("应回乐观锁冲突文案 %s，实际: %v", navigationenums.ErrStaleVersion, err)
	}
	afterFirst := readNavSwapState(t, env.DB, firstID)
	afterSecond := readNavSwapState(t, env.DB, secondID)
	if afterFirst != baselineFirst {
		t.Fatalf("第一条必须整事务回滚：%+v → %+v", baselineFirst, afterFirst)
	}
	if afterSecond != bumped {
		t.Fatalf("第二条应保持外部干预后的状态：%+v → %+v", bumped, afterSecond)
	}
	if afterFirst.sort == afterSecond.sort {
		t.Fatalf("同级不得留下重复排序值：first=%d second=%d", afterFirst.sort, afterSecond.sort)
	}
	assertDispatchDelta(t, env, base, 0, 0, "第二条冲突整事务回滚")
}

// TestNavigationSwapMoveWritesFirstRowThenWaitsForSecondLock（S2）：
// 反向对照 —— holder 锁住**后写的那一行**时，swap 已经写完了先写的那一行（探测 X 拿 55P03），
// holder 释放后整笔提交成功并派发。这条同时是「按 id 升序写」这条不变量在真实库上的观测。
func TestNavigationSwapMoveWritesFirstRowThenWaitsForSecondLock(t *testing.T) {
	env := newNavSwapEnv(t)
	movedID := addNavItem(t, env.Nav, env.ProjectID, "首页", "/", nil, "self")
	neighbourID := addNavItem(t, env.Nav, env.ProjectID, "新品", "/new", nil, "self")
	dir := dirAwayFromEdge(t, env, movedID)
	firstID, secondID := sortedIDs(movedID, neighbourID)
	beforeFirst := readNavSwapState(t, env.DB, firstID)
	beforeSecond := readNavSwapState(t, env.DB, secondID)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	holder := newNavSwapHolder(t, env)
	holder.pid = holder.backendPID(t)
	holder.begin(t)
	holder.lockRow(t, secondID) // 锁后写的那行：swap 会先写 firstID，然后卡在这里

	base := dispatchBase(env)
	done := make(chan error, 1)
	go func() {
		_, err := env.Nav.SwapMove(ctx, moveReq(env, movedID, dir))
		done <- err
	}()
	waitForSwapGoroutineToFinish(t, holder, done)

	waitForSwapBlockedByHolder(t, env, holder.pid)
	// 顺序证据：此刻 holder 只持 secondID，firstID 若被锁只可能来自 swap 的事务。
	holder.assertRowLockedBySwap(t, firstID)
	holder.rollback(t)

	if err := <-done; err != nil {
		t.Fatalf("holder 释放后交换应提交成功，实际: %v", err)
	}
	afterFirst := readNavSwapState(t, env.DB, firstID)
	afterSecond := readNavSwapState(t, env.DB, secondID)
	if afterFirst.sort != beforeSecond.sort || afterSecond.sort != beforeFirst.sort {
		t.Fatalf("终态应是交换后：first %+v、second %+v", afterFirst, afterSecond)
	}
	assertDispatchDelta(t, env, base, 1, 2, "holder 释放后提交成功")
}

// TestNavigationSwapMoveConcurrentStaysWhole（Z2）：两笔并发（无 barrier）只做软断言 ——
// 终态必须是「交换前」或「交换后」之一，绝不出现半交换；派发次数与成功笔数一致。
func TestNavigationSwapMoveConcurrentStaysWhole(t *testing.T) {
	env := newNavSwapEnv(t)
	first := addNavItem(t, env.Nav, env.ProjectID, "首页", "/", nil, "self")
	second := addNavItem(t, env.Nav, env.ProjectID, "新品", "/new", nil, "self")
	beforeFirst := readNavSwapState(t, env.DB, first)
	beforeSecond := readNavSwapState(t, env.DB, second)

	base := dispatchBase(env)
	dir := dirAwayFromEdge(t, env, first)
	var wg sync.WaitGroup
	results := make([]error, 2)
	movedFlags := make([]bool, 2)
	for i := range results {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			res, err := env.Nav.SwapMove(context.Background(), moveReq(env, first, dir))
			results[idx] = err
			movedFlags[idx] = res != nil && res.Moved
		}(i)
	}
	wg.Wait()

	moved := 0
	for i, err := range results {
		if err == nil {
			// 无错不等于真的换了位：第二笔可能读到交换后的树，于是「下移」已是末位，
			// 走的是边界档（Moved=false）——所以下面按 Moved 计数，不按 err==nil 计数。
			if movedFlags[i] {
				moved++
			}
			continue
		}
		if !strings.HasPrefix(err.Error(), navigationenums.ErrStaleVersion) {
			t.Fatalf("并发交换只允许「成功」或「乐观锁冲突」，实际: %v", err)
		}
	}
	afterFirst := readNavSwapState(t, env.DB, first)
	afterSecond := readNavSwapState(t, env.DB, second)
	swapped := afterFirst.sort == beforeSecond.sort && afterSecond.sort == beforeFirst.sort
	unchanged := afterFirst.sort == beforeFirst.sort && afterSecond.sort == beforeSecond.sort
	if !swapped && !unchanged {
		t.Fatalf("终态只能是交换前或交换后：first %+v→%+v、second %+v→%+v",
			beforeFirst, afterFirst, beforeSecond, afterSecond)
	}
	if (moved%2 == 1) != swapped {
		t.Fatalf("终态与真实换位笔数不一致：换位 %d 笔（errors=%v moved=%v）⇒ swapped=%v",
			moved, results, movedFlags, swapped)
	}
	assertDispatchDelta(t, env, base, moved, moved*2, "并发交换")
}

// TestNavigationUpdateStillUsesOptimisticLock 回归：普通单行更新（/api/navigation/update 的那条
// 服务端路径）语义不变 —— 带对 token 成功、带旧 token 拒绝。
func TestNavigationUpdateStillUsesOptimisticLock(t *testing.T) {
	env := newNavSwapEnv(t)
	first := addNavItem(t, env.Nav, env.ProjectID, "首页", "/", nil, "self")
	addNavItem(t, env.Nav, env.ProjectID, "新品", "/new", nil, "self")

	nodes, err := env.Nav.Tree(context.Background(), env.ProjectID, "header")
	if err != nil {
		t.Fatalf("读取导航树失败: %v", err)
	}
	token := tokenOf(nodes, first)
	if token == "" {
		t.Fatal("树节点上应带乐观锁 token（UpdatedAt）")
	}
	newOrder := 99
	if _, err := env.Nav.Update(context.Background(), &navigationdto.UpdateReq{
		ID: first, SortOrder: &newOrder, ExpectedUpdatedAt: &token,
	}); err != nil {
		t.Fatalf("带正确 token 的单行更新应成功，实际: %v", err)
	}
	if got := readNavSwapState(t, env.DB, first).sort; got != newOrder {
		t.Fatalf("单行更新应落库：期望 %d，实际 %d", newOrder, got)
	}
	// 同一个（现在已过期的）token 再来一次：必须拒绝，不能静默覆盖。
	if _, err := env.Nav.Update(context.Background(), &navigationdto.UpdateReq{
		ID: first, SortOrder: &newOrder, ExpectedUpdatedAt: &token,
	}); err == nil {
		t.Fatal("旧 token 的单行更新应被拒")
	} else if !strings.HasPrefix(err.Error(), navigationenums.ErrStaleVersion) {
		t.Fatalf("旧 token 应回 %s，实际: %v", navigationenums.ErrStaleVersion, err)
	}
}

// tokenOf 在树里取某项的乐观锁 token（递归）。
func tokenOf(nodes []*navigationdto.NavigationNode, id string) string {
	for _, n := range nodes {
		if n.ID == id {
			return n.UpdatedAt
		}
		if tok := tokenOf(n.Children, id); tok != "" {
			return tok
		}
	}
	return ""
}

// TestNavigationMoveRouteKeepsPermissionGuardAndWritesThroughChain：写入口的用户契约
// （Session → CSRF → Casbin(navigation:update) → handler → PG）。
//
// task-84 改了实现（两次 Update → 一次 SwapMove），task-91 又把它从页面 POST 搬到了
// `POST /api/navigation/update`（action=move），但**授权锚点一字未动**：这条用生产同构的
// 三层链证明无策略仍被 403 拦住、补上策略后真的能写穿到库里。
func TestNavigationMoveRouteKeepsPermissionGuardAndWritesThroughChain(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)
	// 该装配自己已经落了一条 "/" 的导航项，这里换独占路径，避免 (project,kind,path) 唯一索引冲突。
	first := addNavItem(t, env.NavSvc, env.ProjectID, "交换甲", "/swap-first", nil, "self")
	second := addNavItem(t, env.NavSvc, env.ProjectID, "交换乙", "/swap-second", nil, "self")
	beforeFirst := readNavSwapState(t, env.DB, first)
	beforeSecond := readNavSwapState(t, env.DB, second)
	payload := map[string]interface{}{
		"action":    "move",
		"projectId": env.ProjectID,
		"id":        first,
		"dir":       dirAwayFromEdgeSvc(t, env.NavSvc, first),
	}

	denied := navWriteAuthzPostLang(t, env, env.Plain, navTranslationWriteAPIPath, payload, "")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("无 navigation:update 策略的普通管理员应 403，实际 %d: %s", denied.Code, denied.Body.String())
	}
	if got := readNavSwapState(t, env.DB, first); got != beforeFirst {
		t.Fatalf("被拦下的请求不得写入：%+v → %+v", beforeFirst, got)
	}

	// 补上策略后同一条请求应写穿（权限锚点仍是 /api/navigation/update —— 迁移不改授权面）。
	navTranslationAuthzGrantPolicy(t, env)
	allowed := navWriteAuthzPostLang(t, env, env.Plain, navTranslationWriteAPIPath, payload, "")
	if allowed.Code != http.StatusOK {
		t.Fatalf("补上策略后应 200，实际 %d: %s", allowed.Code, allowed.Body.String())
	}
	if !strings.Contains(allowed.Body.String(), `"moved":true`) {
		t.Fatalf("回执应带 moved:true：%s", allowed.Body.String())
	}
	afterFirst := readNavSwapState(t, env.DB, first)
	afterSecond := readNavSwapState(t, env.DB, second)
	if afterFirst.sort != beforeSecond.sort || afterSecond.sort != beforeFirst.sort {
		t.Fatalf("走完整链路应完成交换：first %+v→%+v、second %+v→%+v",
			beforeFirst, afterFirst, beforeSecond, afterSecond)
	}
}

// TestNavigationMoveRouteBoundaryIsSuccessWithoutWrite 边界档走 HTTP 也是**成功**，且零写零派发。
//
// 迁移前页面侧把边界当成功（navJumpList 的「已完成」）；JSON 档必须同样是 200 ——
// 边界若被报成错误，前端会弹「操作失败」，而用户其实只是把首项又点了一次上移。
// 三条一起断言：状态 200 + moved:false、库里没动、stale 一次都没派发（早退在 service）。
// 先补策略再发请求：否则 403 同样满足「零写零派发」，用例会变成假绿。
func TestNavigationMoveRouteBoundaryIsSuccessWithoutWrite(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)
	first := addNavItem(t, env.NavSvc, env.ProjectID, "边界甲", "/edge-first", nil, "self")
	addNavItem(t, env.NavSvc, env.ProjectID, "边界乙", "/edge-second", nil, "self")

	dispatcher := &countingNavDispatcher{}
	navSwapAttachDispatcher(t, env.NavSvc, dispatcher)
	t.Cleanup(func() { navSwapAttachDispatcher(t, env.NavSvc, nil) })
	base := dispatchBase(&navSwapEnv{Dispatcher: dispatcher})

	// 同级首项 + dir=up 必然越界；同级次序由 (sort_order, id) 决定，故按实际次序取。
	edge := edgeSiblingIDSvc(t, env.NavSvc, first, "up")
	before := readNavSwapState(t, env.DB, edge)

	navTranslationAuthzGrantPolicy(t, env)
	rec := navWriteAuthzPostLang(t, env, env.Plain, navTranslationWriteAPIPath, map[string]interface{}{
		"action": "move", "projectId": env.ProjectID, "id": edge, "dir": "up",
	}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("边界移动应 200（不是错误），实际 %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"moved":false`) || !strings.Contains(rec.Body.String(), `"reason":"boundary"`) {
		t.Fatalf("边界回执应带 moved:false + reason=boundary：%s", rec.Body.String())
	}
	if got := readNavSwapState(t, env.DB, edge); got != before {
		t.Fatalf("边界移动不得写库：%+v → %+v", before, got)
	}
	assertDispatchDelta(t, &navSwapEnv{Dispatcher: dispatcher}, base, 0, 0, "边界移动")
}

// —— Y 窗口的 holder（独立连接 + 行锁 + 定点观测）——

// navSwapHolder 一个持有行锁的独立连接（占用池里的一条物理连接）。
type navSwapHolder struct {
	env  *navSwapEnv
	conn *sql.Conn
	pid  int
}

func newNavSwapHolder(t *testing.T, env *navSwapEnv) *navSwapHolder {
	t.Helper()
	sqlDB, err := env.DB.DB()
	if err != nil {
		t.Fatalf("取数据库句柄失败: %v", err)
	}
	// holder / 被阻塞的 swap / 监测 / 探测各占一条连接 —— 先把池子放大到够用。
	sqlDB.SetMaxOpenConns(6)
	conn, err := sqlDB.Conn(context.Background())
	if err != nil {
		t.Fatalf("取独立连接失败: %v", err)
	}
	holder := &navSwapHolder{env: env, conn: conn}
	t.Cleanup(func() {
		// 无条件收尾：先放锁（否则被阻塞的 swap 会一直等），再关连接。
		_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		_ = conn.Close()
	})
	return holder
}

// waitForSwapGoroutineToFinish 在用例收尾时先放锁、再等交换 goroutine 真的结束。
//
// 为什么必须排在 holder 自己的清理之前：cleanup 是 LIFO，本函数在 goroutine 启动之后才注册，
// 于是先于 holder 的「ROLLBACK + Close」执行。否则用例中途 Fatalf 退出时，交换还卡在 holder
// 的行锁上，而 holder 连接已经被关 —— 库里留下一个等锁到超时的后端，日志里出现与结论无关的
// 报错噪声，更糟的是它可能在本条测试结束之后才提交。
func waitForSwapGoroutineToFinish(t *testing.T, holder *navSwapHolder, done <-chan error) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = holder.conn.ExecContext(context.Background(), "ROLLBACK")
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
	})
}

func (h *navSwapHolder) backendPID(t *testing.T) int {
	t.Helper()
	var pid int
	if err := h.conn.QueryRowContext(context.Background(), "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		t.Fatalf("取 holder 后端 pid 失败: %v", err)
	}
	return pid
}

func (h *navSwapHolder) begin(t *testing.T) {
	t.Helper()
	if _, err := h.conn.ExecContext(context.Background(), "BEGIN"); err != nil {
		t.Fatalf("holder 开启事务失败: %v", err)
	}
}

// lockRow 只锁一行（WHERE 只写 id，不得圈到另一行）。
func (h *navSwapHolder) lockRow(t *testing.T, id string) {
	t.Helper()
	if _, err := h.conn.ExecContext(context.Background(),
		"SELECT id FROM navigations WHERE id = $1 FOR UPDATE", id); err != nil {
		t.Fatalf("holder 锁住 %s 失败: %v", id, err)
	}
}

// bumpRowVersion 在 holder 的**同一个事务**里推走另一行的版本并提交，返回其持久化状态。
func (h *navSwapHolder) bumpRowVersion(t *testing.T, id string) navSwapRowState {
	t.Helper()
	var state navSwapRowState
	if err := h.conn.QueryRowContext(context.Background(),
		"UPDATE navigations SET update_time = update_time + interval '1 second' WHERE id = $1 "+
			"RETURNING sort_order, update_time", id).Scan(&state.sort, &state.updated); err != nil {
		t.Fatalf("holder 推走 %s 的版本失败: %v", id, err)
	}
	if _, err := h.conn.ExecContext(context.Background(), "COMMIT"); err != nil {
		t.Fatalf("holder 提交失败: %v", err)
	}
	return state
}

func (h *navSwapHolder) rollback(t *testing.T) {
	t.Helper()
	if _, err := h.conn.ExecContext(context.Background(), "ROLLBACK"); err != nil {
		t.Fatalf("holder 回滚失败: %v", err)
	}
}

// assertRowLockedBySwap 顺序证据：holder 只持有另一行，因此这一行的行锁只可能来自 swap 的事务
// —— `FOR UPDATE NOWAIT` 拿到 55P03（lock_not_available）即证明「swap 先写了这一行」。
func (h *navSwapHolder) assertRowLockedBySwap(t *testing.T, id string) {
	t.Helper()
	sqlDB, err := h.env.DB.DB()
	if err != nil {
		t.Fatalf("取数据库句柄失败: %v", err)
	}
	conn, err := sqlDB.Conn(context.Background())
	if err != nil {
		t.Fatalf("取探测连接失败: %v", err)
	}
	defer conn.Close()
	var got string
	err = conn.QueryRowContext(context.Background(),
		"SELECT id FROM navigations WHERE id = $1 FOR UPDATE NOWAIT", id).Scan(&got)
	if err == nil {
		t.Fatalf("探测应被 swap 的行锁挡住（说明 swap 还没写第一行），实际取到了 %s", got)
	}
	if !strings.Contains(err.Error(), "55P03") {
		t.Fatalf("探测应报 SQLSTATE 55P03（lock_not_available），实际: %v", err)
	}
}

// waitForSwapBlockedByHolder 定点等待：找到被 holder 阻塞的那个 UPDATE 后端。
//
// 判据不能是「库里任意一个 Lock 等待」（并行用例、其它连接都可能出现），
// 必须同时满足：本测试库、正在等锁、且 pg_blocking_pids(pid) 里含 holder 的 pid。
// 超时即**失败**（不是跳过）：观测不到阻塞说明设计/实现的前提不成立。
func waitForSwapBlockedByHolder(t *testing.T, env *navSwapEnv, holderPID int) {
	t.Helper()
	sqlDB, err := env.DB.DB()
	if err != nil {
		t.Fatalf("取数据库句柄失败: %v", err)
	}
	conn, err := sqlDB.Conn(context.Background())
	if err != nil {
		t.Fatalf("取监测连接失败: %v", err)
	}
	defer conn.Close()

	const budget = 5 * time.Second
	deadline := time.Now().Add(budget)
	query := `SELECT a.pid FROM pg_stat_activity a
		WHERE a.datname = current_database()
		  AND a.wait_event_type = 'Lock'
		  AND a.pid <> pg_backend_pid()
		  AND $1 = ANY(pg_blocking_pids(a.pid))
		LIMIT 1`
	for {
		var pid int
		err := conn.QueryRowContext(context.Background(), query, holderPID).Scan(&pid)
		if err == nil {
			return
		}
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("监测锁等待失败: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("等了 %s 也没观测到被 holder(%d) 阻塞的交换后端（附 pg_stat_activity 诊断）：\n%s",
				budget, holderPID, navSwapActivityDump(t, conn))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// navSwapActivityDump 观测失败时的诊断（超时要能自证，不能只回一句「没等到」）。
func navSwapActivityDump(t *testing.T, conn *sql.Conn) string {
	t.Helper()
	rows, err := conn.QueryContext(context.Background(),
		`SELECT pid, state, coalesce(wait_event_type, '-'), coalesce(wait_event, '-'), left(query, 120)
		 FROM pg_stat_activity WHERE datname = current_database() ORDER BY pid`)
	if err != nil {
		return fmt.Sprintf("(取 pg_stat_activity 失败: %v)", err)
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var pid int
		var state, waitType, wait, query string
		if err := rows.Scan(&pid, &state, &waitType, &wait, &query); err != nil {
			return fmt.Sprintf("(读 pg_stat_activity 失败: %v)", err)
		}
		fmt.Fprintf(&b, "pid=%d state=%s wait=%s/%s query=%s\n", pid, state, waitType, wait, query)
	}
	return b.String()
}
