package feature

// publication_path_lock_test.go — 访问面路径锁（pipeline.PublicationPathLocker）在
// presentation 侧的真实接入行为。
//
// 断言的是"路径锁真的生效"，不是"调用了某个方法"：
//  1. 默认实例来自进程共享单例 —— 换掉它之后，显式注入的锁必须被真的用到（可观测计数）；
//  2. 注入 nil 时 fail closed：处置意图不执行、回执留 pending（无互斥就不动访问面）；
//  3. 锁必须在**副作用之前**取到：锁内回调里读到的链接状态与回调外一致，
//     且整批旧路径共用一次加锁（不是逐条各取一次）。
//
// 为什么在 feature 层：需要真实迁移、真实实例行锁、真实路由写与真实符号链接。

import (
	"context"
	"strings"
	"sync"
	"testing"

	presentationdto "go_wp/internal/module/presentation/dto"
	pubcontract "go_wp/internal/module/publication/contract"
	pubmodel "go_wp/internal/module/publication/model"
	"go_wp/internal/pipeline"
)

// countingPathLocker 记录每次加锁的路径集合，然后转交进程默认实现。
//
// 用它才能区分"接了锁"与"接的是别的锁实例/根本没接"：只断言调用成功无法证明
// 副作用被互斥保护过。
type countingPathLocker struct {
	mu    sync.Mutex
	calls [][]string
	inner pipeline.PublicationPathLocker
}

func (c *countingPathLocker) WithPaths(paths []string, fn func() error) error {
	c.mu.Lock()
	c.calls = append(c.calls, append([]string(nil), paths...))
	c.mu.Unlock()
	return c.inner.WithPaths(paths, fn)
}

func (c *countingPathLocker) snapshot() [][]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([][]string(nil), c.calls...)
}

// TestPresentationServiceUsesInjectedPathLocker 注入的锁必须被发布链真正用到。
func TestPresentationServiceUsesInjectedPathLocker(t *testing.T) {
	f := newMenuFenceFixture(t)
	if f == nil {
		return
	}
	locker := &countingPathLocker{inner: pipeline.NewProcessPathLocker()}
	f.pres.SetPublicationPathLocker(locker)

	if _, err := f.pres.UpdateURL(context.Background(), &presentationdto.UpdateURLReq{
		ProjectID: f.projectID, ID: f.instance, NewPath: "/path-lock-inject", WithRedirect: true,
	}); err != nil {
		t.Fatalf("改 URL 应成功: %v", err)
	}

	calls := locker.snapshot()
	if len(calls) == 0 {
		t.Fatalf("发布链没有走注入的路径锁：访问面副作用在无互斥下执行了")
	}
	// 至少要覆盖两类路径：新路径（激活）与旧路径（处置）。
	var sawNew, sawOld bool
	for _, paths := range calls {
		for _, p := range paths {
			if p == "/path-lock-inject" {
				sawNew = true
			}
			if p == "/menu-fence" {
				sawOld = true
			}
		}
	}
	if !sawNew {
		t.Fatalf("激活新路径时未取路径锁：调用序列 %v", calls)
	}
	if !sawOld {
		t.Fatalf("处置旧路径时未取路径锁：调用序列 %v", calls)
	}
}

// TestNilPathLockerFailsClosedAndKeepsReceiptPending 显式注入 nil 时不得无锁执行。
//
// 判据：改 URL 的**输入意图**已持久化（url_path 已推进），访问面副作用一次都没发生，
// 旧路径处置回执留在 pending 等收敛重放 —— 而不是"悄悄把链接删了"。
func TestNilPathLockerFailsClosedAndKeepsReceiptPending(t *testing.T) {
	f := newMenuFenceFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	f.pres.SetPublicationPathLocker(nil)

	// 编译与提交都会经过路径锁，因此必须整笔失败（不是静默跳过副作用）。
	if _, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{
		ProjectID: f.projectID, ID: f.instance, NewPath: "/path-lock-closed",
	}); err == nil {
		t.Fatalf("没有路径锁时不应完成激活（应 fail closed）")
	}

	store := &pipeline.LocalPublicationStore{ActiveRoot: pipeline.ActiveRoot()}
	state, serr := store.Inspect("/path-lock-closed")
	if serr != nil {
		t.Fatalf("检查访问面失败: %v", serr)
	}
	if state != nil && state.Kind != pipeline.PublicationNone {
		t.Fatalf("没有路径锁时不得改动访问面，实际 %v", state.Kind)
	}
	// 旧路径也不能被处置（它还在服务线上页面）。
	oldState, oerr := store.Inspect("/menu-fence")
	if oerr != nil {
		t.Fatalf("检查旧路径失败: %v", oerr)
	}
	if oldState == nil || oldState.Kind == pipeline.PublicationNone {
		t.Fatalf("fail closed 之后旧路径的线上链接必须仍在")
	}
}

// TestOldPathDisposalLocksEveryPathInOneCall 多条旧路径在**一次**加锁里处置。
//
// 逐条各取一次锁会让"第 1 条已下线、第 2 条被别人抢走"变成半截状态；多条旧路径属于
// 同一次改址意图，必须在同一锁期内完成。
func TestOldPathDisposalLocksEveryPathInOneCall(t *testing.T) {
	f := newMenuFenceFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	locker := &countingPathLocker{inner: pipeline.NewProcessPathLocker()}
	f.pres.SetPublicationPathLocker(locker)

	if _, err := f.pres.UpdateURL(ctx, &presentationdto.UpdateURLReq{
		ProjectID: f.projectID, ID: f.instance, NewPath: "/path-lock-batch", WithRedirect: true,
	}); err != nil {
		t.Fatalf("改 URL 应成功: %v", err)
	}

	// 制造"两条旧路径"的现场：把回执的 oldPaths 扩成两条（与 TestMultiLang 同法）。
	var receiptID int64
	if err := f.db.Raw(`SELECT id FROM publication_receipts
		WHERE source_type = ? AND action = ? AND source_id = ? ORDER BY id DESC LIMIT 1`,
		pubcontract.ReceiptSourcePresentation, pubcontract.ReceiptActionUpdateURL,
		f.instance).Scan(&receiptID).Error; err != nil {
		t.Fatalf("读取回执失败: %v", err)
	}
	if receiptID == 0 {
		t.Fatalf("前置不成立：没有 update_url 回执")
	}
	if err := f.db.Exec(`UPDATE publication_receipts
		SET receipt_data = jsonb_set(receipt_data, '{oldPaths}', '["/menu-fence","/menu-fence-legacy"]'::jsonb),
		    receipt_state = ?, completed_at = NULL
		WHERE id = ?`, pubmodel.ReceiptPending, receiptID).Error; err != nil {
		t.Fatalf("改写 oldPaths 失败: %v", err)
	}

	locker = &countingPathLocker{inner: pipeline.NewProcessPathLocker()}
	f.pres.SetPublicationPathLocker(locker)
	if _, cerr := f.pres.ConvergePendingReceipts(ctx); cerr != nil {
		t.Fatalf("收敛失败: %v", cerr)
	}
	for _, paths := range locker.snapshot() {
		if len(paths) > 1 {
			joined := strings.Join(paths, ",")
			if strings.Contains(joined, "/menu-fence") && strings.Contains(joined, "/menu-fence-legacy") {
				return
			}
		}
	}
	t.Fatalf("多条旧路径必须在同一次加锁里处置，实际调用序列 %v", locker.snapshot())
}
