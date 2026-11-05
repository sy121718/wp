package unit

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go_wp/pkg/builder/core"
	contentdto "go_wp/internal/module/content/dto"
	presentationdto "go_wp/internal/module/presentation/dto"
)

var errCompileNotOverlapped = errors.New(
	"编译阶段超时：同一实例的第二个发布会话没有走进编译（实例锁仍覆盖编译）")

type compileRendezvous struct {
	mu      sync.Mutex
	waiting int
	release chan struct{}
	met     bool
	failed  error
	timeout time.Duration
	armed   atomic.Bool
}

func newCompileRendezvous(timeout time.Duration) *compileRendezvous {
	return &compileRendezvous{release: make(chan struct{}), timeout: timeout}
}

func (r *compileRendezvous) arm() { r.armed.Store(true) }

func (r *compileRendezvous) overlapped() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.met
}

func (r *compileRendezvous) meet() error {
	if !r.armed.Load() {
		return nil
	}
	r.mu.Lock()
	if r.failed != nil {
		err := r.failed
		r.mu.Unlock()
		return err
	}
	if r.met {
		r.mu.Unlock()
		return nil
	}
	r.waiting++
	release := r.release
	if r.waiting >= 2 {
		r.met = true
		r.waiting = 0
		close(release)
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()

	select {
	case <-release:
		return nil
	case <-time.After(r.timeout):
		r.mu.Lock()
		if !r.met {
			r.failed = errCompileNotOverlapped
		}
		r.mu.Unlock()
		return errCompileNotOverlapped
	}
}

type rendezvousRegistry struct {
	core.EntitySourceRegistry
	rendezvous *compileRendezvous
}

func (r *rendezvousRegistry) ResolverFor(ctx context.Context, entityType, entityID string) (core.ContentResolver, error) {
	if err := r.rendezvous.meet(); err != nil {
		return nil, err
	}
	return r.EntitySourceRegistry.ResolverFor(ctx, entityType, entityID)
}

func TestPresentationPublishCompileRunsOutsideInstanceLock(t *testing.T) {
	rendezvous := newCompileRendezvous(5 * time.Second)
	f := newPresFixtureWithRegistry(t, func(inner core.EntitySourceRegistry) core.EntitySourceRegistry {
		return &rendezvousRegistry{EntitySourceRegistry: inner, rendezvous: rendezvous}
	})
	if f == nil {
		return
	}
	ctx := context.Background()

	enableLangs(t, f, "zh-CN", "en-US", "ja-JP")
	f.createTemplate(t)

	entity, err := f.content.Create(ctx, &contentdto.CreateReq{
		EntityType: "article", Slug: "perf01-lock-scope",
		Data: map[string]any{"title": "锁覆盖范围", "excerpt": "摘要"},
	})
	if err != nil {
		t.Fatalf("创建实体失败: %v", err)
	}
	inst, err := f.pres.CreateInstance(ctx, &presentationdto.CreateInstanceReq{
		ProjectID: f.projectID, EntityType: "article", EntityID: entity.ID,
		URLPath: "/products/perf01-lock-scope",
	})
	if err != nil {
		t.Fatalf("CreateInstance 失败: %v", err)
	}

	rendezvous.arm()

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = f.pres.RebuildInstance(ctx, inst.ID)
		}(i)
	}
	wg.Wait()

	if !rendezvous.overlapped() {
		t.Fatalf("实例锁仍覆盖编译阶段：两个发布会话没有在编译期重叠，调用结果 %v", errs)
	}

	for i, rerr := range errs {
		if rerr != nil {
			t.Fatalf("并发重建第 %d 个失败（冲突必须重试收敛）: %v", i+1, rerr)
		}
	}

	batch := ledgerBatch(t, f, inst.ID)
	if len(batch) != 3 {
		t.Fatalf("并发重建后应有三种语言的账本行，实际 %+v", batch)
	}
	for _, row := range batch[1:] {
		if row.Version != batch[0].Version || row.SnapshotID != batch[0].SnapshotID {
			t.Fatalf("并发重建产生了混合版本：%+v", batch)
		}
	}
	assertLedgerMatchesAccessSurface(t, f, inst.ID)
	if active, stale := instancePointer(t, f, inst.ID); active == nil || stale {
		t.Fatalf("并发重建后实例应已发布且非 stale，实际 active=%v stale=%v", active, stale)
	}
}
