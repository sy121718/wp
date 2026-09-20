// presentation_publish_lock_scope_test.go — 编译阶段不再持实例锁（审计 PERF-01）。
//
// 缺陷原状：rebuildInstance 持有实例锁执行**整个** publishAllLangs —— 逐语言 Jet 渲染、
// 产物落盘、落库、激活、指针推进全在同一段临界区。同一实例的第二次发布（依赖失效触发的
// 自动重建、用户重建、改 URL、保存独立文档、快照回滚…）只能排队干等；任一种语言编译慢，
// 就把同实例的所有写路径一起拖住。而真正必须互斥的只有「版本分配 + 发布计划落库 +
// 访问面激活 + 指针推进」那几步。
//
// 本用例的判据不是耗时，而是**因果**：在编译阶段（renderHTML 取实体解析器那一步）放一个
// 双人会合闸门，两个发布会话必须同时到达才放行。
//
//	用例通过  = 两个会话的编译阶段真的重叠过（锁不再覆盖编译）；
//	用例失败  = 第二个会话进不到编译阶段（第一个会准时超时），错误信息直接说明这一点。
//
// 为什么不需要给生产代码加测试钩子：registry.ResolverFor 是每次语言编译的必经一步
// （renderHTML 里唯一取解析器的位置），且只在编译期调用 —— 包装注入给 service 的注册表
// 就能在同一条真实链路上安放探针（fixture 见 newPresFixtureWithRegistry）。
//
// 除「重叠」外还断言两条既有不变量，证明收窄覆盖面没有把正确性一起让出去：
// 并发重建两个调用都必须成功（冲突要重试收敛），结束后每个语言仍是单一 active 版本、
// 账本与访问面一致、实例指针已前进且非 stale。
package unit

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go_wp/internal/builder/core"
	contentdto "go_wp/internal/module/content/dto"
	presentationdto "go_wp/internal/module/presentation/dto"
	"go_wp/pkg/i18n"
)

// errCompileNotOverlapped 编译阶段没等来第二个发布会话 —— 实例锁仍覆盖编译。
var errCompileNotOverlapped = errors.New(
	"编译阶段超时：同一实例的第二个发布会话没有走进编译（实例锁仍覆盖编译）")

// compileRendezvous 编译阶段的双人会合闸门（PERF-01 用例专用）。
//
// 只要求会合**一次**：证明两个会话的编译段重叠就够了。此后的编译调用直接放行 ——
// 冲突重试会再走一遍编译，若继续要求配对，重试会平白被拖成超时。
//
// 失败是**粘性**的：一旦超时，后面所有到达都拿到同一个错误。否则第二个会话会
// 「自己跟自己」凑够两人，把用例伪装成通过（第一个早就放弃了）。
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

// arm 从此刻起要求编译阶段会合（fixture 装配与首发布之前不拦）。
func (r *compileRendezvous) arm() { r.armed.Store(true) }

// overlapped 是否真的发生过「两个会话同时在编译阶段」。
func (r *compileRendezvous) overlapped() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.met
}

// meet 到达编译阶段：等到第二个会话也到达（或本次会合已经完成过）。
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

// rendezvousRegistry 在编译阶段会合的实体类型注册表包装。
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

// TestPresentationPublishCompileRunsOutsideInstanceLock 同一实例的两个发布
// 会话必须能在编译阶段重叠（PERF-01 的验收判据）。
func TestPresentationPublishCompileRunsOutsideInstanceLock(t *testing.T) {
	rendezvous := newCompileRendezvous(5 * time.Second)
	f := newPresFixtureWithRegistry(t, func(inner core.EntitySourceRegistry) core.EntitySourceRegistry {
		return &rendezvousRegistry{EntitySourceRegistry: inner, rendezvous: rendezvous}
	})
	if f == nil {
		return
	}
	ctx := context.Background()
	i18n.SetSiteLangURLMode(i18n.SiteLangURLModeDefaultPlain)
	t.Cleanup(func() { i18n.SetSiteLangURLMode(i18n.SiteLangURLModeDefaultPlain) })
	// 三种语言：一次发布会话要在编译阶段被拦住三次，跨会话会合的机会足够。
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

	// 首发布自己也是一次发布会话：先让它跑完，再arm闸门，闸门只衡量并发的这两次。
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

	// 判据一（本票核心）：两个会话确实在编译阶段重叠过。
	// 锁仍覆盖编译时，第二个会话卡在实例锁上，第一个会在闸门超时后失败。
	if !rendezvous.overlapped() {
		t.Fatalf("实例锁仍覆盖编译阶段：两个发布会话没有在编译期重叠，调用结果 %v", errs)
	}
	// 判据二：并发重建两个调用都必须成功 —— 编译可以并行，提交必须互斥；
	// 提交时发现实例已被别的批次推进的那一次要重新冻结重试，而不是把请求判失败。
	for i, rerr := range errs {
		if rerr != nil {
			t.Fatalf("并发重建第 %d 个失败（冲突必须重试收敛）: %v", i+1, rerr)
		}
	}
	// 判据三：正确性不后退 —— 每个语言仍是单一 active 版本（不出现混合批次），
	// 账本与访问面一致，实例指针已前进且非 stale。
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
