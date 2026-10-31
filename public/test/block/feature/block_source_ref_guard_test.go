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

	json "go_wp/pkg/jsonx"
	"go_wp/public/test/support"
)

type refEnv struct {
	t         *testing.T
	db        *gorm.DB
	projectID string
	blocks    blockcontract.BlockService
	templates *contenttemplateservice.Service
}

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

	blocks.SetReferenceUsageChecker(routers.BlockReferenceChecker(blocks, pages, projects, presentations, templates))
	blocks.SetStalePropagator(func(context.Context, string) error { return nil })
	return &refEnv{t: t, db: db, projectID: projectID, blocks: blocks, templates: templates}
}

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

func globalRefDoc(blockID string) string {
	return fmt.Sprintf(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"ref","type":"core.globalref","props":{"blockId":"%s"}}]}`, blockID)
}

const emptyDoc = `{"settings":{"layout":{"mode":"full"}},"root":[{"id":"txt","type":"core.text","props":{"text":"占位"}}]}`

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

func (e *refEnv) latestVersionID(templateID string) string {
	e.t.Helper()
	var id string
	if err := e.db.Raw("SELECT id::text FROM content_template_versions WHERE template_id = ? ORDER BY version DESC LIMIT 1", templateID).Scan(&id).Error; err != nil || id == "" {
		e.t.Fatalf("取模板版本失败(%s): %v", templateID, err)
	}
	return id
}

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

func TestBlockDeleteGuardCoversUnpublishedTemplate(t *testing.T) {
	e := newRefEnv(t)
	blockID := e.createBlock("模板引用的块")
	e.createTemplate("未发布页眉模板", globalRefDoc(blockID))
	e.expectBlocked(blockID, string(blockcontract.UsageKindContentTemplate), "未发布页眉模板")
}

func TestBlockDeleteGuardCoversTemplateHistoryVersion(t *testing.T) {
	e := newRefEnv(t)
	blockID := e.createBlock("历史版本引用的块")
	tplID := e.createTemplate("会改草稿的模板", globalRefDoc(blockID))
	if _, err := e.templates.Update(context.Background(), &contenttemplatedto.UpdateReq{
		ID: tplID, DraftDocument: []byte(emptyDoc),
	}); err != nil {
		t.Fatalf("更新模板失败: %v", err)
	}

	e.expectBlocked(blockID, string(blockcontract.UsageKindContentTemplate), "会改草稿的模板")
}

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

func TestBlockDeleteGuardCoversPageStructureSlots(t *testing.T) {
	e := newRefEnv(t)
	blockID := e.createBlock("槽位绑定的块")
	doc := fmt.Sprintf(`{"settings":{"structure":{"slots":{"announcement":"%s"}}},"root":[]}`, blockID)
	e.insertPage("/slots-page", doc)
	e.expectBlocked(blockID, string(blockcontract.UsageKindPageStructure), "/slots-page")
}

func TestBlockDeleteGuardCoversPageRevisionHistory(t *testing.T) {
	e := newRefEnv(t)
	blockID := e.createBlock("历史修订引用的块")
	pageID := e.insertPage("/revision-page", emptyDoc)
	if err := e.db.Exec(`INSERT INTO page_revisions
		(id, page_id, version, draft_path, draft_document, source_hash, create_time)
		VALUES (?, ?, 1, ?, ?::jsonb, 'h', now())`,
		uuid.NewString(), pageID, "/revision-page", globalRefDoc(blockID)).Error; err != nil {
		t.Fatalf("造历史修订失败: %v", err)
	}
	e.expectBlocked(blockID, string(blockcontract.UsageKindPageRevision), "revision v1")
}

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

func TestBlockDeleteOldForceInputCannotBypassSourceReference(t *testing.T) {
	e := newRefEnv(t)
	inner := e.createBlock("不可强删的内部块")
	_, err := e.blocks.Create(context.Background(), &blockdto.CreateReq{ProjectID: e.projectID, Name: "实际引用者", Document: []byte(fmt.Sprintf(`{"settings":{"layout":{"mode":"full"}},"root":[{"id":"ref","type":"core.globalref","props":{"blockId":%q}}]}`, inner))})
	if err != nil {
		t.Fatal(err)
	}
	var req blockdto.DeleteReq
	if err = json.Unmarshal([]byte(fmt.Sprintf(`{"id":%q,"force":true}`, inner)), &req); err != nil {
		t.Fatal(err)
	}
	err = e.blocks.Delete(context.Background(), &req)
	var inUse *blockcontract.BlockInUseError
	if !errors.Is(err, blockcontract.ErrBlockInUse) || !errors.As(err, &inUse) || len(inUse.Usages) == 0 {
		t.Fatalf("必须拒绝且有实际引用定位: %v", err)
	}
	if _, err = e.blocks.Detail(context.Background(), &blockdto.DetailReq{ProjectID: e.projectID, ID: inner}); err != nil {
		t.Fatalf("拒绝后块必须仍存在: %v", err)
	}
}
