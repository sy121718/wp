package rlstest

// rls_project_scope_test.go — project(theme) 入口的工程作用域护栏（DB-009 第三批）。
//
// 本批把 soleProjectID（「工程表恰好一个工程时猜作用域，猜不到就退回不限工程」）换成
// **逐工程探测定位**：主题 id 是主键，跨工程不会重复命中，所以逐个工程各设一次作用域
// 探测的结果是确定的。断言要点：
//   - 多工程下按 id 定位仍然成功（旧实现退化为「不限工程」，换角色后是静默「主题不存在」）；
//   - 但也**不是**「不限工程」：拿 A 的作用域读不到 B 的主题；
//   - 单工程部署下行为逐条不变（父批次点名的回归项）；
//   - 按块反查主题覆盖每个工程。
//
// 全程非超级角色（rlsFixture + rls.BypassedRole 自检）。

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	projectdto "go_wp/internal/module/project/dto"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
)

// resetProjects 清空工程表（连带外键子表），让「本用例有几个工程」由用例自己决定。
//
// TRUNCATE ... CASCADE 需要属主身份：SET ROLE 是会话级的，用完立刻切回非超级角色，
// 保证后续断言仍然跑在受限角色下。
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

// seedTheme 经 model 的写入路径落一行主题（themes 带 FORCE 策略，写入承工程作用域）。
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

// themeFixture 造「非超级角色 + 指定数量的工程」。
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

// TestRLS_ProjectTheme_LocateAcrossProjects 多工程下按 id 定位主题仍然可用。
func TestRLS_ProjectTheme_LocateAcrossProjects(t *testing.T) {
	db, svc, pA, pB := themeFixture(t)
	ctx := context.Background()
	tA := seedTheme(t, db, pA, "A 的主题", `{"color":"red"}`)
	tB := seedTheme(t, db, pB, "B 的主题", `{"color":"blue"}`)
	tB2 := seedTheme(t, db, pB, "B 的第二套", `{"color":"green"}`)

	// GetTheme（契约签名里没有工程参数）在多工程下不再报「主题不存在」。
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

	// 激活与删除同样走定位（激活态主题不可删，先删非激活的那套）。
	if err := svc.ActivateTheme(ctx, &projectdto.ThemeActivateReq{ID: tB2}); err != nil {
		t.Fatalf("多工程下激活 B 的主题应成功: %v", err)
	}
	if err := svc.DeleteTheme(ctx, tB); err != nil {
		t.Fatalf("多工程下删除 B 的非激活主题应成功: %v", err)
	}
}

// TestRLS_ProjectTheme_NotUnlimitedScope 定位不是「不限工程」：跨工程仍然读不到。
func TestRLS_ProjectTheme_NotUnlimitedScope(t *testing.T) {
	db, _, pA, pB := themeFixture(t)
	ctx := context.Background()
	tA := seedTheme(t, db, pA, "A 的主题", `{}`)
	m := projectmodel.NewProjectModel(db)

	// 拿 B 的作用域读 A 的行必须读不到 —— 定位是「逐工程各设一次作用域」，
	// 不是「放宽谓词看全部工程」。
	if _, err := m.GetTheme(ctx, pB, tA); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("跨工程按 id 读主题应 ErrRecordNotFound，实际: %v", err)
	}
	if _, err := m.GetTheme(ctx, pA, tA); err != nil {
		t.Fatalf("本工程按 id 读主题应成功，实际: %v", err)
	}
}

// TestRLS_ProjectTheme_SingleProjectUnchanged 单工程部署下行为不变。
func TestRLS_ProjectTheme_SingleProjectUnchanged(t *testing.T) {
	db, role := rlsFixture(t)
	resetProjects(t, db, role)
	svc := projectservice.NewService(projectmodel.NewProjectModel(db))
	ctx := context.Background()
	pOnly := uuid.NewString()
	seedProject(t, db, pOnly, "唯一工程")
	t1 := seedTheme(t, db, pOnly, "T1", `{}`)
	t2 := seedTheme(t, db, pOnly, "T2", `{}`)

	// 改造前靠 soleProjectID（工程唯一）拿作用域；改造后走逐工程枚举 —— 结果必须一致。
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

// TestRLS_ProjectTheme_ListByBlockIDAcrossProjects 按块反查主题覆盖每个工程。
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
