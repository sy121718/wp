package feature

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

func TestNilPathLockerFailsClosedAndKeepsReceiptPending(t *testing.T) {
	f := newMenuFenceFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	f.pres.SetPublicationPathLocker(nil)

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

	oldState, oerr := store.Inspect("/menu-fence")
	if oerr != nil {
		t.Fatalf("检查旧路径失败: %v", oerr)
	}
	if oldState == nil || oldState.Kind == pipeline.PublicationNone {
		t.Fatalf("fail closed 之后旧路径的线上链接必须仍在")
	}
}

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
