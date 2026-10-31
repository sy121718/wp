package rlstest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	orderdto "go_wp/internal/module/order/dto"
	ordermodel "go_wp/internal/module/order/model"
	orderservice "go_wp/internal/module/order/service"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
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
