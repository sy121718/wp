package rlstest

// rls_contenttemplate_scope_test.go — contenttemplate 入口的工程作用域护栏（DB-009 第三批）。
//
// 本模块的旧契约方法靠 resolveProjectID 取「唯一工程」，多工程部署下要么报「需要显式
// 指定工程」，要么（更早的形态）退化成不限工程。本批按语义分成两类处理：
//
//   - **按 id 定位**的入口（Get / Update / ResolveTemplateByID）：模板 id 是主键，
//     service 改为逐工程独立作用域探测定位 —— 多工程下功能不再退化，也不放宽谓词；
//   - **语义要求单一工程**的入口（List / ResolveTemplate / ResolveTemplateByRole）：
//     模板列表或「该类型的当前模板」只可能属于一个工程，扇出会得到互相冲突的多份结果，
//     所以保持**显式失败**（ErrProjectRequired），调用方应改用 *Scoped 变体或补 ProjectID。
//
// 全程非超级角色（rlsFixture + rls.BypassedRole 自检）。

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/builder/core"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplateenums "go_wp/internal/module/contenttemplate/enums"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	contenttemplateservice "go_wp/internal/module/contenttemplate/service"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
)

// ctStubSource 测试桩：只声明实体类型存在，不提供字段解析器（与模块内单测同形）。
type ctStubSource struct{ entityType string }

func (s ctStubSource) EntityType() string       { return s.entityType }
func (s ctStubSource) FieldWhitelist() []string { return nil }

func (s ctStubSource) ResolverFor(_ context.Context, _ string) (core.ContentResolver, error) {
	return nil, errors.New("测试桩不提供字段解析器")
}

// ctPageDoc 合法 Page Document（严格校验要求 settings.layout.mode；缺它会被 ErrDataInvalid 拒绝）。
const ctPageDoc = `{"settings":{"layout":{"mode":"full"}},"root":[]}`

// seedTemplate 落一行模板 + 首个不可变版本（CreateWithVersion 是聚合内原子组合）。
//
// isDefault 必须由调用方指定：迁移 164 的唯一索引是 `(entity_type) WHERE is_default`
// —— **不含 project_id**，所以两个工程各建一个默认 article 模板会撞唯一键。
// 这个索引本身在工程隔离视角下是可疑的（全局唯一 vs 工程内唯一），见本批报告；
// 测试只能按现状只让一个工程带默认标记。
func seedTemplate(t *testing.T, db *gorm.DB, projectID, name string, isDefault bool) string {
	t.Helper()
	id, err := insertTemplate(db, projectID, name, isDefault)
	if err != nil {
		t.Fatalf("写入模板失败: %v", err)
	}
	return id
}

// insertTemplate 直接落一行模板 + 首个版本，返回 (id, error)：
// 默认模板的唯一索引要能看到**失败**，所以不能把断言烧进 helper。
func insertTemplate(db *gorm.DB, projectID, name string, isDefault bool) (string, error) {
	id, verID := uuid.NewString(), uuid.NewString()
	now := time.Now().UTC()
	doc := json.RawMessage(ctPageDoc)
	err := contenttemplatemodel.NewModel(db).CreateWithVersion(context.Background(),
		&contenttemplatemodel.TemplateEntity{
			ID: id, ProjectID: projectID, Name: name, EntityType: "article",
			TemplateRole:  contenttemplatemodel.TemplateRoleDetail,
			DraftDocument: doc, DraftVersion: 1, CurrentVersionID: &verID,
			IsDefault: isDefault, CreatedAt: now, UpdatedAt: now,
		},
		&contenttemplatemodel.VersionEntity{
			ID: verID, TemplateID: id, Version: 1, Document: doc,
			SourceHash: "h", CreatedBy: "00000000-0000-0000-0000-000000000000", CreatedAt: now,
		})
	return id, err
}

// ctFixture 造「非超级角色 + 两个工程 + contenttemplate service」。
func ctFixture(t *testing.T) (*gorm.DB, contenttemplatecontract.ContentTemplateService, string, string) {
	t.Helper()
	db, role := rlsFixture(t)
	resetProjects(t, db, role)
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	reg := core.NewEntitySourceRegistry()
	if err := reg.Register(ctStubSource{entityType: "article"}); err != nil {
		t.Fatalf("注册实体类型失败: %v", err)
	}
	svc := contenttemplateservice.NewService(contenttemplatemodel.NewModel(db), projects, reg)
	pA, pB := uuid.NewString(), uuid.NewString()
	seedProject(t, db, pA, "工程 A")
	seedProject(t, db, pB, "工程 B")
	return db, svc, pA, pB
}

// TestRLS_ContentTemplate_LocateAcrossProjects 按 id 的三个入口在多工程下仍可用。
func TestRLS_ContentTemplate_LocateAcrossProjects(t *testing.T) {
	db, svc, pA, pB := ctFixture(t)
	ctx := context.Background()
	tA := seedTemplate(t, db, pA, "A 模板", true)
	tB := seedTemplate(t, db, pB, "B 模板", false)

	got, err := svc.Get(ctx, &contenttemplatedto.GetReq{ID: tA})
	if err != nil {
		t.Fatalf("多工程下按 id 取模板应成功（逐工程定位），实际: %v", err)
	}
	if got.ID != tA {
		t.Fatalf("应取到模板 %s，实际 %s", tA, got.ID)
	}

	resolved, err := svc.ResolveTemplateByID(ctx, tB)
	if err != nil {
		t.Fatalf("多工程下按 id 解析模板版本应成功，实际: %v", err)
	}
	if resolved.TemplateID != tB {
		t.Fatalf("应解析到模板 %s，实际 %s", tB, resolved.TemplateID)
	}

	if _, err := svc.Update(ctx, &contenttemplatedto.UpdateReq{
		ID: tA, DraftDocument: json.RawMessage(ctPageDoc),
	}); err != nil {
		t.Fatalf("多工程下更新模板应成功（逐工程定位后按 e.ProjectID 作用域写入），实际: %v", err)
	}
}

// TestRLS_ContentTemplate_LocateIsNotUnlimited 定位不放宽谓词：跨工程仍读不到。
func TestRLS_ContentTemplate_LocateIsNotUnlimited(t *testing.T) {
	db, _, pA, pB := ctFixture(t)
	tA := seedTemplate(t, db, pA, "A 模板", true)
	m := contenttemplatemodel.NewModel(db)

	if _, err := m.Get(context.Background(), pB, tA); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("拿 B 的作用域读 A 的模板应 ErrRecordNotFound，实际: %v", err)
	}
}

// TestRLS_ContentTemplate_ListAndResolveRequireExplicitProject
// 语义要求单一工程的入口在多工程下显式失败，不混工程、也不退化成不限工程。
func TestRLS_ContentTemplate_ListAndResolveRequireExplicitProject(t *testing.T) {
	db, svc, pA, pB := ctFixture(t)
	ctx := context.Background()
	seedTemplate(t, db, pA, "A 模板", true)
	seedTemplate(t, db, pB, "B 模板", false)

	if _, err := svc.List(ctx, &contenttemplatedto.ListReq{}); err == nil ||
		!strings.Contains(err.Error(), contenttemplateenums.ErrProjectRequired) {
		t.Fatalf("多工程下列表应显式要求工程（ErrProjectRequired），实际: %v", err)
	}
	if _, err := svc.ResolveTemplate(ctx, "article"); err == nil ||
		!strings.Contains(err.Error(), contenttemplateenums.ErrProjectRequired) {
		t.Fatalf("多工程下解析模板应显式要求工程（ErrProjectRequired），实际: %v", err)
	}
}

// TestRLS_ContentTemplate_SingleProjectUnchanged 单工程部署下旧契约行为不变。
func TestRLS_ContentTemplate_SingleProjectUnchanged(t *testing.T) {
	db, role := rlsFixture(t)
	resetProjects(t, db, role)
	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	reg := core.NewEntitySourceRegistry()
	if err := reg.Register(ctStubSource{entityType: "article"}); err != nil {
		t.Fatalf("注册实体类型失败: %v", err)
	}
	svc := contenttemplateservice.NewService(contenttemplatemodel.NewModel(db), projects, reg)
	ctx := context.Background()
	pOnly := uuid.NewString()
	seedProject(t, db, pOnly, "唯一工程")
	tpl := seedTemplate(t, db, pOnly, "唯一模板", true)

	// 「唯一工程」兜底保留：单工程部署下这些入口照旧工作。
	list, err := svc.List(ctx, &contenttemplatedto.ListReq{})
	if err != nil || len(list) != 1 {
		t.Fatalf("单工程下列表应返回 1 条，实际 %d（err=%v）", len(list), err)
	}
	resolved, err := svc.ResolveTemplate(ctx, "article")
	if err != nil {
		t.Fatalf("单工程下解析模板应成功: %v", err)
	}
	if resolved.TemplateID != tpl {
		t.Fatalf("应解析到 %s，实际 %s", tpl, resolved.TemplateID)
	}
}

// TestRLS_ContentTemplate_DefaultTemplatePerProject 迁移 223 的实测：默认模板的唯一性
// 从「全库唯一」改成「工程内唯一」。
//
// 改前旧索引是 (entity_type) WHERE is_default —— 第二个工程给同类型标默认会撞 23505
// （本文件第一次写这些用例时就是这么红的）；改后两个工程可各有一个默认 article 模板，
// 而同工程同类型再来一个仍然被拒（粒度变细，唯一性没有放松）。
func TestRLS_ContentTemplate_DefaultTemplatePerProject(t *testing.T) {
	db, _, pA, pB := ctFixture(t)

	if _, err := insertTemplate(db, pA, "A 默认", true); err != nil {
		t.Fatalf("工程 A 的首个默认模板应可写入: %v", err)
	}
	if _, err := insertTemplate(db, pB, "B 默认", true); err != nil {
		t.Fatalf("工程 B 也应能各有一个默认模板（迁移 223 之前这里撞唯一索引）: %v", err)
	}
	if _, err := insertTemplate(db, pA, "A 第二个默认", true); err == nil {
		t.Fatal("同工程同实体类型不应允许两个默认模板")
	}
	if _, err := insertTemplate(db, pA, "A 非默认", false); err != nil {
		t.Fatalf("非默认模板不受唯一索引约束: %v", err)
	}

	// 索引层面的实测：旧索引已删、新索引在。
	var n int64
	if err := db.Raw(`SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema()
		AND indexname IN ('idx_content_templates_default_per_project_type', 'idx_content_templates_default_per_type')
		AND indexname = 'idx_content_templates_default_per_type'`).Scan(&n).Error; err != nil {
		t.Fatalf("查询索引失败: %v", err)
	}
	if n != 0 {
		t.Fatal("迁移 223 后旧的 (entity_type) 唯一索引应已删除")
	}
	if err := db.Raw(`SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema()
		AND indexname = 'idx_content_templates_default_per_project_type'`).Scan(&n).Error; err != nil {
		t.Fatalf("查询索引失败: %v", err)
	}
	if n != 1 {
		t.Fatal("迁移 223 后应存在 (project_id, entity_type) WHERE is_default 唯一索引")
	}
}
