package rlstest

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	orderdto "go_wp/internal/module/order/dto"
	ordermodel "go_wp/internal/module/order/model"
	orderservice "go_wp/internal/module/order/service"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	"go_wp/pkg/facing"
	"go_wp/pkg/rls"
)

func seedOrder(t *testing.T, db *gorm.DB, projectID, orderNo, status string, age time.Duration) uint64 {
	t.Helper()
	var id uint64
	now := time.Now().UTC()
	err := rls.InProjectScope(context.Background(), db, projectID, func(tx *gorm.DB) error {
		return tx.Raw(`INSERT INTO orders (project_id, order_no, status, attribution, create_time, update_time)
			VALUES (?, ?, ?, '{}'::jsonb, ?, ?) RETURNING id`,
			projectID, orderNo, status, now.Add(-age), now).Scan(&id).Error
	})
	if err != nil {
		t.Fatalf("写入订单失败: %v", err)
	}
	return id
}

func orderFixture(t *testing.T) (*gorm.DB, *ordermodel.OrderModel, string, string) {
	t.Helper()
	db, role := rlsFixture(t)
	resetProjects(t, db, role)
	pA, pB := uuid.NewString(), uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	seedProject(t, db, pB, "工程 B")
	return db, ordermodel.NewOrderModel(db), pA, pB
}

func TestRLS_OrderScope_PendingScanAcrossProjects(t *testing.T) {
	db, m, pA, pB := orderFixture(t)
	ctx := context.Background()
	cutoff := time.Now().UTC().Add(-30 * time.Minute)
	seedOrder(t, db, pA, "A-OLD", ordermodel.OrderStatusPending, 2*time.Hour)
	seedOrder(t, db, pA, "A-NEW", ordermodel.OrderStatusPending, time.Minute)
	seedOrder(t, db, pA, "A-PAID", ordermodel.OrderStatusPaid, 3*time.Hour)
	seedOrder(t, db, pB, "B-OLD", ordermodel.OrderStatusPending, 2*time.Hour)

	listA, err := m.ListPendingCreatedBefore(ctx, pA, cutoff, 10)
	if err != nil {
		t.Fatalf("按工程扫描失败: %v", err)
	}
	if len(listA) != 1 || listA[0].OrderNo != "A-OLD" {
		t.Fatalf("工程 A 应只扫到 1 单 A-OLD（未超时/非待付款/他工程都不算），实际 %+v", listA)
	}
	listB, err := m.ListPendingCreatedBefore(ctx, pB, cutoff, 10)
	if err != nil {
		t.Fatalf("按工程扫描失败: %v", err)
	}
	if len(listB) != 1 || listB[0].OrderNo != "B-OLD" {
		t.Fatalf("工程 B 应只扫到 1 单 B-OLD，实际 %+v", listB)
	}

	var raw int64
	if err := db.Table("orders").Where("status = ? AND create_time < ?",
		ordermodel.OrderStatusPending, cutoff).Count(&raw).Error; err != nil {
		t.Fatalf("裸查失败: %v", err)
	}
	if raw != 0 {
		t.Fatalf("未设 app.project_id 时应 0 行可见（fail closed），实际 %d 行", raw)
	}
}

func TestRLS_OrderScope_MissingProjectRejected(t *testing.T) {
	_, m, _, _ := orderFixture(t)
	if _, err := m.ListPendingCreatedBefore(context.Background(), "  ", time.Now(), 10); !errors.Is(err, ordermodel.ErrProjectRequired) {
		t.Fatalf("缺工程应 ErrProjectRequired（而不是扫全部或静默空集），实际: %v", err)
	}
}

func TestRLS_ProjectScope_FanoutListCoversProjects(t *testing.T) {
	db, _, pA, pB := orderFixture(t)
	list, err := projectservice.NewService(projectmodel.NewProjectModel(db)).List(context.Background())
	if err != nil {
		t.Fatalf("取工程清单失败: %v", err)
	}
	ids := make([]string, 0, len(list))
	for i := range list {
		ids = append(ids, list[i].ID)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	if !seen[pA] || !seen[pB] {
		t.Fatalf("工程清单应含两个工程，实际 %v", ids)
	}
}

func TestRLS_OrderScope_SingleProjectUnchanged(t *testing.T) {
	db, role := rlsFixture(t)
	resetProjects(t, db, role)
	m := ordermodel.NewOrderModel(db)
	ctx := context.Background()
	pOnly := uuid.NewString()
	seedProject(t, db, pOnly, "唯一工程")
	seedOrder(t, db, pOnly, "ONLY-OLD", ordermodel.OrderStatusPending, 2*time.Hour)

	list, err := m.ListPendingCreatedBefore(ctx, pOnly, time.Now().UTC().Add(-30*time.Minute), 10)
	if err != nil {
		t.Fatalf("单工程下扫描应成功: %v", err)
	}
	if len(list) != 1 || list[0].OrderNo != "ONLY-OLD" {
		t.Fatalf("单工程下应扫到 1 单 ONLY-OLD，实际 %+v", list)
	}
}

func TestRLS_OrderScope_LocateByIdAcrossProjects(t *testing.T) {
	db, m, pA, pB := orderFixture(t)
	ctx := context.Background()
	idB := seedOrder(t, db, pB, "LOC-B", ordermodel.OrderStatusPending, time.Hour)

	if e, err := m.GetByID(ctx, idB, ""); !errors.Is(err, ordermodel.ErrProjectRequired) || e != nil {
		t.Fatalf("缺工程时应显式 ErrProjectRequired，实际 e=%v err=%v", e, err)
	}

	e, err := m.GetByID(ctx, idB, pB)
	if err != nil || e == nil {
		t.Fatalf("拿工程 B 的作用域应读到订单，实际 %v err=%v", e, err)
	}
	if other, gerr := m.GetByID(ctx, idB, pA); gerr != nil || other != nil {
		t.Fatalf("拿工程 A 的作用域不应读到 B 的订单，实际 %v err=%v", other, gerr)
	}
}

func TestRLS_OrderScope_SubTableQueriesRequireProject(t *testing.T) {
	db, _, _, _ := orderFixture(t)
	ctx := context.Background()
	returns := ordermodel.NewReturnModel(db)
	coupons := ordermodel.NewCouponModel(db)

	if _, err := returns.IDsByOrder(ctx, "", 1, ordermodel.ReturnActiveStatuses); !errors.Is(err, ordermodel.ErrProjectRequired) {
		t.Fatalf("IDsByOrder 缺工程应 ErrProjectRequired，实际 %v", err)
	}
	if _, err := coupons.CountRedemptions(ctx, "", 1, nil); !errors.Is(err, ordermodel.ErrProjectRequired) {
		t.Fatalf("CountRedemptions 缺工程应 ErrProjectRequired，实际 %v", err)
	}
	if _, err := coupons.CountRedemptionsTx(ctx, db, "", 1, nil); !errors.Is(err, ordermodel.ErrProjectRequired) {
		t.Fatalf("CountRedemptionsTx 缺工程应 ErrProjectRequired，实际 %v", err)
	}
}

// couponRLSService 装配只含优惠码写路径的 service（工程清单来自真实 projects 表）。
func couponRLSService(t *testing.T, db *gorm.DB) *orderservice.Service {
	t.Helper()
	svc := orderservice.NewService(
		ordermodel.NewOrderModel(db), ordermodel.NewOrderItemModel(db), ordermodel.NewOrderStatusLogModel(db),
		ordermodel.NewCouponModel(db), ordermodel.NewReturnModel(db),
		nil, nil, nil, nil,
	)
	svc.SetProjects(projectservice.NewService(projectmodel.NewProjectModel(db)))
	return svc
}

func seedCouponForWrite(t *testing.T, svc *orderservice.Service, projectID, code string, status int) uint64 {
	t.Helper()
	res, err := svc.CreateCoupon(context.Background(), &orderdto.CouponSaveReq{
		ProjectID: projectID, Code: code, Name: "券 " + code,
		DiscountType: "fixed", DiscountValue: 100, Status: status,
	})
	if err != nil {
		t.Fatalf("建券 %q 失败: %v", code, err)
	}
	return res.ID
}

func couponExists(t *testing.T, db *gorm.DB, projectID string, id uint64) bool {
	t.Helper()
	var n int64
	if err := rls.InProjectScope(context.Background(), db, projectID, func(tx *gorm.DB) error {
		return tx.Table("coupons").Where("project_id = ? AND id = ?", projectID, id).Count(&n).Error
	}); err != nil {
		t.Fatalf("查券失败: %v", err)
	}
	return n > 0
}

func seedCouponRedemption(t *testing.T, db *gorm.DB, projectID string, couponID, orderID uint64) {
	t.Helper()
	err := rls.InProjectScope(context.Background(), db, projectID, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO coupon_redemptions (coupon_id, project_id, code, order_id, order_no, discount_amount, create_time)
			VALUES (?, ?, 'RLS-COUPON', ?, 'NO-RLS', 100, NOW())`,
			couponID, projectID, orderID).Error
	})
	if err != nil {
		t.Fatalf("写入核销记录失败: %v", err)
	}
}

// TestRLS_CouponBulkDeleteAcrossProjects 验证非超级角色 + FORCE RLS 下，
// 跨工程批量删券真正切换作用域：全成、后工程已核销时前工程已删段整体回滚。
func TestRLS_CouponBulkDeleteAcrossProjects(t *testing.T) {
	db, role := rlsFixture(t)
	resetProjects(t, db, role)
	ctx := context.Background()
	pA, pB := uuid.NewString(), uuid.NewString()
	seedProject(t, db, pA, "券工程 A")
	seedProject(t, db, pB, "券工程 B")
	svc := couponRLSService(t, db)

	t.Run("跨工程全成", func(t *testing.T) {
		a := seedCouponForWrite(t, svc, pA, "RLS-DEL-A", 1)
		b := seedCouponForWrite(t, svc, pB, "RLS-DEL-B", 1)
		deleted, err := svc.DeleteCoupons(ctx, &orderdto.CouponBulkDeleteReq{
			IDs: []string{strconv.FormatUint(a, 10), strconv.FormatUint(b, 10)},
		})
		if err != nil {
			t.Fatalf("非超级角色下跨工程批量删除应整批成功：%v", err)
		}
		if deleted != 2 {
			t.Fatalf("应删除 2 张，实际 %d", deleted)
		}
		if couponExists(t, db, pA, a) || couponExists(t, db, pB, b) {
			t.Fatal("整批成功后两张券都应不存在")
		}
	})

	t.Run("后工程已核销使前工程已删段回滚", func(t *testing.T) {
		a := seedCouponForWrite(t, svc, pA, "RLS-ROLL-A", 1)
		b := seedCouponForWrite(t, svc, pB, "RLS-ROLL-B", 1)
		seedCouponRedemption(t, db, pB, b, 880001)

		_, err := svc.DeleteCoupons(ctx, &orderdto.CouponBulkDeleteReq{
			IDs: []string{strconv.FormatUint(a, 10), strconv.FormatUint(b, 10)},
		})
		if err == nil {
			t.Fatal("后工程已核销必须整批拒绝")
		}
		if status, _ := facing.StatusOf(err); status != 409 {
			t.Fatalf("已核销应按 409，实际 %d（%v）", status, err)
		}
		if !couponExists(t, db, pA, a) {
			t.Fatal("前工程（已执行段）的券必须随整批回滚仍在")
		}
		if !couponExists(t, db, pB, b) {
			t.Fatal("后工程被拒的券必须还在")
		}
	})

	t.Run("后工程删除故障使前工程已删段回滚", func(t *testing.T) {
		a := seedCouponForWrite(t, svc, pA, "RLS-DBFAIL-A", 1)
		b := seedCouponForWrite(t, svc, pB, "RLS-DBFAIL-B", 1)

		// 触发器允许本事务内第一次删除，第二次删除时报错：
		// 用事务级 GUC 计数，不依赖工程排序，确定见证「前项已写、后项故障、整批回滚」。
		fn := "coupon_rls_fail_guard"
		trg := fn + "_trg"
		if err := db.Exec("RESET ROLE").Error; err != nil {
			t.Fatalf("切回属主身份失败: %v", err)
		}
		err := db.Exec(`
CREATE OR REPLACE FUNCTION ` + fn + `() RETURNS trigger AS $$
BEGIN
    IF current_setting('coupon_test.deleted', true) = '1' THEN
        RAISE EXCEPTION 'injected coupon delete failure after first write';
    END IF;
    PERFORM set_config('coupon_test.deleted', '1', true);
    RETURN OLD;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER ` + trg + ` BEFORE DELETE ON coupons
FOR EACH ROW EXECUTE FUNCTION ` + fn + `();`).Error
		if rerr := db.Exec("SET ROLE " + role).Error; rerr != nil {
			t.Fatalf("切回非超级角色失败: %v", rerr)
		}
		if err != nil {
			t.Fatalf("装故障触发器失败: %v", err)
		}
		t.Cleanup(func() {
			_ = db.Exec("RESET ROLE").Error
			_ = db.Exec(`DROP TRIGGER IF EXISTS ` + trg + ` ON coupons`).Error
			_ = db.Exec(`DROP FUNCTION IF EXISTS ` + fn + `()`).Error
			_ = db.Exec("SET ROLE " + role).Error
		})

		_, derr := svc.DeleteCoupons(ctx, &orderdto.CouponBulkDeleteReq{
			IDs: []string{strconv.FormatUint(a, 10), strconv.FormatUint(b, 10)},
		})
		if derr == nil {
			t.Fatal("后工程 DB 故障必须整批失败")
		}
		// 必须是由注入的 DB 故障触发，而不是锁内缺失/冲突被误判成故障。
		var blocked facing.BlockedError
		if errors.As(derr, &blocked) || !strings.Contains(derr.Error(), "injected coupon delete failure") {
			t.Fatalf("失败原因应是注入的数据库错误，实际 %T: %v", derr, derr)
		}
		if !couponExists(t, db, pA, a) {
			t.Fatal("前工程（已执行段）的券必须随整批回滚仍在")
		}
		if !couponExists(t, db, pB, b) {
			t.Fatal("后工程失败的券必须还在")
		}
	})

	t.Run("跨工程批量启停只改状态", func(t *testing.T) {
		a := seedCouponForWrite(t, svc, pA, "RLS-ST-A", 1)
		b := seedCouponForWrite(t, svc, pB, "RLS-ST-B", 1)
		updated, err := svc.SetCouponsStatus(ctx, &orderdto.CouponBulkStatusReq{
			IDs: []string{strconv.FormatUint(a, 10), strconv.FormatUint(b, 10)}, Status: 0,
		})
		if err != nil {
			t.Fatalf("非超级角色下跨工程批量停用应成功：%v", err)
		}
		if updated != 2 {
			t.Fatalf("应更新 2 张，实际 %d", updated)
		}
		for _, tc := range []struct {
			name string
			id   uint64
		}{{"券 RLS-ST-A", a}, {"券 RLS-ST-B", b}} {
			got, gerr := svc.GetCoupon(ctx, tc.id)
			if gerr != nil || got == nil {
				t.Fatalf("读券 %d 失败: %v", tc.id, gerr)
			}
			if got.Status != 0 {
				t.Fatalf("启停应把 status 改成 0，实际 %d", got.Status)
			}
			if got.Name != tc.name || got.DiscountValue != 100 || got.DiscountType != "fixed" {
				t.Fatalf("启停只该改 status，实际 %+v", got)
			}
		}
	})
}

func TestRLS_OrderScope_ChangeStatusLocatesProject(t *testing.T) {
	db, _, pA, pB := orderFixture(t)
	ctx := context.Background()
	svc := orderservice.NewService(
		ordermodel.NewOrderModel(db), nil, ordermodel.NewOrderStatusLogModel(db),
		nil, nil, nil, nil, nil, nil)
	svc.SetProjects(projectservice.NewService(projectmodel.NewProjectModel(db)))
	idB := seedOrder(t, db, pB, "CHG-B", ordermodel.OrderStatusPending, time.Hour)

	if err := svc.ChangeStatus(ctx, &orderdto.ChangeStatusReq{
		OrderID: idB, ToStatus: ordermodel.OrderStatusPaid,
	}); err != nil {
		t.Fatalf("后台改状态应成功（逐工程定位 + 事务作用域）: %v", err)
	}
	e, err := ordermodel.NewOrderModel(db).GetByID(ctx, idB, pB)
	if err != nil || e == nil {
		t.Fatalf("回读订单失败: %v", err)
	}
	if e.Status != ordermodel.OrderStatusPaid {
		t.Fatalf("订单状态应为 paid，实际 %s", e.Status)
	}
	_ = pA
}
