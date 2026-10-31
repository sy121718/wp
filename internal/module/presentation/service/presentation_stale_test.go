package presentationservice

import (
	"context"
	"sync"
	"testing"
)

type fakeBuildQueue struct {
	mu       sync.Mutex
	enqueued []string
	seen     map[string]bool
}

func newFakeBuildQueue() *fakeBuildQueue {
	return &fakeBuildQueue{seen: map[string]bool{}}
}

func (f *fakeBuildQueue) EnqueuePresentationBuild(_ context.Context, presentationID, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

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

func TestSetBuildQueueNilReceiverSafe(t *testing.T) {
	var s *Service
	s.SetBuildQueue(newFakeBuildQueue())
}
