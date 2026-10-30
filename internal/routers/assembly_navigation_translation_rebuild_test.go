package routers

// assembly_navigation_translation_rebuild_test.go — menu_stale 执行器的分块严格重建（纯逻辑，无库）。
//
// 这里钉住三件**行为**而不是「调了几次」：
//  1. 100 条待重建页面按页面模块公开的批容量分块，每块都在容量内（不触发 page 的
//     overflow 入队 + ErrRebuildOverloaded —— 那条路径在本执行器里没有下一轮触发源）；
//  2. 任一块返回错误立即整体返回，**后续块一次都不调用**；
//  3. 块全部成功才进入实例同步；页面侧失败时实例侧一次都不调用（不能拿「实例重建了」
//     掩盖「页面没重建完」）。
//
// 为什么用探针而不是真实 page 服务：本文件要断言的是**编排**（分块边界、失败短路、
// 阶段顺序），真实服务会让失败注入变成改库比赛；真实链路的收敛另有 feature 用例。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	buildcontract "go_wp/internal/module/build/contract"
	builddto "go_wp/internal/module/build/dto"
	pagecontract "go_wp/internal/module/page/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
)

// menuStaleRebuildSpy 记录每次 RebuildStaleStrict 收到的 id 批次，并可按批次注入失败。
type menuStaleRebuildSpy struct {
	batches  [][]string
	failOn   int // 1-based 批次序号；0 = 全部成功
	failWith error
	// refs 是位置键反查（FindPagesByDependency）返回的「直接受影响面」；
	// staleIDs 是 ListStalePageIDs 返回的「库里已承认待重建」。两个集合由执行器并集去重。
	refs     []pagecontract.DependencyPageRef
	staleIDs []string
}

func (s *menuStaleRebuildSpy) RebuildStaleStrict(_ context.Context, ids []string) error {
	s.batches = append(s.batches, append([]string(nil), ids...))
	if s.failOn > 0 && len(s.batches) == s.failOn {
		if s.failWith != nil {
			return s.failWith
		}
		return errors.New("注入的重建失败")
	}
	return nil
}

// 页面只读反查：本文件把它当作「本次改动的直接受影响面」返回固定集合。
func (s *menuStaleRebuildSpy) FindPagesByDependency(_ context.Context, _, _, _ string) ([]pagecontract.DependencyPageRef, error) {
	return s.refs, nil
}

func (s *menuStaleRebuildSpy) ListStalePageIDs(context.Context, string) ([]string, error) {
	return append([]string(nil), s.staleIDs...), nil
}

// menuStaleInstanceSpy 记录实例侧是否被调用（顺序判据用）。
type menuStaleInstanceSpy struct {
	calls int
}

func (s *menuStaleInstanceSpy) RebuildStaleSync(context.Context, string) (int, error) {
	s.calls++
	return 0, nil
}

// menuStaleTestProjectID 是真实形状的工程 id（pages / build_jobs 的 project_id 都是 uuid 列）。
//
// source_id 由 MenuStaleSourceID 生成，它对第一段做 uuid.Parse —— 传 "p1" 这种伪 id
// 会直接 ok=false。这里不绕过那条校验（那是生产契约），而是给一个合法 uuid：
// 本文件是纯编排用例，执行器不读库，工程 id 只承担「形状真实」这件事。
const menuStaleTestProjectID = "11111111-1111-4111-8111-111111111111"

func menuStaleTestJob(projectID, kind string) *builddto.Job {
	sourceID, ok := buildcontract.MenuStaleSourceID(projectID, kind)
	if !ok {
		panic("测试用例给了非 uuid 的工程 id 或空位置：" + projectID + " / " + kind)
	}
	return &builddto.Job{ID: "1", ProjectID: projectID, SourceID: sourceID}
}

func pageIDs(prefix string, n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("%s-%03d", prefix, i))
	}
	return out
}

// TestMenuStaleRebuildChunksBoundedByContract 100 条 → 按 pagecontract.RebuildBatchCapacity 分块。
//
// 断言的是「每块都在容量内」与「恰好覆盖全部 id 一次」，不是「块数等于某个数」：
// 块数由契约常量推导，容量调整时用例自动跟上，不需要同时改两个地方。
func TestMenuStaleRebuildChunksBoundedByContract(t *testing.T) {
	capacity := pagecontract.RebuildBatchCapacity
	if capacity <= 0 {
		t.Fatalf("页面模块公开的批容量必须是正数，实际 %d", capacity)
	}
	stale := pageIDs("page", 100)
	spy := &menuStaleRebuildSpy{staleIDs: stale}
	instances := &menuStaleInstanceSpy{}
	err := executeMenuStaleJob(context.Background(), menuStaleTestJob(menuStaleTestProjectID, "header"), spy, spy, instances)
	if err != nil {
		t.Fatalf("全部成功时不应返回错误: %v", err)
	}
	if len(spy.batches) == 0 {
		t.Fatal("100 条待重建页面必须分块调用，实际一次都没调用")
	}
	seen := map[string]int{}
	for i, batch := range spy.batches {
		if len(batch) > capacity {
			t.Fatalf("第 %d 块 %d 条超过页面批容量 %d：会触发 page 的 overflow 入队与 ErrRebuildOverloaded",
				i+1, len(batch), capacity)
		}
		if len(batch) == 0 {
			t.Fatalf("第 %d 块为空：分块边界算错", i+1)
		}
		for _, id := range batch {
			seen[id]++
		}
	}
	for _, id := range stale {
		if seen[id] != 1 {
			t.Fatalf("页面 %s 应恰好被纳入一次，实际 %d 次", id, seen[id])
		}
	}
	if want := (len(stale) + capacity - 1) / capacity; len(spy.batches) != want {
		t.Fatalf("100 条应按容量 %d 分成 %d 块，实际 %d 块", capacity, want, len(spy.batches))
	}
	if instances.calls != 1 {
		t.Fatalf("页面全部成功后必须且只能同步实例一次，实际 %d 次", instances.calls)
	}
}

// TestMenuStaleRebuildSecondChunkFailureSkipsRestAndInstances 第二块失败 → 立即返回，后续块与实例都不执行。
//
// 这是「不把部分成功当收敛」的核心判据：分块不是为了「尽量多做」，而是为了让失败
// 边界精确到块；一旦某块失败，剩下的块必须留着 stale（可观测），实例侧也不能跑。
func TestMenuStaleRebuildSecondChunkFailureSkipsRestAndInstances(t *testing.T) {
	capacity := pagecontract.RebuildBatchCapacity
	stale := pageIDs("page", capacity*3) // 恰好三块：第二块失败时还有第三块没跑
	boom := errors.New("注入：第二块重建失败")
	spy := &menuStaleRebuildSpy{staleIDs: stale, failOn: 2, failWith: boom}
	instances := &menuStaleInstanceSpy{}
	err := executeMenuStaleJob(context.Background(), menuStaleTestJob(menuStaleTestProjectID, "header"), spy, spy, instances)
	if err == nil {
		t.Fatal("第二块失败必须整体返回错误，不能当成成功")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("错误必须携带底层失败事实（可定位），实际: %v", err)
	}
	// 包装里要能看出是哪一块失败的（「第 X-Y 页（共 N）」）；文案逐字不钉，只钉定位信息存在。
	if !strings.Contains(err.Error(), "页") {
		t.Fatalf("错误应带块范围（第 X-Y 页 / 共 N）便于定位，实际: %v", err)
	}
	if len(spy.batches) != 2 {
		t.Fatalf("第二块失败后不得再调用后续块，实际调用了 %d 块", len(spy.batches))
	}
	if instances.calls != 0 {
		t.Fatalf("页面侧失败时实例侧必须一次都不跑，实际 %d 次", instances.calls)
	}
}

// TestMenuStaleRebuildDedupesBeforeChunking 位置键与 stale 集合并集去重后再分块。
//
// 不去重会让同一页在两个集合里各出现一次 → 多一次重建（幂等但白做），更糟的是
// 去重前分块会让「块容量」被重复 id 占满，真正的内容被挤到后面的块里。
func TestMenuStaleRebuildDedupesBeforeChunking(t *testing.T) {
	shared := "page-shared"
	spy := &menuStaleRebuildSpy{
		staleIDs: []string{shared, "page-only-stale"},
		refs:     []pagecontract.DependencyPageRef{{ID: shared}, {ID: "page-only-ref"}},
	}
	instances := &menuStaleInstanceSpy{}
	if err := executeMenuStaleJob(context.Background(), menuStaleTestJob(menuStaleTestProjectID, "header"), spy, spy, instances); err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	seen := map[string]int{}
	for _, batch := range spy.batches {
		for _, id := range batch {
			seen[id]++
		}
	}
	if seen[shared] != 1 {
		t.Fatalf("两个集合并集里的公共页面 %s 必须只重建一次，实际 %d 次", shared, seen[shared])
	}
	for _, id := range []string{"page-only-stale", "page-only-ref"} {
		if seen[id] != 1 {
			t.Fatalf("并集里的页面 %s 应恰好重建一次，实际 %d 次", id, seen[id])
		}
	}
}

var (
	_ menuStalePageRebuilder                = (*menuStaleRebuildSpy)(nil)
	_ pagecontract.PageDependencyLookup     = (*menuStaleRebuildSpy)(nil)
	_ presentationcontract.StaleRebuildPort = (*menuStaleInstanceSpy)(nil)
)
