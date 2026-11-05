package rlstest

import (
	"context"
	"errors"
	"testing"

	masterdatamodel "go_wp/internal/module/masterdata/model"
	masterdataservice "go_wp/internal/module/masterdata/service"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	"go_wp/pkg/rls"
)

func TestRLS_VariantSnapshotScope_OwnProjectOnly(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()
	pA, _, aIDs, _ := productScopeFixture(t, db)

	svc := productservice.NewService(productmodel.NewModel(db), nil, masterdataservice.NewService(masterdatamodel.NewModel(db), nil))
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

	if sn.ProductName == "" || sn.ProjectID != pA {
		t.Fatalf("商品名 / 工程应从本工程商品行补齐，实际 name=%q project=%q", sn.ProductName, sn.ProjectID)
	}
}

func TestRLS_VariantSnapshotScope_CrossProjectInvisible(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()
	pA, _, aIDs, bIDs := productScopeFixture(t, db)

	svc := productservice.NewService(productmodel.NewModel(db), nil, masterdataservice.NewService(masterdatamodel.NewModel(db), nil))

	snaps, err := svc.VariantSnapshots(ctx, []string{bIDs["variant"]}, pA)
	if err != nil {
		t.Fatalf("跨工程取快照不应报错，实际 %v", err)
	}
	if len(snaps) != 0 {
		t.Fatalf("拿工程 A 的作用域不该取到工程 B 的变体，实际 %d 条: %+v", len(snaps), snaps)
	}

	mixed, err := svc.VariantSnapshots(ctx, []string{aIDs["variant"], bIDs["variant"]}, pA)
	if err != nil {
		t.Fatalf("混合入参取快照不应报错，实际 %v", err)
	}
	if len(mixed) != 1 || mixed[0].VariantID != aIDs["variant"] {
		t.Fatalf("混合入参应只出工程 A 的变体，实际 %d 条", len(mixed))
	}
}

func TestRLS_VariantSnapshotScope_RejectsMissingScope(t *testing.T) {
	db, _ := rlsFixture(t)
	ctx := context.Background()
	_, _, aIDs, _ := productScopeFixture(t, db)

	svc := productservice.NewService(productmodel.NewModel(db), nil, masterdataservice.NewService(masterdatamodel.NewModel(db), nil))
	for _, bad := range []string{"", "not-a-uuid"} {
		if _, err := svc.VariantSnapshots(ctx, []string{aIDs["variant"]}, bad); !errors.Is(err, rls.ErrInvalidProjectID) {
			t.Errorf("工程 id 为 %q 时应返回 rls.ErrInvalidProjectID，实际 %v", bad, err)
		}
	}
}
