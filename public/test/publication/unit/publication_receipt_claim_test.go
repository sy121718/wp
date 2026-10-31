package unit

import (
	"context"
	"fmt"
	json "go_wp/pkg/jsonx"
	"strconv"
	"strings"
	"testing"
	"time"

	pubdto "go_wp/internal/module/publication/dto"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"

	"gorm.io/gorm"
)

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

func TestClaimPendingReceiptsFiltersAndLimits(t *testing.T) {
	svc := newUnitService(t)
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour)

	first := seedPendingReceipt(t, svc, ctx, "/claim-1", pubdto.ReceiptActionSwitchActive, pubdto.ReceiptSourcePage, base)
	second := seedPendingReceipt(t, svc, ctx, "/claim-2", pubdto.ReceiptActionUpdateURL, pubdto.ReceiptSourcePage, base.Add(time.Minute))

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

	n, cerr := svc.CountPendingReceipts(ctx, claimQuery(0))
	if cerr != nil {
		t.Fatalf("统计失败: %v", cerr)
	}
	if n != 2 {
		t.Fatalf("同口径待办数应为 2，实际 %d", n)
	}

	all, aerr := svc.ClaimPendingReceipts(ctx, &pubdto.ReceiptsQueryReq{Limit: 10})
	if aerr != nil {
		t.Fatalf("不带过滤条件领取失败: %v", aerr)
	}
	if len(all) != 4 {
		t.Fatalf("不带过滤条件应领到全部 4 条 pending，实际 %d", len(all))
	}
}

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

func TestClaimPendingReceiptsSkipsLockedRows(t *testing.T) {
	svc := newUnitService(t)
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour)
	lockedID := seedPendingReceipt(t, svc, ctx, "/claim-locked", pubdto.ReceiptActionSwitchActive, pubdto.ReceiptSourcePage, base)
	nextID := seedPendingReceipt(t, svc, ctx, "/claim-next", pubdto.ReceiptActionSwitchActive, pubdto.ReceiptSourcePage, base.Add(time.Minute))

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
