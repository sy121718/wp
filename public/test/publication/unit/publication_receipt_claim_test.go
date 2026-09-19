package unit

// publication_receipt_claim_test.go —— 回执收敛的领取端（分批 + SKIP LOCKED + 口径过滤）。
//
// 领取是收敛例程唯一的多实例互斥点（page 侧的收敛按批领取后逐条重放），
// 因此两条性质必须钉住：
//   · 领取口径与「谁能收敛」一致 —— 领到不属于自己的行只会被反复领取再跳过；
//   · 已被别的实例锁住的行要跳过，而不是排队等锁（SKIP LOCKED 的真实语义）。

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	pubdto "go_wp/internal/module/publication/dto"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"

	"gorm.io/gorm"
)

// claimQuery 与 page 侧收敛例程用的过滤条件同形（归属 + 三种访问面切换动作）。
func claimQuery(limit int) *pubdto.ReceiptsQueryReq {
	return &pubdto.ReceiptsQueryReq{
		SourceType: pubdto.ReceiptSourcePage,
		Actions: []string{
			pubdto.ReceiptActionSwitchActive,
			pubdto.ReceiptActionUpdateURL,
			pubdto.ReceiptActionRollback,
		},
		Limit: limit,
	}
}

// seedPendingReceipt 直插一条 pending 回执（时间显式给，避免同微秒并列导致领取顺序不确定）。
func seedPendingReceipt(t *testing.T, svc *pubservice.Service, ctx context.Context,
	path, action, sourceType string, at time.Time) int64 {
	t.Helper()
	row := &pubmodel.ReceiptEntity{
		SourceType: sourceType, SourceID: pageID, Action: action, Path: path,
		ReceiptState: pubmodel.ReceiptPending,
		ReceiptData:  json.RawMessage(`{"lang":"zh-CN"}`),
		CreateTime:   at,
	}
	if err := svc.Model().ReceiptDB(ctx).Create(row).Error; err != nil {
		t.Fatalf("插入 pending 回执失败: %v", err)
	}
	return row.ID
}

// TestClaimPendingReceiptsFiltersAndLimits 领取按 create_time 升序、遵守单批上限，且过滤口径精确。
func TestClaimPendingReceiptsFiltersAndLimits(t *testing.T) {
	svc := newUnitService(t)
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour)

	first := seedPendingReceipt(t, svc, ctx, "/claim-1", pubdto.ReceiptActionSwitchActive, pubdto.ReceiptSourcePage, base)
	second := seedPendingReceipt(t, svc, ctx, "/claim-2", pubdto.ReceiptActionUpdateURL, pubdto.ReceiptSourcePage, base.Add(time.Minute))
	// 两条**不该被领到**的行：路由回执（activate）与自动发布实例的回执，各有自己的处理方。
	seedPendingReceipt(t, svc, ctx, "/claim-route", "activate", pubdto.ReceiptSourcePage, base.Add(2*time.Minute))
	seedPendingReceipt(t, svc, ctx, "/claim-pres", pubdto.ReceiptActionSwitchActive, pubdto.ReceiptSourcePresentation, base)

	items, err := svc.ClaimPendingReceipts(ctx, claimQuery(2))
	if err != nil {
		t.Fatalf("领取失败: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("单批上限 2，应领到 2 条，实际 %d", len(items))
	}
	if items[0].ID != strconv.FormatInt(first, 10) || items[1].ID != strconv.FormatInt(second, 10) {
		t.Fatalf("应按 create_time 升序领取，实际 %s / %s", items[0].ID, items[1].ID)
	}

	// 统计与领取同口径：只有那 2 条 page 的访问面切换回执。口径分叉会表现为
	// 「健康检查报 0 而实际有残留」，所以这里与领取用同一个 Req 断言。
	n, cerr := svc.CountPendingReceipts(ctx, claimQuery(0))
	if cerr != nil {
		t.Fatalf("统计失败: %v", cerr)
	}
	if n != 2 {
		t.Fatalf("同口径待办数应为 2，实际 %d", n)
	}

	// 不筛动作时能领到路由回执 —— 证明上面那 2 条确实是被动作条件挡住的，而不是没插进去。
	all, aerr := svc.ClaimPendingReceipts(ctx, &pubdto.ReceiptsQueryReq{Limit: 10})
	if aerr != nil {
		t.Fatalf("不带过滤条件领取失败: %v", aerr)
	}
	if len(all) != 4 {
		t.Fatalf("不带过滤条件应领到全部 4 条 pending，实际 %d", len(all))
	}
}

// TestPendingReceiptsPartialIndexExists 迁移 267：pending 部分索引真的建出来了（空转查询必须廉价）。
//
// 断言的是**形态**而不只是名字：索引键列与部分谓词任一处写错，领取与计数就退回全表扫描，
// 而这件事在功能测试里完全看不出来（结果一样，只是慢）—— 只增不删的回执表上会越跑越慢。
func TestPendingReceiptsPartialIndexExists(t *testing.T) {
	svc := newUnitService(t)
	var def string
	if err := svc.Model().ReceiptDB(context.Background()).
		Raw("SELECT indexdef FROM pg_indexes WHERE schemaname = current_schema() AND tablename = 'publication_receipts' AND indexname = 'idx_publication_receipts_pending'").
		Scan(&def).Error; err != nil {
		t.Fatalf("查询索引定义失败: %v", err)
	}
	if def == "" {
		t.Fatal("未找到迁移 267 建出的部分索引 idx_publication_receipts_pending")
	}
	if !strings.Contains(def, "create_time") || !strings.Contains(def, "receipt_state = 'pending'") {
		t.Fatalf("部分索引形态不符（应为 (create_time) WHERE receipt_state = 'pending'）: %s", def)
	}
}

// TestClaimPendingReceiptsSkipsLockedRows SKIP LOCKED 生效：被别的实例锁住的行要跳过，不排队等锁。
func TestClaimPendingReceiptsSkipsLockedRows(t *testing.T) {
	svc := newUnitService(t)
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour)
	lockedID := seedPendingReceipt(t, svc, ctx, "/claim-locked", pubdto.ReceiptActionSwitchActive, pubdto.ReceiptSourcePage, base)
	nextID := seedPendingReceipt(t, svc, ctx, "/claim-next", pubdto.ReceiptActionSwitchActive, pubdto.ReceiptSourcePage, base.Add(time.Minute))

	// 在一个独立事务里锁住最旧的那一行（模拟另一个实例正在收敛它），
	// 持锁期间用**另一条连接**领取：必须跳过被锁的行，而不是等锁超时。
	lockedErr := svc.Model().ReceiptDB(ctx).Transaction(func(tx *gorm.DB) error {
		var id int64
		if err := tx.Raw("SELECT id FROM publication_receipts WHERE receipt_state = 'pending' ORDER BY create_time ASC LIMIT 1 FOR UPDATE").
			Scan(&id).Error; err != nil {
			return err
		}
		if id != lockedID {
			return fmt.Errorf("夹具不符：被锁的应是最旧的一条 %d，实际 %d", lockedID, id)
		}
		items, cerr := svc.ClaimPendingReceipts(ctx, claimQuery(1))
		if cerr != nil {
			return cerr
		}
		if len(items) != 1 || items[0].ID != strconv.FormatInt(nextID, 10) {
			return fmt.Errorf("SKIP LOCKED 未生效：应领到下一条 %d，实际 %+v", nextID, items)
		}
		return nil
	})
	if lockedErr != nil {
		t.Fatalf("持锁领取失败: %v", lockedErr)
	}
}
