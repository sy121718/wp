// Package feature 覆盖「块删除保护」的完整源码引用面（审计 ARCH-02）。
//
// 修复前 BlockReferenceChecker 只查主题与页面的一部分：引用只存在于**嵌套块**、
// **未发布模板**、**独立文档模式实例**时，块能被安静地删掉，站点在下一次构建才暴露缺失。
// 本文件用真实 PostgreSQL + 生产迁移建表，逐类引用断言「删不掉」，并断言
// **历史 artifact 不阻断删除**（产物保留由 GC 策略决定，不是源码引用）。
//
// 走的是生产同一条路径：装配层的 routers.BlockReferenceChecker 注入真实 block service，
// 因此断言的是「删除入口的最终行为」，不是某个 model 方法单独跑得对。
package feature

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	artifactmodel "go_wp/internal/module/artifact/model"
	artifactservice "go_wp/internal/module/artifact/service"
	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
	blockmodel "go_wp/internal/module/block/model"
	blockservice "go_wp/internal/module/block/service"
	contenttemplatedto "go_wp/internal/module/contenttemplate/dto"
	contenttemplatemodel "go_wp/internal/module/contenttemplate/model"
	contenttemplateservice "go_wp/internal/module/contenttemplate/service"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	presentationmodel "go_wp/internal/module/presentation/model"
	presentationservice "go_wp/internal/module/presentation/service"
	projectmodel "go_wp/internal/module/project/model"
	projectservice "go_wp/internal/module/project/service"
	pubmodel "go_wp/internal/module/publication/model"
	pubservice "go_wp/internal/module/publication/service"
	"go_wp/internal/routers"

	"go_wp/public/test/support"
)

// refEnv 一套真实的模块链路（与生产装配同形，只把与引用无关的可选端口留空）。
type refEnv struct {
	t         *testing.T
	db        *gorm.DB
	projectID string
	blocks    blockcontract.BlockService
	templates *contenttemplateservice.Service
}

// newRefEnv 装配真实 service 链路并把装配层的引用检查注入 block service。
func newRefEnv(t *testing.T) *refEnv {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "块引用保护站点")

	projects := projectservice.NewService(projectmodel.NewProjectModel(db))
	artifacts := artifactservice.NewService(artifactmodel.NewArtifactModel(db))
	routes := pubservice.NewService(pubmodel.NewPublicationModel(db))
	blocks := blockservice.NewService(blockmodel.NewBlockModel(db), projects)
	pages := pageservice.NewService(pagemodel.NewPageModel(db), artifacts, routes, projects, blocks, nil, nil, nil, nil)
	templates := contenttemplateservice.NewService(contenttemplatemodel.NewModel(db), projects, nil)
	presentations := presentationservice.NewService(presentationmodel.NewModel(db), templates, nil, projects, blocks, routes)

	// 与 assembly_publish.go 同一处接线（ARCH-02）。
	blocks.SetReferenceUsageChecker(routers.BlockReferenceChecker(blocks, pages, projects, presentations, templates))
	blocks.SetStalePropagator(func(context.Context, string) error { return nil })
	return &refEnv{t: t, db: db, projectID: projectID, blocks: blocks, templates: templates}
}

// createBlock 建一个空文档的全局块。
func (e *refEnv) createBlock(name string) string {
	e.t.Helper()
	res, err := e.blocks.Create(context.Background(), &blockdto.CreateReq{
		ProjectID: e.projectID, Name: name, Kind: "block",
		Document: []byte(emptyDoc),
	})
	if err != nil {
		e.t.Fatalf("创建块 %q 失败: %v", name, err)
	}
	return res.ID
}

// globalRefDoc 一份引用 blockID 的文档（顶层 core.globalref；嵌套形态由页面直接 INSERT 覆盖，
// 那条路径不过 AST 校验器）。
func globalRefDoc(blockID string) string {
	return fmt.Sprintf(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"ref","type":"core.globalref","props":{"blockId":"%s"}}]}`, blockID)
}

// emptyDoc 不含任何块引用的合法文档（core.text 占位，避免空 root 被校验器拒绝）。
const emptyDoc = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"txt","type":"core.text","props":{"text":"占位"}}]}`

// insertPage 造一张页面行（只补真实父行所需的最小列）。
func (e *refEnv) insertPage(path, doc string) string {
	e.t.Helper()
	id := uuid.NewString()
	err := e.db.Exec(`INSERT INTO pages
		(id, project_id, kind, content_target_type, draft_path, draft_document, draft_version, stale, create_time, update_time)
		VALUES (?, ?, 'home', 'none', ?, ?::jsonb, 1, false, now(), now())`,
		id, e.projectID, path, doc).Error
	if err != nil {
		e.t.Fatalf("造页面行失败(%s): %v", path, err)
	}
	return id
}

// createTemplate 建一套模板（草稿即引用文档，v1 = 该文档）。
func (e *refEnv) createTemplate(name, doc string) string {
	e.t.Helper()
	res, err := e.templates.Create(context.Background(), &contenttemplatedto.CreateReq{
		ProjectID: e.projectID, Name: name, EntityType: "header", DraftDocument: []byte(doc),
	})
	if err != nil {
		e.t.Fatalf("创建模板 %q 失败: %v", name, err)
	}
	return res.ID
}

// latestVersionID 取模板最新版本行 id（快照的 source_template_version_id 外键）。
func (e *refEnv) latestVersionID(templateID string) string {
	e.t.Helper()
	var id string
	if err := e.db.Raw("SELECT id::text FROM content_template_versions WHERE template_id = ? ORDER BY version DESC LIMIT 1", templateID).Scan(&id).Error; err != nil || id == "" {
		e.t.Fatalf("取模板版本失败(%s): %v", templateID, err)
	}
	return id
}

// expectBlocked 断言删除被拒，且明细里出现期望的类别与（可选）定位片段。
//
// 定位片段同时匹配 Label 与 Detail：修订版本号这类补充定位落在 Detail 上
// （「页面 /x（revision v1）」），只看 Label 会漏掉它。
func (e *refEnv) expectBlocked(blockID, wantKind, wantLabel string) {
	e.t.Helper()
	err := e.blocks.Delete(context.Background(), &blockdto.DeleteReq{ID: blockID})
	if !errors.Is(err, blockcontract.ErrBlockInUse) {
		e.t.Fatalf("期望 ErrBlockInUse，实际 %v", err)
	}
	var inUse *blockcontract.BlockInUseError
	if !errors.As(err, &inUse) {
		e.t.Fatalf("拒绝必须携带引用明细（*BlockInUseError），实际 %T: %v", err, err)
	}
	for _, u := range inUse.Usages {
		if string(u.Kind) == wantKind && (wantLabel == "" || strings.Contains(u.Label+" "+u.Detail, wantLabel)) {
			return
		}
	}
	e.t.Fatalf("引用明细里应包含 %s / %q，实际 %#v", wantKind, wantLabel, inUse.Usages)
}

// TestBlockDeleteGuardCoversNestedBlockDocument 仅存在于其它块文档树里的引用必须拦住删除。
func TestBlockDeleteGuardCoversNestedBlockDocument(t *testing.T) {
	e := newRefEnv(t)
	inner := e.createBlock("内层块")
	if _, err := e.blocks.Create(context.Background(), &blockdto.CreateReq{
		ProjectID: e.projectID, Name: "外层块", Kind: "block",
		Document: []byte(globalRefDoc(inner)),
	}); err != nil {
		t.Fatalf("创建外层块失败: %v", err)
	}
	e.expectBlocked(inner, string(blockcontract.UsageKindBlockDocument), "外层块")
}

// TestBlockDeleteGuardCoversUnpublishedTemplate 未被任何实例使用、也从未构建过的模板同样拦。
func TestBlockDeleteGuardCoversUnpublishedTemplate(t *testing.T) {
	e := newRefEnv(t)
	blockID := e.createBlock("模板引用的块")
	e.createTemplate("未发布页眉模板", globalRefDoc(blockID))
	e.expectBlocked(blockID, string(blockcontract.UsageKindContentTemplate), "未发布页眉模板")
}

// TestBlockDeleteGuardCoversTemplateHistoryVersion 草稿已改掉引用、历史版本仍引用时也要拦。
func TestBlockDeleteGuardCoversTemplateHistoryVersion(t *testing.T) {
	e := newRefEnv(t)
	blockID := e.createBlock("历史版本引用的块")
	tplID := e.createTemplate("会改草稿的模板", globalRefDoc(blockID))
	if _, err := e.templates.Update(context.Background(), &contenttemplatedto.UpdateReq{
		ID: tplID, DraftDocument: []byte(emptyDoc),
	}); err != nil {
		t.Fatalf("更新模板失败: %v", err)
	}
	// 草稿（与最新版本）已经不含引用；v1 仍然引用 —— 版本是源码快照，不是产物，必须拦。
	e.expectBlocked(blockID, string(blockcontract.UsageKindContentTemplate), "会改草稿的模板")
}

// TestBlockDeleteGuardCoversInstanceDocumentAndSnapshot 独立文档模式实例与快照都要拦。
func TestBlockDeleteGuardCoversInstanceDocumentAndSnapshot(t *testing.T) {
	e := newRefEnv(t)

	t.Run("覆盖文档", func(t *testing.T) {
		blockID := e.createBlock("实例覆盖文档引用的块")
		tplID := e.createTemplate("实例底稿模板", emptyDoc)
		versionID := e.latestVersionID(tplID)
		instID := uuid.NewString()
		if err := e.db.Exec(`INSERT INTO presentation_instances
			(id, project_id, entity_type, entity_id, instance_role, url_path, template_id, override_document, render_mode, stale, create_time, update_time)
			VALUES (?, ?, 'product', ?, 'detail', ?, ?, ?::jsonb, 'document', false, now(), now())`,
			instID, e.projectID, uuid.NewString(), "/product/ref-override", tplID, globalRefDoc(blockID)).Error; err != nil {
			t.Fatalf("造实例行失败: %v", err)
		}
		_ = versionID
		e.expectBlocked(blockID, string(blockcontract.UsageKindPresentationInstance), "/product/ref-override")
	})

	t.Run("文档快照", func(t *testing.T) {
		blockID := e.createBlock("实例快照引用的块")
		tplID := e.createTemplate("快照底稿模板", emptyDoc)
		versionID := e.latestVersionID(tplID)
		instID := uuid.NewString()
		if err := e.db.Exec(`INSERT INTO presentation_instances
			(id, project_id, entity_type, entity_id, instance_role, url_path, template_id, override_document, render_mode, stale, create_time, update_time)
			VALUES (?, ?, 'product', ?, 'detail', ?, ?, NULL, 'template', false, now(), now())`,
			instID, e.projectID, uuid.NewString(), "/product/ref-snapshot", tplID).Error; err != nil {
			t.Fatalf("造实例行失败: %v", err)
		}
		if err := e.db.Exec(`INSERT INTO document_snapshots
			(id, presentation_instance_id, source_template_version_id, source_entity_revision_id, document, create_time)
			VALUES (?, ?, ?, ?, ?::jsonb, now())`,
			uuid.NewString(), instID, versionID, uuid.NewString(), globalRefDoc(blockID)).Error; err != nil {
			t.Fatalf("造快照行失败: %v", err)
		}
		e.expectBlocked(blockID, string(blockcontract.UsageKindPresentationInstance), "/product/ref-snapshot")
	})
}

// TestBlockDeleteGuardCoversPageStructureSlots 页面 settings.structure 的其余槽位（slots）
// 是第三个此前漏掉的页面通道：它既不在 root 树里，也不是 header/footer 两个旧字段。
func TestBlockDeleteGuardCoversPageStructureSlots(t *testing.T) {
	e := newRefEnv(t)
	blockID := e.createBlock("槽位绑定的块")
	doc := fmt.Sprintf(`{"settings":{"structure":{"slots":{"announcement":"%s"}}},"root":[]}`, blockID)
	e.insertPage("/slots-page", doc)
	e.expectBlocked(blockID, string(blockcontract.UsageKindPageStructure), "/slots-page")
}

// TestBlockDeleteGuardCoversPageRevisionHistory 只存在于历史修订（当前草稿已不再引用）
// 的引用必须拦住删除 —— 断链只在「回滚到那一版修订」的瞬间暴露，是这批里最难排查的一类。
//
// 与「历史 artifact 不阻断」不冲突：修订是可回滚的**源码历史**（不是编译产物），
// 且有保留期兜底（90 天 / 每页最近 20 版），不会造成永久阻断。
func TestBlockDeleteGuardCoversPageRevisionHistory(t *testing.T) {
	e := newRefEnv(t)
	blockID := e.createBlock("历史修订引用的块")
	pageID := e.insertPage("/revision-page", emptyDoc) // 当前草稿不含引用
	if err := e.db.Exec(`INSERT INTO page_revisions
		(id, page_id, version, draft_path, draft_document, source_hash, create_time)
		VALUES (?, ?, 1, ?, ?::jsonb, 'h', now())`,
		uuid.NewString(), pageID, "/revision-page", globalRefDoc(blockID)).Error; err != nil {
		t.Fatalf("造历史修订失败: %v", err)
	}
	e.expectBlocked(blockID, string(blockcontract.UsageKindPageRevision), "revision v1")
}

// TestBlockDeleteGuardIgnoresPublishedArtifact 历史 artifact 里的引用**不**阻断源码删除。
//
// 验收口径：artifact 是不可变的编译产物字节，它的保留由 GC 策略决定 ——
// 拿它当删除判据会让「产物一旦构建过，源码就永远删不掉」。
func TestBlockDeleteGuardIgnoresPublishedArtifact(t *testing.T) {
	e := newRefEnv(t)
	blockID := e.createBlock("只被产物引用的块")
	pageID := e.insertPage("/artifact-only", emptyDoc)
	if err := e.db.Exec(`INSERT INTO page_artifacts
		(id, page_id, version, lang, source_document, page_document_schema_version, source_hash,
		 build_input_manifest, build_input_hash, artifact_provider, artifact_key, artifact_hash,
		 compiler_version, registry_version, manifest, payload_state, note, created_by, create_time)
		VALUES (?, ?, 1, 'zh-CN', ?::jsonb, 1, 'h', '{}'::jsonb, 'h', 'local', 'k', 'ah', 'v1', 'rv', '{}'::jsonb, 'available', '', ?, now())`,
		uuid.NewString(), pageID, globalRefDoc(blockID), uuid.NewString()).Error; err != nil {
		t.Fatalf("造 page_artifacts 行失败: %v", err)
	}
	if err := e.blocks.Delete(context.Background(), &blockdto.DeleteReq{ID: blockID}); err != nil {
		t.Fatalf("只被历史产物引用时删除必须放行（产物由 GC 策略处理），实际 %v", err)
	}
}
