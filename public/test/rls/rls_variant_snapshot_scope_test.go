package rlstest

// rls_variant_snapshot_scope_test.go — 变体快照链（VariantSnapshotPort）的工程隔离护栏。
//
// 这条链是 DB-009 里**最后一条**补上作用域的读数路径，它此前是唯一的显式例外：
// 端口签名只有 (ctx, ids)，实现在 model 层走 ListByIDsWithoutScope 裸读。后果是
// order 下单落快照 / cart 加购与结算 / productLivePrice 片段三条消费链在**非超级角色**下
// 会一起静默失效（0 行、不报错）：订单快照为空、加购拿不到变体、价格核对对不出任何结论 ——
// 一条错误日志都没有。
//
// 现在端口必带 projectID，逐条钉住三件事：
//
//  1. 带工程作用域：只拿得到**本工程**的变体（商品名与工程是从 products 行补齐的，
//     补齐那一步也走作用域）；
//  2. 工程不匹配（= 没有该工程的作用域）：跨工程的变体**从返回里消失**，而不是
//     「返回一条商品名为空的快照」。后者会让 order / cart 那道
//     `sn.ProjectID != "" && sn.ProjectID != projectID` 守卫失效（空串不等于任何工程，
//     比较直接放行）—— 那是用一次静默降级换掉一条越权拦截，订单会落一行
//     商品名为空的快照项；
//  3. 缺工程作用域：**当场报 ErrInvalidProjectID**，不退化成静默 0 行。
//
// 全程在非超级角色下跑（rlsFixture 负责 SET ROLE，并用 rls.BypassedRole 自检）：
// 超级用户无条件绕过 RLS，用 root 跑这套断言会全绿而没有一行是真的。
//
// 失败能力：把 model.ListByIDs 里的 rls.InProjectScope 摘掉（退回裸读），
// 第 2 条会读到工程 B 的变体、第 3 条不再报错 —— 两条一起红。

import (
	"context"
	"errors"
	"testing"

	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	"go_wp/pkg/rls"
)

// TestRLS_VariantSnapshotScope_OwnProjectOnly 带工程时只拿得到本工程的变体。
func TestRLS_VariantSnapshotScope_OwnProjectOnly(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()
	pA, _, aIDs, _ := productScopeFixture(t, db)

	svc := productservice.NewService(productmodel.NewModel(db), nil)
	snaps, err := svc.VariantSnapshots(ctx, []string{aIDs["variant"]}, pA)
	if err != nil {
		t.Fatalf("本工程取变体快照应成功，实际 %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("本工程应取到 1 条快照，实际 %d 条", len(snaps))
	}
	sn := snaps[0]
	if sn.VariantID != aIDs["variant"] || sn.ProductID != aIDs["products"] {
		t.Fatalf("快照的变体 / 商品 id 不符: %+v", sn)
	}
	// 商品名与工程来自 products 行的补齐（同样经作用域）：为空说明作用域把商品行挡掉了。
	if sn.ProductName == "" || sn.ProjectID != pA {
		t.Fatalf("商品名 / 工程应从本工程商品行补齐，实际 name=%q project=%q", sn.ProductName, sn.ProjectID)
	}
}

// TestRLS_VariantSnapshotScope_CrossProjectInvisible 不属于本工程的变体不出现在返回里。
//
// 变体表（product_variants）没有工程列、也不在迁移 215 的名单里，所以别的工程的变体 id
// 是读得出来的 —— 隔离只能落在「补齐商品名时被作用域挡掉，进而丢掉这条变体」这一步。
// 断言的是「消失」，不是「存在但字段为空」：后者会让消费方的工程比较失效（见文件头）。
func TestRLS_VariantSnapshotScope_CrossProjectInvisible(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()
	pA, _, aIDs, bIDs := productScopeFixture(t, db)

	svc := productservice.NewService(productmodel.NewModel(db), nil)

	// 拿工程 A 的作用域要工程 B 的变体 → 空（不报错，也不是一条没有商品名的快照）。
	snaps, err := svc.VariantSnapshots(ctx, []string{bIDs["variant"]}, pA)
	if err != nil {
		t.Fatalf("跨工程取快照不应报错，实际 %v", err)
	}
	if len(snaps) != 0 {
		t.Fatalf("拿工程 A 的作用域不该取到工程 B 的变体，实际 %d 条: %+v", len(snaps), snaps)
	}

	// 混合入参：只出本工程那条。消费方按「请求了哪些 / 拿到哪些」做差集，
	// 跨工程与「规格不存在」对调用方是同一种结果（都是买不了）。
	mixed, err := svc.VariantSnapshots(ctx, []string{aIDs["variant"], bIDs["variant"]}, pA)
	if err != nil {
		t.Fatalf("混合入参取快照不应报错，实际 %v", err)
	}
	if len(mixed) != 1 || mixed[0].VariantID != aIDs["variant"] {
		t.Fatalf("混合入参应只出工程 A 的变体，实际 %d 条", len(mixed))
	}
}

// TestRLS_VariantSnapshotScope_RejectsMissingScope 缺工程作用域时显式报错。
//
// 这是这条链与「裸句柄 fail closed」最本质的差别：裸查询换角色后是 0 行、**不报错**，
// 只能靠现象（订单快照为空）倒推；这条链有 rls 把关，漏传工程当场报
// rls.ErrInvalidProjectID —— 调用点的问题在调用点暴露。
func TestRLS_VariantSnapshotScope_RejectsMissingScope(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()
	_, _, aIDs, _ := productScopeFixture(t, db)

	svc := productservice.NewService(productmodel.NewModel(db), nil)
	for _, bad := range []string{"", "not-a-uuid"} {
		if _, err := svc.VariantSnapshots(ctx, []string{aIDs["variant"]}, bad); !errors.Is(err, rls.ErrInvalidProjectID) {
			t.Errorf("工程 id 为 %q 时应返回 rls.ErrInvalidProjectID，实际 %v", bad, err)
		}
	}
}
