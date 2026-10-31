package routers

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

type menuStaleRebuildSpy struct {
	batches  [][]string
	failOn   int
	failWith error

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

func (s *menuStaleRebuildSpy) FindPagesByDependency(_ context.Context, _, _, _ string) ([]pagecontract.DependencyPageRef, error) {
	return s.refs, nil
}

func (s *menuStaleRebuildSpy) ListStalePageIDs(context.Context, string) ([]string, error) {
	return append([]string(nil), s.staleIDs...), nil
}

type menuStaleInstanceSpy struct {
	calls int
}

func (s *menuStaleInstanceSpy) RebuildStaleSync(context.Context, string) (int, error) {
	s.calls++
	return 0, nil
}

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

func TestMenuStaleRebuildSecondChunkFailureSkipsRestAndInstances(t *testing.T) {
	capacity := pagecontract.RebuildBatchCapacity
	stale := pageIDs("page", capacity*3)
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
