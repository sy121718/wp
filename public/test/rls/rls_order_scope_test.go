package rlstest

// rls_order_scope_test.go — order 超时取消扫描的工程作用域护栏（DB-009 第三批）。
//
// ListPendingCreatedBefore 原是「全表扫描」：orders 带 FORCE 策略，换非超级角色后它
// **静默返回空集** —— 定时任务照每 15 分钟跑一次，一单都不会被取消，库存被一直占住，
// 而日志里没有任何异常。现在它是「按工程扫描」，工程清单由 model 从 projects 表取
// （工程表是隔离主体、没有 project_id 列、不在迁移 215 的清单里）。
//
// 断言要点：
//   - 每个工程的超时待付款单都能被扫到（各自作用域）；
//   - 别的工程的行不会串进来；
//   - 不带作用域的裸查是 fail closed（这就是改造动机本身）；
//   - 缺工程参数时显式报错，不退化成「不限工程」；
//   - 单工程部署下行为不变。

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
	"go_wp/pkg/rls"
)

// seedOrder 落一行订单（经工程作用域写入：orders 带 FORCE 策略，缺变量的写入会被拒绝）。
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

// orderFixture 造「非超级角色 + 两个工程 + order model」。
func orderFixture(t *testing.T) (*gorm.DB, *ordermodel.OrderModel, string, string) {
	t.Helper()
	db, role := rlsFixture(t)
	resetProjects(t, db, role)
	pA, pB := uuid.NewString(), uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	seedProject(t, db, pB, "工程 B")
	return db, ordermodel.NewOrderModel(db), pA, pB
}

// TestRLS_OrderScope_PendingScanAcrossProjects 每个工程的超时待付款单都要能被扫到。
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

	// 不带作用域的裸查 = 改造前那条「全表扫描」路径：非超级角色下静默 0 行。
	// 这就是本批要消除的退化 —— 排障时它看起来完全正常，只是「没有超时订单」。
	var raw int64
	if err := db.Table("orders").Where("status = ? AND create_time < ?",
		ordermodel.OrderStatusPending, cutoff).Count(&raw).Error; err != nil {
		t.Fatalf("裸查失败: %v", err)
	}
	if raw != 0 {
		t.Fatalf("未设 app.project_id 时应 0 行可见（fail closed），实际 %d 行", raw)
	}
}

// TestRLS_OrderScope_MissingProjectRejected 缺工程参数时显式报错。
func TestRLS_OrderScope_MissingProjectRejected(t *testing.T) {
	_, m, _, _ := orderFixture(t)
	if _, err := m.ListPendingCreatedBefore(context.Background(), "  ", time.Now(), 10); !errors.Is(err, ordermodel.ErrProjectRequired) {
		t.Fatalf("缺工程应 ErrProjectRequired（而不是扫全部或静默空集），实际: %v", err)
	}
}

// TestRLS_OrderScope_ListAllProjectIDsCoversProjects 扇出清单能列出全部工程。
func TestRLS_OrderScope_ListAllProjectIDsCoversProjects(t *testing.T) {
	_, m, pA, pB := orderFixture(t)
	ids, err := m.ListAllProjectIDs(context.Background())
	if err != nil {
		t.Fatalf("取工程清单失败: %v", err)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	if !seen[pA] || !seen[pB] {
		t.Fatalf("工程清单应含两个工程，实际 %v", ids)
	}
}

// TestRLS_OrderScope_SingleProjectUnchanged 单工程部署下扫描行为不变。
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

// TestRLS_OrderScope_LocateByIdAcrossProjects 只带订单 id 的定位跳（后台订单操作）。
//
// orders 带 FORCE 策略：不带作用域的按 id 读取在非超级角色下返回 nil（表现为「订单不存在」），
// 这正是「只给 id 的后台入口」必须逐工程探测出归属的原因。
func TestRLS_OrderScope_LocateByIdAcrossProjects(t *testing.T) {
	db, m, pA, pB := orderFixture(t)
	ctx := context.Background()
	idB := seedOrder(t, db, pB, "LOC-B", ordermodel.OrderStatusPending, time.Hour)

	// 改造前的形态：无作用域直查 —— 静默读不到。
	if e, err := m.GetByID(ctx, idB, ""); err != nil || e != nil {
		t.Fatalf("未设作用域时按 id 读应读不到（fail closed），实际 %v err=%v", e, err)
	}
	// 逐工程探测：拿 B 的作用域能读到，拿 A 的读不到。
	e, err := m.GetByID(ctx, idB, pB)
	if err != nil || e == nil {
		t.Fatalf("拿工程 B 的作用域应读到订单，实际 %v err=%v", e, err)
	}
	if other, gerr := m.GetByID(ctx, idB, pA); gerr != nil || other != nil {
		t.Fatalf("拿工程 A 的作用域不应读到 B 的订单，实际 %v err=%v", other, gerr)
	}
}

// TestRLS_OrderScope_ChangeStatusLocatesProject service 层的定位跳：
// 后台改状态只带订单 id，链路是「事务外逐工程探测 → 事务内 ScopeTx + 加锁读」。
func TestRLS_OrderScope_ChangeStatusLocatesProject(t *testing.T) {
	db, _, pA, pB := orderFixture(t)
	ctx := context.Background()
	svc := orderservice.NewService(
		ordermodel.NewOrderModel(db), nil, ordermodel.NewOrderStatusLogModel(db),
		nil, nil, nil, nil, nil, nil)
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
