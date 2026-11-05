package unit

import (
	"context"
	publish "go_wp/pkg/publish"
	"sync"
	"testing"
	"time"

	pagedto "go_wp/internal/module/page/dto"
	pageservice "go_wp/internal/module/page/service"
	pubmodel "go_wp/internal/module/publication/model"
)

func TestPageServiceUsesProcessSharedPathLock(t *testing.T) {
	shared := publish.DefaultPathLocker()
	if shared == nil {
		t.Fatal("进程默认路径锁为空：各服务会各自 new 一个，等于没加锁")
	}
	if publish.DefaultPathLocker() != shared {
		t.Fatal("DefaultPathLocker 不是稳定单例")
	}
	_, svc, _, _ := newPageServiceWithRoutes(t)

	concrete, ok := svc.(*pageservice.Service)
	if !ok {
		t.Fatalf("页面服务具体类型不是 *pageservice.Service，实际 %T", svc)
	}
	concrete.SetPublicationPathLocker(shared)
	concrete.SetPublicationPathLocker(publish.DefaultPathLocker())
}

func TestPathLockSerializesSamePathTwoPhases(t *testing.T) {
	locker := &publish.ProcessPathLocker{}
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

	run("A")

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

func TestPathLockConcurrentSamePathNeverOverlaps(t *testing.T) {
	locker := &publish.ProcessPathLocker{}
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

func TestPageRedirectFailsClosedWithoutPathLock(t *testing.T) {
	db, svc, _, projectID := newPageServiceWithRoutes(t)
	ctx := context.Background()

	concrete, ok := svc.(*pageservice.Service)
	if !ok {
		t.Fatalf("页面服务具体类型不是 *pageservice.Service，实际 %T", svc)
	}
	concrete.SetPublicationPathLocker(nil)
	defer concrete.SetPublicationPathLocker(publish.DefaultPathLocker())

	err := svc.DeleteRedirect(context.Background(), &pagedto.RedirectDeleteReq{
		ProjectID: projectID,
		Path:      "/lock/closed",
	})
	if err == nil {
		t.Fatal("路径锁关闭时删除重定向仍返回成功：存在无互斥的 FS 处置")
	}

	page := createPage(t, svc, projectID, "/lock/closed-page", publishContextDoc)
	buildAndPublish(t, svc, page.ID)
	if live := activeLinkExists(t, "/lock/closed-page"); !live {
		t.Fatal("前置失败：页面应已上线")
	}

	if derr := svc.Delete(ctx, &pagedto.DeleteReq{ID: page.ID}); derr != nil {
		t.Logf("删除返回错误（允许：撤链失败也会保 pending 交收敛）: %v", derr)
	}

	if !activeLinkExists(t, "/lock/closed-page") {
		t.Fatal("fail closed 下访问面链接被删掉了：处置没有真正被拦住")
	}
	if kind := activeKind(t, "/lock/closed-page"); kind != "page" {
		t.Fatalf("fail closed 下访问面应保持页面激活，实际 %q", kind)
	}

	rec := deactivationReceiptOf(t, db, page.ID)
	if rec.ReceiptState != pubmodel.ReceiptPending {
		t.Fatalf("撤链被 fail closed 时回执必须保持 pending，实际 %q", rec.ReceiptState)
	}

	if !pageservice.DeactivationBusinessCommittedFromData(rec.ReceiptData) {
		t.Fatal("业务事务应已提交（businessCommitted=true），否则收敛永远不会撤链")
	}
	var deletedAt *time.Time
	if qerr := db.Raw(`SELECT deleted_at FROM pages WHERE id = ?`, page.ID).Scan(&deletedAt).Error; qerr != nil {
		t.Fatalf("读取页面失败: %v", qerr)
	}
	if deletedAt == nil {
		t.Fatal("业务事务已提交，页面应已被软删")
	}
}

func TestSettleRefusesOldPathTakenOverByAnotherOwner(t *testing.T) {
	db, svc, projects, projectID := newPageServiceWithRoutes(t)
	_ = projects
	_ = db

	ownerPage := createPage(t, svc, projectID, "/lock/taken-new", publishContextDoc)
	buildAndPublish(t, svc, ownerPage.ID)
	intruder := createPage(t, svc, projectID, "/lock/taken", publishContextDoc)
	buildAndPublish(t, svc, intruder.ID)

	before := routeRowsAt(t, db, projectID, "/lock/taken")
	if before == 0 {
		t.Fatal("前置失败：旧路径应存在路由行")
	}

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

	if got := ledgerReceiptID(t, db, ownerPage.ID, "zh-CN"); got == receiptID {
		t.Fatal("未激活成功的回执被当成成功身份写进账本")
	}
}
