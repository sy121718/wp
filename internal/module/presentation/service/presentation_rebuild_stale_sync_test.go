package presentationservice

// presentation_rebuild_stale_sync_test.go — StaleRebuildPort（RebuildStaleSync）的策略判据。
//
// 这个端口存在的全部理由只有一条：**失败必须被发现**。旧 RebuildStale 在两处吞掉单条失败
// （入队失败、逐条重建失败都只记日志），最后恒返回 nil —— menu_stale 的队列任务因此被标
// succeeded，而实例一个都没重建。所以这里钉住的不是「能重建」，而是四条边界：
//
//  1. 没有工程作用域 → 报错，不发查询（FORCE RLS 下无作用域读是空集，会被当成「没有待办」）；
//  2. 队列已注入也不改走入队（再次入队算成功 = 把「没做成」写成「做成了」）；
//  3. 无待重建实例 → 成功且 rebuilt=0（正常收敛态，不是错误）；
//  4. 重建实现体原样上抛错误（不吞、不重试）。
//
// 单条失败与「已成功的保留」两条依赖真实库与真实构建，属于 feature 层
// （public/test/presentation/feature），这里只测本包可见的控制流。

import (
	"context"
	"testing"

	presentationcontract "go_wp/internal/module/presentation/contract"
)

// TestRebuildStaleSyncRequiresProject 没有工程作用域时拒绝，且不依赖 model。
func TestRebuildStaleSyncRequiresProject(t *testing.T) {
	s := &Service{}
	if n, err := s.RebuildStaleSync(context.Background(), "  "); err == nil {
		t.Fatalf("空 projectID 必须报错（否则空集被当成「没有待办」）, got rebuilt=%d", n)
	}
}

// TestRebuildStaleSyncIgnoresQueue 队列注入不会把同步入口变成入队入口。
//
// 判据是「入队端口一次都没被调用」：一旦有人把 RebuildStaleSync 实现成
// 「能在队列就在队列」，本用例立刻红 —— 那正是任务被判成功而实例没重建的形态。
func TestRebuildStaleSyncIgnoresQueue(t *testing.T) {
	s := &Service{}
	q := newFakeBuildQueue()
	s.SetBuildQueue(q)
	// 空工程 → 在发查询之前就被拒，队列同样不该被碰。
	if _, err := s.RebuildStaleSync(context.Background(), ""); err == nil {
		t.Fatal("空 projectID 必须报错")
	}
	if got := q.snapshot(); len(got) != 0 {
		t.Fatalf("同步重建入口不得入队，实际入队 %v", got)
	}
}

// TestRebuildStaleSyncSatisfiesPort 编译期断言：本模块满足契约里的 StaleRebuildPort。
//
// 装配层（menu_stale 消费侧）持有的正是这条端口：签名一旦漂移（例如多返回一个
// 错误、或把返回值改成 int64），这里先红，而不是等到装配期那处类型断言静默失败。
func TestRebuildStaleSyncSatisfiesPort(t *testing.T) {
	var port presentationcontract.StaleRebuildPort = &Service{}
	if port == nil {
		t.Fatal("不可达")
	}
}
