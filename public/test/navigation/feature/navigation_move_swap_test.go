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
	navigationservice "go_wp/internal/module/navigation/service"
)

type navDispatchBase struct{ menu, nav int }

func dispatchBase(env *navSwapEnv) navDispatchBase {
	menu, nav := env.Counter.counts()
	return navDispatchBase{menu: menu, nav: nav}
}

func assertDispatchDelta(t *testing.T, env *navSwapEnv, base navDispatchBase, wantMenu, wantNav int, step string) {
	t.Helper()
	menu, nav := env.Counter.counts()
	if menu-base.menu != wantMenu || nav-base.nav != wantNav {
		t.Fatalf("%s：本步应派发 menu×%d + nav×%d，实际 menu×%d + nav×%d（累计 menu=%d nav=%d；建项等前置已派发 menu×%d nav×%d）",
			step, wantMenu, wantNav, menu-base.menu, nav-base.nav, menu, nav, base.menu, base.nav)
	}
}

type navSwapEnv struct {
	DB        *gorm.DB
	Nav       navigationcontract.NavigationService
	ProjectID string
	Counter   *navStaleCounter
}

func newNavSwapEnv(t *testing.T) *navSwapEnv {
	t.Helper()
	db, _, navSvc, projectID, ports := newNavigationEnvPorts(t)
	counter := &navStaleCounter{}
	navSwapAttachCounter(t, navSvc, ports, counter)
	return &navSwapEnv{DB: db, Nav: navSvc, ProjectID: projectID, Counter: counter}
}

func navSwapAttachCounter(t *testing.T, svc navigationcontract.NavigationService,
	ports navWritePortFixture, counter *navStaleCounter) {
	t.Helper()
	setter, ok := svc.(interface {
		SetWritePorts(navigationservice.NavigationWriteDeps)
	})
	if !ok {
		t.Fatalf("NavSvc 未暴露 SetWritePorts：端口注入点变了，本用例失去计数能力")
	}
	setter.SetWritePorts(navStaleCountDeps(ports, counter))
}

type navSwapRowState struct {
	sort    int
	updated time.Time
}

func readNavSwapState(t *testing.T, db *gorm.DB, id string) navSwapRowState {
	t.Helper()
	var state navSwapRowState
	if err := db.Raw("SELECT sort_order, update_time FROM navigations WHERE id = ?", id).
		Row().Scan(&state.sort, &state.updated); err != nil {
		t.Fatalf("读回导航项 %s 失败: %v", id, err)
	}
	return state
}

func sortedIDs(a, b string) (first, second string) {
	if strings.Compare(a, b) < 0 {
		return a, b
	}
	return b, a
}

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

func getNavItem(t *testing.T, env *navSwapEnv, id string) *navigationdto.NavigationResp {
	t.Helper()
	item, err := env.Nav.Get(context.Background(), &navigationdto.GetReq{ID: id})
	if err != nil {
		t.Fatalf("读取导航项 %s 失败: %v", id, err)
	}
	return item
}

func moveReq(env *navSwapEnv, id, dir string) *navigationdto.SwapMoveReq {
	return &navigationdto.SwapMoveReq{ProjectID: env.ProjectID, ID: id, Dir: dir}
}

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

func TestNavigationSwapMoveBoundaryWritesNothing(t *testing.T) {
	env := newNavSwapEnv(t)
	first := addNavItem(t, env.Nav, env.ProjectID, "首页", "/", nil, "self")
	second := addNavItem(t, env.Nav, env.ProjectID, "新品", "/new", nil, "self")
	baselineFirst := readNavSwapState(t, env.DB, first)
	baselineSecond := readNavSwapState(t, env.DB, second)
	base := dispatchBase(env)

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

func TestNavigationSwapMoveTiedIsRefusedWithoutWrite(t *testing.T) {
	env := newNavSwapEnv(t)
	first := addNavItem(t, env.Nav, env.ProjectID, "首页", "/", nil, "self")
	second := addNavItem(t, env.Nav, env.ProjectID, "新品", "/new", nil, "self")

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

	assertDispatchDelta(t, env, base, 1, 3, "嵌套同级交换")
}

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
	holder.lockRow(t, firstID)

	base := dispatchBase(env)
	done := make(chan error, 1)
	go func() {
		_, err := env.Nav.SwapMove(ctx, moveReq(env, movedID, dir))
		done <- err
	}()
	waitForSwapGoroutineToFinish(t, holder, done)

	waitForSwapBlockedByHolder(t, env, holder.pid)

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
	holder.lockRow(t, secondID)

	base := dispatchBase(env)
	done := make(chan error, 1)
	go func() {
		_, err := env.Nav.SwapMove(ctx, moveReq(env, movedID, dir))
		done <- err
	}()
	waitForSwapGoroutineToFinish(t, holder, done)

	waitForSwapBlockedByHolder(t, env, holder.pid)

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

	if _, err := env.Nav.Update(context.Background(), &navigationdto.UpdateReq{
		ID: first, SortOrder: &newOrder, ExpectedUpdatedAt: &token,
	}); err == nil {
		t.Fatal("旧 token 的单行更新应被拒")
	} else if !strings.HasPrefix(err.Error(), navigationenums.ErrStaleVersion) {
		t.Fatalf("旧 token 应回 %s，实际: %v", navigationenums.ErrStaleVersion, err)
	}
}

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

func TestNavigationMoveRouteKeepsPermissionGuardAndWritesThroughChain(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)

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

func TestNavigationMoveRouteBoundaryIsSuccessWithoutWrite(t *testing.T) {
	env := newNavTranslationAuthzEnv(t)
	first := addNavItem(t, env.NavSvc, env.ProjectID, "边界甲", "/edge-first", nil, "self")
	addNavItem(t, env.NavSvc, env.ProjectID, "边界乙", "/edge-second", nil, "self")

	counter := &navStaleCounter{}
	navSwapAttachCounter(t, env.NavSvc, env.Ports, counter)
	base := dispatchBase(&navSwapEnv{Counter: counter})

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
	assertDispatchDelta(t, &navSwapEnv{Counter: counter}, base, 0, 0, "边界移动")
}

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

	sqlDB.SetMaxOpenConns(6)
	conn, err := sqlDB.Conn(context.Background())
	if err != nil {
		t.Fatalf("取独立连接失败: %v", err)
	}
	holder := &navSwapHolder{env: env, conn: conn}
	t.Cleanup(func() {

		_, _ = conn.ExecContext(context.Background(), "ROLLBACK")
		_ = conn.Close()
	})
	return holder
}

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

func (h *navSwapHolder) lockRow(t *testing.T, id string) {
	t.Helper()
	if _, err := h.conn.ExecContext(context.Background(),
		"SELECT id FROM navigations WHERE id = $1 FOR UPDATE", id); err != nil {
		t.Fatalf("holder 锁住 %s 失败: %v", id, err)
	}
}

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
