package rlstest

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	blockdto "go_wp/internal/module/block/dto"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
)

func seedScopeBlock(t *testing.T, db *gorm.DB, projectID, name string) string {
	t.Helper()
	id := uuid.NewString()
	err := blockmodel.NewBlockModel(db).Create(context.Background(), &blockmodel.BlockEntity{
		ID: id, ProjectID: projectID, Name: name,
		Kind: blockmodel.KindBlock, Category: blockmodel.DefaultCategory,
		ReuseMode:  blockmodel.ReuseTemplate,
		Document:   []byte(`{"settings":{"layout":{"mode":"full"}},"root":[]}`),
		CreateTime: timeNow(), UpdatedAt: timeNow(),
	})
	if err != nil {
		t.Fatalf("写入块失败（RLS 生效时写入必须经 InProjectScope）: %v", err)
	}
	return id
}

func blockScopeFixture(t *testing.T) (*gorm.DB, *blockservice.Service, string, string) {
	t.Helper()
	db, role := rlsFixture(t)
	resetProjects(t, db, role)
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	svc := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	pA, pB := uuid.NewString(), uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	seedProject(t, db, pB, "工程 B")
	return db, svc, pA, pB
}

func TestRLS_BlockLocate_NoScopeFailsClosed(t *testing.T) {
	db, _, pA, pB := blockScopeFixture(t)
	ctx := context.Background()
	idB := seedScopeBlock(t, db, pB, "B 的块")

	var raw int64
	if err := db.Table("blocks").Where("id = ?", idB).Count(&raw).Error; err != nil {
		t.Fatalf("裸查失败: %v", err)
	}
	if raw != 0 {
		t.Fatalf("未设 app.project_id 时应 0 行可见（fail closed），实际 %d 行", raw)
	}

	if _, err := blockmodel.NewBlockModel(db).GetByID(ctx, idB, ""); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("缺作用域按 id 直查应 ErrRecordNotFound（fail closed），实际 %v", err)
	}

	if _, err := blockmodel.NewBlockModel(db).GetByID(ctx, idB, pA); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("拿工程 A 的作用域读工程 B 的块应 ErrRecordNotFound，实际 %v", err)
	}
	if _, err := blockmodel.NewBlockModel(db).GetByID(ctx, idB, pB); err != nil {
		t.Fatalf("拿工程 B 的作用域读本工程的块应成功，实际 %v", err)
	}
}

func TestRLS_BlockLocate_ServiceFindsOwnerAcrossProjects(t *testing.T) {
	db, svc, pA, pB := blockScopeFixture(t)
	ctx := context.Background()
	idA := seedScopeBlock(t, db, pA, "A 的块")
	idB := seedScopeBlock(t, db, pB, "B 的块")

	name := "B 的块（改后）"
	res, err := svc.Update(ctx, &blockdto.UpdateReq{ID: idB, ExpectedVersion: 1, Name: name})
	if err != nil {
		t.Fatalf("多工程下只带 id 更新应成功（逐工程定位 + 事务作用域）: %v", err)
	}
	if res.ProjectID != pB {
		t.Fatalf("应定位到工程 B，实际 %s", res.ProjectID)
	}
	resA, err := svc.Update(ctx, &blockdto.UpdateReq{ID: idA, ExpectedVersion: 1, Name: "A 的块（改后）"})
	if err != nil || resA.ProjectID != pA {
		t.Fatalf("应定位到工程 A，实际 %+v（err=%v）", resA, err)
	}

	if _, err := svc.Update(ctx, &blockdto.UpdateReq{ID: uuid.NewString(), ExpectedVersion: 1, Name: "x"}); !errors.Is(err, blockservice.ErrNotFound) {
		t.Fatalf("不存在的块应 ErrNotFound，实际 %v", err)
	}
}

func TestRLS_BlockLocate_DetailByIdFindsOwner(t *testing.T) {
	db, svc, pA, pB := blockScopeFixture(t)
	ctx := context.Background()
	idA := seedScopeBlock(t, db, pA, "A 的块")
	idB := seedScopeBlock(t, db, pB, "B 的块")

	for _, tc := range []struct{ id, projectID string }{{idA, pA}, {idB, pB}} {
		got, err := svc.Detail(ctx, &blockdto.DetailReq{ID: tc.id})
		if err != nil {
			t.Fatalf("只带 id 查详情应成功（逐工程定位）: %v", err)
		}
		if got.ProjectID != tc.projectID {
			t.Fatalf("应定位到工程 %s，实际 %s", tc.projectID, got.ProjectID)
		}
	}

	if got, err := svc.Detail(ctx, &blockdto.DetailReq{ProjectID: pA, ID: idA}); err != nil || got.ID != idA {
		t.Fatalf("带工程参数的详情查询应成功，实际 %+v（err=%v）", got, err)
	}
	if _, err := svc.Detail(ctx, &blockdto.DetailReq{ProjectID: pA, ID: idB}); !errors.Is(err, blockservice.ErrNotFound) {
		t.Fatalf("拿工程 A 的作用域查工程 B 的块应 ErrNotFound，实际 %v", err)
	}

	if _, err := svc.Detail(ctx, &blockdto.DetailReq{ID: uuid.NewString()}); !errors.Is(err, blockservice.ErrNotFound) {
		t.Fatalf("不存在的块应 ErrNotFound，实际 %v", err)
	}
}

func TestRLS_BlockLocate_WritesLandOnOwnerRow(t *testing.T) {
	db, svc, _, pB := blockScopeFixture(t)
	ctx := context.Background()
	idB := seedScopeBlock(t, db, pB, "B 的块")

	name := "B 的块（改后）"
	if _, err := svc.Update(ctx, &blockdto.UpdateReq{ID: idB, ExpectedVersion: 1, Name: name}); err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	m := blockmodel.NewBlockModel(db)
	e, err := m.GetByID(ctx, idB, pB)
	if err != nil {
		t.Fatalf("回读更新后的块失败: %v", err)
	}
	if e.Name != name {
		t.Fatalf("块名应已更新为 %q，实际 %q —— 更新静默落到 0 行", name, e.Name)
	}

	if err := svc.Delete(ctx, &blockdto.DeleteReq{ID: idB}); err != nil {
		t.Fatalf("多工程下只带 id 删除应成功: %v", err)
	}
	if _, err := m.GetByID(ctx, idB, pB); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("删除后应查不到，实际 %v", err)
	}
}

func TestRLS_BlockLocate_EmptyProjectTableFailsExplicitly(t *testing.T) {
	db, role := rlsFixture(t)
	resetProjects(t, db, role)
	svc := blockservice.NewService(blockmodel.NewBlockModel(db),
		projectservice.NewService(projectmodel.NewProjectModel(db)))

	_, err := svc.Update(context.Background(), &blockdto.UpdateReq{ID: uuid.NewString(), ExpectedVersion: 1, Name: "x"})
	if !errors.Is(err, blockservice.ErrProjectRequired) {
		t.Fatalf("无工程时定位应显式失败 ErrProjectRequired（而不是静默 ErrNotFound），实际 %v", err)
	}
}
