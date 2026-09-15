package presentationservice

// presentation_stale_test.go — PERF-020 单测：失效重建入队路径（纯逻辑，不碰数据库）。
//
// 并发场景在这里覆盖到「多 goroutine 同时触发 RebuildStale」：入队端口由
// fake 模拟队列侧的部分唯一索引去重，-race 下验证入队路径无数据竞争、
// 同一实例不会被堆出多份任务。

import (
	"context"
	"sync"
	"testing"
)

// fakeBuildQueue 模拟构建队列的入队端口：记录请求 + 按 id 去重（模拟部分唯一索引）。
type fakeBuildQueue struct {
	mu       sync.Mutex
	enqueued []string
	seen     map[string]bool
}

func newFakeBuildQueue() *fakeBuildQueue {
	return &fakeBuildQueue{seen: map[string]bool{}}
}

func (f *fakeBuildQueue) EnqueuePresentationBuild(_ context.Context, presentationID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	// 部分唯一索引语义：同一实例同时只有一条待办，重复入队是幂等的。
	if f.seen[presentationID] {
		return nil
	}
	f.seen[presentationID] = true
	f.enqueued = append(f.enqueued, presentationID)
	return nil
}

func (f *fakeBuildQueue) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.enqueued...)
}

// TestRebuildStaleEnqueuesAllWhenQueueInjected 队列注入后 RebuildStale 全量入队，
// 不做单次上限截断，也不再同步重建（不触碰 model 层）。
func TestRebuildStaleEnqueuesAllWhenQueueInjected(t *testing.T) {
	s := &Service{}
	q := newFakeBuildQueue()
	s.SetBuildQueue(q)

	ids := []string{"inst-1", "inst-2", "inst-3"}
	if err := s.RebuildStale(context.Background(), ids); err != nil {
		t.Fatalf("RebuildStale 入队路径报错: %v", err)
	}
	got := q.snapshot()
	if len(got) != len(ids) {
		t.Fatalf("入队条数 = %d, 期望 %d（%v）", len(got), len(ids), got)
	}
	for i, id := range ids {
		if got[i] != id {
			t.Fatalf("入队顺序[%d] = %s, 期望 %s", i, got[i], id)
		}
	}
}

// TestRebuildStaleConcurrentEnqueueStaysUnique 并发场景：多个 goroutine 同时触发
// RebuildStale 同一批实例，队列侧唯一去重保证每个实例只落一条任务。
func TestRebuildStaleConcurrentEnqueueStaysUnique(t *testing.T) {
	s := &Service{}
	q := newFakeBuildQueue()
	s.SetBuildQueue(q)

	ids := []string{"inst-a", "inst-b", "inst-c"}
	const workers = 8
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.RebuildStale(context.Background(), ids); err != nil {
				t.Errorf("并发 RebuildStale 报错: %v", err)
			}
		}()
	}
	wg.Wait()

	got := q.snapshot()
	if len(got) != len(ids) {
		t.Fatalf("并发触发后任务条数 = %d, 期望去重后的 %d（%v）", len(got), len(ids), got)
	}
}

// TestRebuildStaleEmptyIDsIsNoop 空列表直接返回，不触碰队列。
func TestRebuildStaleEmptyIDsIsNoop(t *testing.T) {
	s := &Service{}
	q := newFakeBuildQueue()
	s.SetBuildQueue(q)
	if err := s.RebuildStale(context.Background(), nil); err != nil {
		t.Fatalf("空列表报错: %v", err)
	}
	if n := len(q.snapshot()); n != 0 {
		t.Fatalf("空列表不该入队, 实际 %d 条", n)
	}
}

// TestSetBuildQueueNilReceiverSafe SetBuildQueue 对 nil 接收者安全（与其它 setter 同口径）。
func TestSetBuildQueueNilReceiverSafe(t *testing.T) {
	var s *Service
	s.SetBuildQueue(newFakeBuildQueue())
}
