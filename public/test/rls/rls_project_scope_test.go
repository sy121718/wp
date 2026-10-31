package rlstest

import (
	"context"
	"errors"
	json "go_wp/pkg/jsonx"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
)

func resetProjects(t *testing.T, db *gorm.DB, role string) {
	t.Helper()
	if err := db.Exec("RESET ROLE").Error; err != nil {
		t.Fatalf("切回属主身份失败: %v", err)
	}
	if err := db.Exec("TRUNCATE projects CASCADE").Error; err != nil {
		t.Fatalf("清空工程表失败: %v", err)
	}
	if err := db.Exec("SET ROLE " + role).Error; err != nil {
		t.Fatalf("切回非超级角色失败: %v", err)
	}
}

func seedTheme(t *testing.T, db *gorm.DB, projectID, name, settingsJSON string) string {
	t.Helper()
	id := uuid.NewString()
	now := time.Now().UTC()
	err := projectmodel.NewProjectModel(db).CreateTheme(context.Background(), &projectmodel.ThemeEntity{
		ID: id, ProjectID: projectID, Name: name,
		Settings: json.RawMessage(settingsJSON), CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatalf("写入主题失败: %v", err)
	}
	return id
}

func themeFixture(t *testing.T) (*gorm.DB, *projectservice.Service, string, string) {
	t.Helper()
	db, role := rlsFixture(t)
	resetProjects(t, db, role)
	svc := projectservice.NewService(projectmodel.NewProjectModel(db))
	pA, pB := uuid.NewString(), uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	seedProject(t, db, pB, "工程 B")
	return db, svc, pA, pB
}

func TestRLS_ProjectTheme_LocateAcrossProjects(t *testing.T) {
	db, svc, pA, pB := themeFixture(t)
	ctx := context.Background()
	tA := seedTheme(t, db, pA, "A 的主题", `{"color":"red"}`)
	tB := seedTheme(t, db, pB, "B 的主题", `{"color":"blue"}`)
	tB2 := seedTheme(t, db, pB, "B 的第二套", `{"color":"green"}`)

	gotA, err := svc.GetTheme(ctx, tA)
	if err != nil {
		t.Fatalf("多工程下按 id 取 A 的主题应成功，实际: %v", err)
	}
	if gotA.ProjectID != pA {
		t.Fatalf("定位到的工程应为 A，实际 %s", gotA.ProjectID)
	}
	gotB, err := svc.GetTheme(ctx, tB)
	if err != nil || gotB.ProjectID != pB {
		t.Fatalf("多工程下按 id 取 B 的主题应成功且归属 B，实际 %+v err=%v", gotB, err)
	}

	if err := svc.ActivateTheme(ctx, &projectdto.ThemeActivateReq{ID: tB2}); err != nil {
		t.Fatalf("多工程下激活 B 的主题应成功: %v", err)
	}
	if err := svc.DeleteTheme(ctx, tB); err != nil {
		t.Fatalf("多工程下删除 B 的非激活主题应成功: %v", err)
	}
}

func TestRLS_ProjectTheme_NotUnlimitedScope(t *testing.T) {
	db, _, pA, pB := themeFixture(t)
	ctx := context.Background()
	tA := seedTheme(t, db, pA, "A 的主题", `{}`)
	m := projectmodel.NewProjectModel(db)

	if _, err := m.GetTheme(ctx, pB, tA); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("跨工程按 id 读主题应 ErrRecordNotFound，实际: %v", err)
	}
	if _, err := m.GetTheme(ctx, pA, tA); err != nil {
		t.Fatalf("本工程按 id 读主题应成功，实际: %v", err)
	}
}

func TestRLS_ProjectTheme_SingleProjectUnchanged(t *testing.T) {
	db, role := rlsFixture(t)
	resetProjects(t, db, role)
	svc := projectservice.NewService(projectmodel.NewProjectModel(db))
	ctx := context.Background()
	pOnly := uuid.NewString()
	seedProject(t, db, pOnly, "唯一工程")
	t1 := seedTheme(t, db, pOnly, "T1", `{}`)
	t2 := seedTheme(t, db, pOnly, "T2", `{}`)

	got, err := svc.GetTheme(ctx, t1)
	if err != nil || got.ID != t1 {
		t.Fatalf("单工程下 GetTheme 应成功，实际 %+v err=%v", got, err)
	}
	if err := svc.ActivateTheme(ctx, &projectdto.ThemeActivateReq{ID: t2}); err != nil {
		t.Fatalf("单工程下 ActivateTheme 应成功: %v", err)
	}
	if err := svc.DeleteTheme(ctx, t1); err != nil {
		t.Fatalf("单工程下删除非激活主题应成功: %v", err)
	}
	if _, err := svc.GetTheme(ctx, t1); !errors.Is(err, projectservice.ErrThemeNotFound) {
		t.Fatalf("删除后应查不到，实际: %v", err)
	}
}

func TestRLS_ProjectTheme_ListByBlockIDAcrossProjects(t *testing.T) {
	db, svc, pA, pB := themeFixture(t)
	ctx := context.Background()
	const blk = "blk-theme-0001"
	seedTheme(t, db, pA, "A 页眉", `{"headerBlockId":"`+blk+`"}`)
	seedTheme(t, db, pB, "B 页脚", `{"footerBlockId":"`+blk+`"}`)
	seedTheme(t, db, pB, "B 无关", `{"color":"red"}`)

	themes, err := svc.ListThemesByBlockID(ctx, blk)
	if err != nil {
		t.Fatalf("ListThemesByBlockID 失败: %v", err)
	}
	if len(themes) != 2 {
		t.Fatalf("跨工程应命中 2 套主题（A 1 + B 1），实际 %d —— 扇出漏了某个工程", len(themes))
	}
}
