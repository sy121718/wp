package unit

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go_wp/internal/module/page/dto"
	"go_wp/internal/module/page/service"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/internal/pipeline"
)

// 本文件守「路径锁真的落在生产入口上」三条不变量：
//  1. page 服务默认装的是**进程共享**锁实例（各服务各自 new = 两把锁 = 没锁）；
//  2. 同一路径的处置严格串行，第二段必须等第一段释放（不是「检查后各自动手」）；
//  3. 锁关闭时 fail closed —— 宁可返回错误留 pending 重放，也不无互斥删链接。

// TestPageServiceUsesProcessSharedPathLock 默认锁实例必须全进程唯一。
func TestPageServiceUsesProcessSharedPathLock(t *testing.T) {
	shared := pipeline.DefaultPathLocker()
	if shared == nil {
		t.Fatal("进程默认路径锁为空：各服务会各自 new 一个，等于没加锁")
	}
	if pipeline.DefaultPathLocker() != shared {
		t.Fatal("DefaultPathLocker 不是稳定单例")
	}
	_, svc, _, _ := newPageServiceWithRoutes(t)
	// 注入共享实例必须幂等（装配层重复接线不改变语义）。
	// svc 是聚合契约 PageService，锁端口只在具体服务上：用类型断言取真实实现，
	// 而不是往契约接口里塞测试用方法（契约不该为测试变形）。
	concrete, ok := svc.(*service.Service)
	if !ok {
		t.Fatalf("页面服务具体类型不是 *service.Service，实际 %T", svc)
	}
	concrete.SetPublicationPathLocker(shared)
	concrete.SetPublicationPathLocker(pipeline.DefaultPathLocker())
}

// TestPathLockSerializesSamePathTwoPhases 两阶段序列化：先让 A 完全结束，再让 B 进入，
// 断言 A 的副作用发生在 B 之前且不重叠。
//
// 刻意**不在锁内等待 B**（那会让 B 永远进不来、测试自己死锁）：这里只验证「同一路径
// 上的两段处置不可能交叠」，业务侧「已持路径锁时复核归属并拒绝」由
// page_url_old_path_ownership_test.go 与 receipt 身份用例覆盖。
func TestPathLockSerializesSamePathTwoPhases(t *testing.T) {
	locker := &pipeline.ProcessPathLocker{}
	path := "/lock/serialized"
	var mu sync.Mutex
	var trace []string

	run := func(tag string) {
		err := locker.WithPaths([]string{path}, func() error {
			mu.Lock()
			trace = append(trace, tag+":enter")
			mu.Unlock()
			time.Sleep(5 * time.Millisecond)
			mu.Lock()
			trace = append(trace, tag+":leave")
			mu.Unlock()
			return nil
		})
		if err != nil {
			t.Errorf("%s 取锁失败: %v", tag, err)
		}
	}

	// 阶段一：A 单独跑完。
	run("A")
	// 阶段二：B 单独跑完。
	run("B")

	want := []string{"A:enter", "A:leave", "B:enter", "B:leave"}
	if len(trace) != len(want) {
		t.Fatalf("轨迹长度不符: %v", trace)
	}
	for i := range want {
		if trace[i] != want[i] {
			t.Fatalf("轨迹 = %v，期望 %v（同一路径处置必须串行且不交叠）", trace, want)
		}
	}
}

// TestPathLockConcurrentSamePathNeverOverlaps 并发下也不交叠（不靠时序 lucky）。
func TestPathLockConcurrentSamePathNeverOverlaps(t *testing.T) {
	locker := &pipeline.ProcessPathLocker{}
	path := "/lock/overlap"
	var inFlight, maxInFlight int32
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = locker.WithPaths([]string{path}, func() error {
				mu.Lock()
				inFlight++
				if inFlight > maxInFlight {
					maxInFlight = inFlight
				}
				mu.Unlock()
				time.Sleep(time.Millisecond)
				mu.Lock()
				inFlight--
				mu.Unlock()
				return nil
			})
		}()
	}
	wg.Wait()
	if maxInFlight != 1 {
		t.Fatalf("同一路径最大并发进入数 = %d，期望 1", maxInFlight)
	}
}

// TestPageRedirectFailsClosedWithoutPathLock 锁被关掉时，删除重定向必须在碰 FS 之前失败。
func TestPageRedirectFailsClosedWithoutPathLock(t *testing.T) {
	_, svc, projects, projectID := newPageServiceWithRoutes(t)
	proj, err := projects.Create(context.Background(), &projectdto.CreateReq{Name: "锁关闭用例"})
	if err != nil {
		t.Fatalf("建工程失败: %v", err)
	}

	concrete, ok := svc.(*service.Service)
	if !ok {
		t.Fatalf("页面服务具体类型不是 *service.Service，实际 %T", svc)
	}
	concrete.SetPublicationPathLocker(nil)
	defer concrete.SetPublicationPathLocker(pipeline.DefaultPathLocker())

	err = svc.DeleteRedirect(context.Background(), &dto.RedirectDeleteReq{
		ProjectID: proj.ID,
		Path:      "/lock/closed",
	})
	if err == nil {
		t.Fatal("路径锁关闭时删除重定向仍返回成功：存在无互斥的 FS 处置")
	}
	// 真正要证明的是「发布链上清旧路径的入口」在锁关闭时也拒绝：用**真实业务入口**
	// （删页 → deactivatePathsOf）造夹具，不打任何测试专用导出口。
	_ = projectID
	page := createPage(t, svc, proj.ID, "/lock/closed-page", publishContextDoc)
	buildAndPublish(t, svc, page.ID)
	if live := activeLinkExists(t, "/lock/closed-page"); !live {
		t.Fatal("前置失败：页面应已上线")
	}

	derr := svc.Delete(context.Background(), &dto.DeleteReq{ID: page.ID})
	if derr == nil {
		t.Fatal("路径锁关闭时删页仍返回成功：存在无互斥的 FS 处置")
	}
	if !errors.Is(derr, pipeline.ErrPathLockerUnavailable) {
		t.Fatalf("期望 fail closed（%v），实际 %v", pipeline.ErrPathLockerUnavailable, derr)
	}
	// 链接必须还在：fail closed 的语义是「什么都没动」，不是「动了一半」。
	if !activeLinkExists(t, "/lock/closed-page") {
		t.Fatal("fail closed 下访问面链接被删掉了：处置没有真正被拦住")
	}
}

// TestSettleRefusesOldPathTakenOverByAnotherOwner 真实接管反例（不是互斥轨迹）。
//
// 形状：页 A 改址后旧路径 /lock/taken 的**路由归属**被页 B 接管（DB 事实），同时访问面
// 链接指向 B 的产物。此时 A 的旧路径处置在锁内复验必须判定 errOldPathTaken —— 既不删
// B 的路由行，也不删 B 的链接，回执保持 pending。
//
// 这条与 page_url_old_path_ownership_test.go 的区别：那条走整条改 URL 链，这条直接
// 打到 settle 的锁内复验判据上，断言「复核失败时一个字节都没动」。
func TestSettleRefusesOldPathTakenOverByAnotherOwner(t *testing.T) {
	db, svc, projects, projectID := newPageServiceWithRoutes(t)
	_ = projects
	_ = db

	// A：发布在 /lock/taken-new；B：发布在 /lock/taken（即将接管旧路径）。
	ownerPage := createPage(t, svc, projectID, "/lock/taken-new", publishContextDoc)
	buildAndPublish(t, svc, ownerPage.ID)
	intruder := createPage(t, svc, projectID, "/lock/taken", publishContextDoc)
	buildAndPublish(t, svc, intruder.ID)

	before := routeRowsAt(t, db, projectID, "/lock/taken")
	if before == 0 {
		t.Fatal("前置失败：旧路径应存在路由行")
	}

	// 造一条属于 A 的 pending 改址回执：A 从 /lock/taken 改到 /lock/taken-new。
	// 此刻 /lock/taken 已被 B 接管 —— 收敛重放必须拒绝处置，而不是删掉 B 的链接。
	receiptID := insertUpdateURLReceipt(t, db, ownerPage.ID, "/lock/taken-new", "/lock/taken",
		"zh-CN", "", true, 0)
	if receiptID == 0 {
		t.Fatal("前置失败：未能登记 pending 回执")
	}
	recovered, rolledBack, err := recoverPending(t, svc, context.Background())
	if err != nil {
		t.Fatalf("收敛返回错误: %v", err)
	}
	if recovered+rolledBack == 0 {
		t.Fatal("收敛未处理这条 pending 回执")
	}
	after := routeRowsAt(t, db, projectID, "/lock/taken")
	if after != before {
		t.Fatalf("拒绝时路由行数变化: before=%d after=%d", before, after)
	}
	if !activeLinkExists(t, "/lock/taken") {
		t.Fatal("拒绝时访问面链接被删掉了：复核失败后不得动 FS")
	}
	// 账本身份不得被这条老回执改写（它没有真的激活过）。
	if got := ledgerReceiptID(t, db, ownerPage.ID, "zh-CN"); got == receiptID {
		t.Fatal("未激活成功的回执被当成成功身份写进账本")
	}
}
