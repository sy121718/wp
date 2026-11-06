package feature

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	orderdto "go_wp/internal/module/order/dto"
	orderenums "go_wp/internal/module/order/enums"
	ordermodel "go_wp/internal/module/order/model"
	orderservice "go_wp/internal/module/order/service"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/pkg/bulkids"
	"go_wp/pkg/facing"
	"go_wp/public/test/support"
)

// couponBulkFixture 只装优惠码写路径需要的模型与工程查询，避免为原子性用例
// 拉起整条订单/库存链路；表结构仍来自生产迁移。
type couponBulkFixture struct {
	db        *gorm.DB
	svc       *orderservice.Service
	coupons   *ordermodel.CouponModel
	projectID string
}

func newCouponBulkFixture(t *testing.T) *couponBulkFixture {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	if db == nil {
		return nil
	}
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "券批量原子性工程")
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	coupons := ordermodel.NewCouponModel(db)
	svc := orderservice.NewService(
		ordermodel.NewOrderModel(db), ordermodel.NewOrderItemModel(db), ordermodel.NewOrderStatusLogModel(db),
		coupons, ordermodel.NewReturnModel(db),
		nil, nil, nil, nil,
	)
	svc.SetProjects(projects)
	return &couponBulkFixture{db: db, svc: svc, coupons: coupons, projectID: projectID}
}

func (f *couponBulkFixture) addCoupon(t *testing.T, code string) *orderdto.CouponResp {
	t.Helper()
	res, err := f.svc.CreateCoupon(context.Background(), &orderdto.CouponSaveReq{
		ProjectID: f.projectID, Code: code, Name: "券 " + code,
		DiscountType: "fixed", DiscountValue: 100, Status: 1,
	})
	if err != nil {
		t.Fatalf("建券 %q 失败: %v", code, err)
	}
	return res
}

func (f *couponBulkFixture) exists(t *testing.T, id uint64) bool {
	t.Helper()
	var n int64
	if err := f.db.Table("coupons").Where("id = ?", id).Count(&n).Error; err != nil {
		t.Fatalf("查券失败: %v", err)
	}
	return n > 0
}

func (f *couponBulkFixture) redemptionRows(t *testing.T, couponID uint64) int64 {
	t.Helper()
	var n int64
	if err := f.db.Table("coupon_redemptions").Where("coupon_id = ?", couponID).Count(&n).Error; err != nil {
		t.Fatalf("查核销记录失败: %v", err)
	}
	return n
}

// writeRedemptionTx 复刻核销写入口：先锁券行，再写核销明细并累加已用次数。
func (f *couponBulkFixture) writeRedemptionTx(t *testing.T, ctx context.Context, tx *gorm.DB,
	couponID uint64, orderID uint64) error {
	t.Helper()
	e, err := f.coupons.LockByIDTx(ctx, tx, f.projectID, couponID)
	if err != nil {
		return err
	}
	if e == nil {
		return orderenums.Err(orderenums.ErrCouponNotFound)
	}
	if _, err = f.coupons.InsertRedemptionTx(ctx, tx, &ordermodel.CouponRedemptionEntity{
		CouponID: couponID, ProjectID: f.projectID, Code: e.Code,
		OrderID: orderID, OrderNo: "NO-" + uuid.NewString()[:8],
		DiscountAmount: 100, CreateTime: time.Now(),
	}); err != nil {
		return err
	}
	_, err = f.coupons.IncrementUsedTx(ctx, tx, couponID)
	return err
}

func couponBlockedItems(t *testing.T, err error) []facing.BlockedItem {
	t.Helper()
	var blocked facing.BlockedError
	if !errors.As(err, &blocked) {
		t.Fatalf("错误应带可定位的拒绝条目，实际 %T: %v", err, err)
	}
	return blocked.BlockedItems()
}

// TestOrderCouponBulkIDsDedupeNumericAliases 覆盖 "1" 与 "01" 这类数字别名：
// 按数值身份去重后再计数，不能把同一张券当成两个目标。
func TestOrderCouponBulkIDsDedupeNumericAliases(t *testing.T) {
	f := newCouponBulkFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	cp := f.addCoupon(t, "ALIAS-ONE")

	// 数字别名 + 同值重复：只算一个目标。
	deleted, err := f.svc.DeleteCoupons(ctx, &orderdto.CouponBulkDeleteReq{
		ProjectID: f.projectID,
		IDs:       []string{formatID(cp.ID), "0" + formatID(cp.ID), formatID(cp.ID)},
	})
	if err != nil {
		t.Fatalf("数字别名应去重后成功：%v", err)
	}
	if deleted != 1 {
		t.Fatalf("同一张券的三种写法应只删 1 张，实际 %d", deleted)
	}
	if f.exists(t, cp.ID) {
		t.Fatal("目标券应已删除")
	}

	// 超过上限时按去重后的数量判断：MaxIDs 个别名不能触发超限。
	raw := make([]string, 0, 2*bulkids.MaxIDs)
	for i := 1; i <= bulkids.MaxIDs; i++ {
		id := strconv.Itoa(i)
		raw = append(raw, id, "0"+id)
	}
	if _, err := f.svc.DeleteCoupons(ctx, &orderdto.CouponBulkDeleteReq{
		ProjectID: f.projectID, IDs: raw,
	}); err != nil {
		var tooMany *bulkids.IDsError
		if errors.As(err, &tooMany) {
			t.Fatalf("去重后恰好 %d 个目标不应触发超限：%v", bulkids.MaxIDs, err)
		}
		// 这里的目标都不存在，整批 404 才是正确结论。
		if status, _ := facing.StatusOf(err); status != 404 {
			t.Fatalf("去重后应只剩「全部不存在」，实际 %d（%v）", status, err)
		}
	}
}

// TestOrderCouponBulkDeleteRejectsMixedMissingAndRedeemedWithoutSideEffects
// 覆盖混合缺失 / 已核销：整批拒绝且两处都零提交。
func TestOrderCouponBulkDeleteRejectsMixedMissingAndRedeemedWithoutSideEffects(t *testing.T) {
	f := newCouponBulkFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	clean := f.addCoupon(t, "MIX-CLEAN")
	used := f.addCoupon(t, "MIX-USED")

	if err := f.coupons.Transaction(ctx, func(tx *gorm.DB) error {
		return f.writeRedemptionTx(t, ctx, tx, used.ID, 900001)
	}); err != nil {
		t.Fatalf("写入核销记录失败: %v", err)
	}

	_, err := f.svc.DeleteCoupons(ctx, &orderdto.CouponBulkDeleteReq{
		ProjectID: f.projectID,
		IDs:       []string{formatID(clean.ID), formatID(used.ID), "999999999"},
	})
	if err == nil {
		t.Fatal("混合缺失与已核销必须整批拒绝")
	}
	if status, _ := facing.StatusOf(err); status != 409 {
		t.Fatalf("已核销优先按冲突 409，实际 %d（%v）", status, err)
	}
	items := couponBlockedItems(t, err)
	if len(items) != 2 {
		t.Fatalf("应给出两条拒绝原因（缺失 + 已核销），实际 %d: %+v", len(items), items)
	}
	if !f.exists(t, clean.ID) || !f.exists(t, used.ID) {
		t.Fatal("整批拒绝后两张券都必须还在")
	}
	if n := f.redemptionRows(t, used.ID); n != 1 {
		t.Fatalf("已核销记录不得被动，实际 %d 条", n)
	}

	// 只有缺失：404，且已存在的券不删。
	if _, err := f.svc.DeleteCoupons(ctx, &orderdto.CouponBulkDeleteReq{
		ProjectID: f.projectID,
		IDs:       []string{formatID(clean.ID), "999999998"},
	}); err == nil {
		t.Fatal("含不存在 id 必须整批拒绝")
	} else if status, _ := facing.StatusOf(err); status != 404 {
		t.Fatalf("缺失应按 404，实际 %d（%v）", status, err)
	}
	if !f.exists(t, clean.ID) {
		t.Fatal("整批拒绝后已存在的券不得被删")
	}
}

// TestOrderCouponBulkDeleteRollsBackWhenLaterDeleteFails 覆盖「后项故障」：
// 触发器只在第一张券已在本事务内删除时才失败，用于见证前项已写、随后整体回滚。
func TestOrderCouponBulkDeleteRollsBackWhenLaterDeleteFails(t *testing.T) {
	f := newCouponBulkFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	first := f.addCoupon(t, "ROLL-FIRST")
	second := f.addCoupon(t, "ROLL-SECOND")
	if first.ID >= second.ID {
		t.Fatalf("需要 first 先于 second 被删除：first=%d second=%d", first.ID, second.ID)
	}

	fn := "coupon_test_rollback_guard"
	trg := fn + "_trg"
	if err := f.db.Exec(`
CREATE OR REPLACE FUNCTION ` + fn + `() RETURNS trigger AS $$
BEGIN
    IF OLD.id = ` + formatID(second.ID) + ` AND NOT EXISTS (SELECT 1 FROM coupons WHERE id = ` + formatID(first.ID) + `) THEN
        RAISE EXCEPTION 'injected coupon delete failure after first write';
    END IF;
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER ` + trg + ` BEFORE DELETE ON coupons
FOR EACH ROW EXECUTE FUNCTION ` + fn + `();`).Error; err != nil {
		t.Fatalf("装故障触发器失败: %v", err)
	}
	t.Cleanup(func() {
		_ = f.db.Exec(`DROP TRIGGER IF EXISTS ` + trg + ` ON coupons`).Error
		_ = f.db.Exec(`DROP FUNCTION IF EXISTS ` + fn + `()`).Error
	})

	if _, err := f.svc.DeleteCoupons(ctx, &orderdto.CouponBulkDeleteReq{
		ProjectID: f.projectID,
		IDs:       []string{formatID(first.ID), formatID(second.ID)},
	}); err == nil {
		t.Fatal("后项 DB 故障必须整批失败")
	}
	for _, id := range []uint64{first.ID, second.ID} {
		if !f.exists(t, id) {
			t.Fatalf("整批回滚后 id=%d 不该被删", id)
		}
	}
}

// TestOrderCouponBulkStatusOnlyWritesStatusAndKeepsConcurrentEdit
// 覆盖启停只改 status，且与并发编辑按券行锁串行，不覆盖对方写入的其它字段。
func TestOrderCouponBulkStatusOnlyWritesStatusAndKeepsConcurrentEdit(t *testing.T) {
	f := newCouponBulkFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	cp, err := f.svc.CreateCoupon(ctx, &orderdto.CouponSaveReq{
		ProjectID: f.projectID, Code: "TOGGLE-ATOMIC", Name: "启停原子券",
		DiscountType: "fixed", DiscountValue: 2000, MinSubtotal: 9900,
		MaxUses: 100, PerUserLimit: 2, Remark: "活动说明",
		StartsAt: "2026-01-01 09:00", EndsAt: "2026-01-31 23:59", Status: 1,
	})
	if err != nil {
		t.Fatalf("建券失败: %v", err)
	}

	// 先来一次串行启停：除 status 与审计列外，其它字段必须原样。
	if _, err := f.svc.SetCouponsStatus(ctx, &orderdto.CouponBulkStatusReq{
		ProjectID: f.projectID, IDs: []string{formatID(cp.ID)}, Status: 0,
	}); err != nil {
		t.Fatalf("批量停用失败: %v", err)
	}
	after, err := f.svc.GetCoupon(ctx, cp.ID)
	if err != nil || after == nil {
		t.Fatalf("重读券失败: %v", err)
	}
	if after.Status != 0 {
		t.Fatalf("status 应改成 0，实际 %d", after.Status)
	}
	if after.Name != "启停原子券" || after.DiscountType != "fixed" || after.DiscountValue != 2000 ||
		after.MinSubtotal != 9900 || after.MaxUses != 100 || after.PerUserLimit != 2 || after.Remark != "活动说明" {
		t.Fatalf("启停不该覆盖其它字段：%+v", after)
	}
	if after.StartsAt == nil || after.EndsAt == nil {
		t.Fatalf("启停后时间窗丢失：%+v", after)
	}

	// 并发编辑：对方先锁住券行并改名，启停必须等它提交后再只写 status。
	started := make(chan struct{})
	release := make(chan struct{})
	editErr := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		editErr <- f.coupons.Transaction(ctx, func(tx *gorm.DB) error {
			if _, lerr := f.coupons.LockByIDTx(ctx, tx, f.projectID, cp.ID); lerr != nil {
				return lerr
			}
			close(started)
			<-release
			return tx.Model(&ordermodel.CouponEntity{}).
				Where("project_id = ? AND id = ?", f.projectID, cp.ID).
				Updates(map[string]any{"name": "并发改名", "remark": "并发备注"}).Error
		})
	}()
	<-started

	toggleDone := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, terr := f.svc.SetCouponsStatus(ctx, &orderdto.CouponBulkStatusReq{
			ProjectID: f.projectID, IDs: []string{formatID(cp.ID)}, Status: 1,
		})
		toggleDone <- terr
	}()

	time.Sleep(150 * time.Millisecond)
	close(release)
	wg.Wait()
	close(editErr)
	close(toggleDone)
	if eerr := <-editErr; eerr != nil {
		t.Fatalf("并发编辑失败: %v", eerr)
	}
	if terr := <-toggleDone; terr != nil {
		t.Fatalf("并发启停失败: %v", terr)
	}

	final, err := f.svc.GetCoupon(ctx, cp.ID)
	if err != nil || final == nil {
		t.Fatalf("最终读取券失败: %v", err)
	}
	if final.Name != "并发改名" || final.Remark != "并发备注" {
		t.Fatalf("启停覆盖了并发编辑写入的字段：name=%q remark=%q", final.Name, final.Remark)
	}
	if final.Status != 1 {
		t.Fatalf("启停最终应把 status 改成 1，实际 %d", final.Status)
	}
}

// TestOrderCouponDeleteAndRedeemShareCouponLock 覆盖删除与核销共用券行锁：
// 核销先持锁时删除等待并最终按「已核销」拒绝；删除先提交时核销拿不到券行且不留孤儿。
func TestOrderCouponDeleteAndRedeemShareCouponLock(t *testing.T) {
	f := newCouponBulkFixture(t)
	if f == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	t.Run("核销先持锁时删除等待并拒绝", func(t *testing.T) {
		cp := f.addCoupon(t, "LOCK-REDEEM-FIRST")
		locked := make(chan struct{})
		release := make(chan struct{})
		redeemDone := make(chan error, 1)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			redeemDone <- f.coupons.Transaction(ctx, func(tx *gorm.DB) error {
				e, lerr := f.coupons.LockByIDTx(ctx, tx, f.projectID, cp.ID)
				if lerr != nil {
					return lerr
				}
				if e == nil {
					return orderenums.Err(orderenums.ErrCouponNotFound)
				}
				close(locked)
				<-release
				if _, ierr := f.coupons.InsertRedemptionTx(ctx, tx, &ordermodel.CouponRedemptionEntity{
					CouponID: cp.ID, ProjectID: f.projectID, Code: e.Code,
					OrderID: 900002, OrderNo: "NO-LOCK-1", DiscountAmount: 100, CreateTime: time.Now(),
				}); ierr != nil {
					return ierr
				}
				_, ierr := f.coupons.IncrementUsedTx(ctx, tx, cp.ID)
				return ierr
			})
		}()
		<-locked

		deleteDone := make(chan error, 1)
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, derr := f.svc.DeleteCoupons(ctx, &orderdto.CouponBulkDeleteReq{
				ProjectID: f.projectID, IDs: []string{formatID(cp.ID)},
			})
			deleteDone <- derr
		}()

		time.Sleep(150 * time.Millisecond)
		close(release)
		wg.Wait()
		close(redeemDone)
		close(deleteDone)
		if rerr := <-redeemDone; rerr != nil {
			t.Fatalf("核销事务失败: %v", rerr)
		}
		derr := <-deleteDone
		if derr == nil {
			t.Fatal("核销已落库时删除必须被拒绝")
		}
		if status, _ := facing.StatusOf(derr); status != 409 {
			t.Fatalf("已核销应按 409 拒绝，实际 %d（%v）", status, derr)
		}
		if !f.exists(t, cp.ID) {
			t.Fatal("被拒后券必须还在")
		}
		if n := f.redemptionRows(t, cp.ID); n != 1 {
			t.Fatalf("核销记录应恰好 1 条，实际 %d", n)
		}
	})

	t.Run("删除先提交时核销拿不到券且无孤儿", func(t *testing.T) {
		cp := f.addCoupon(t, "LOCK-DELETE-FIRST")
		if _, err := f.svc.DeleteCoupons(ctx, &orderdto.CouponBulkDeleteReq{
			ProjectID: f.projectID, IDs: []string{formatID(cp.ID)},
		}); err != nil {
			t.Fatalf("删除应成功: %v", err)
		}
		err := f.coupons.Transaction(ctx, func(tx *gorm.DB) error {
			return f.writeRedemptionTx(t, ctx, tx, cp.ID, 900003)
		})
		if err == nil {
			t.Fatal("券已删除后核销必须失败")
		}
		if f.exists(t, cp.ID) {
			t.Fatal("券应已被删除")
		}
		if n := f.redemptionRows(t, cp.ID); n != 0 {
			t.Fatalf("不得留下孤儿核销记录，实际 %d 条", n)
		}
	})
}

// TestOrderCouponBulkDeleteReverseOrderDoesNotDeadlock 覆盖反序 id：
// 两个批量请求都按 id 升序加锁，最多一个成功，另一个按缺失拒绝，不得出现死锁。
func TestOrderCouponBulkDeleteReverseOrderDoesNotDeadlock(t *testing.T) {
	f := newCouponBulkFixture(t)
	if f == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	a := f.addCoupon(t, "REV-A")
	b := f.addCoupon(t, "REV-B")

	type outcome struct {
		deleted int64
		err     error
	}
	results := make(chan outcome, 2)
	var wg sync.WaitGroup
	run := func(ids []uint64) {
		defer wg.Done()
		deleted, err := f.svc.DeleteCoupons(ctx, &orderdto.CouponBulkDeleteReq{
			ProjectID: f.projectID,
			IDs:       []string{formatID(ids[0]), formatID(ids[1])},
		})
		results <- outcome{deleted: deleted, err: err}
	}
	wg.Add(2)
	go run([]uint64{a.ID, b.ID})
	go run([]uint64{b.ID, a.ID})
	wg.Wait()
	close(results)

	success := 0
	for res := range results {
		if res.err != nil {
			if strings.Contains(strings.ToLower(res.err.Error()), "deadlock") ||
				strings.Contains(res.err.Error(), "40P01") {
				t.Fatalf("反序加锁不得死锁：%v", res.err)
			}
			continue
		}
		if res.deleted != 2 {
			t.Fatalf("成功的一次应删掉 2 张券，实际 %d", res.deleted)
		}
		success++
	}
	if success != 1 {
		t.Fatalf("两张券只该被一个请求删掉，成功次数实际 %d", success)
	}
	if f.exists(t, a.ID) || f.exists(t, b.ID) {
		t.Fatal("成功后两张券都应不存在")
	}
}

func formatID(id uint64) string { return strconv.FormatUint(id, 10) }
