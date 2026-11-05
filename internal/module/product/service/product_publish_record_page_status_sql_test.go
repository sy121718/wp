package productservice_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/public/test/support"
	"gorm.io/gorm"
)

// seedProductTemplate 造一个商品模板行：presentation_instances.template_id 是 NOT NULL 外键。
func seedProductTemplate(t *testing.T, db *gorm.DB, projectID, name string) string {
	t.Helper()
	id := uuid.NewString()
	now := time.Now().UTC()
	if err := db.Exec("INSERT INTO content_templates (id, project_id, name, entity_type, draft_document, create_time, update_time)"+
		" VALUES (?, ?, ?, 'product', '{}'::jsonb, ?, ?)", id, projectID, name, now, now).Error; err != nil {
		t.Fatalf("造商品模板失败：%v", err)
	}
	return id
}

// seedProductTemplateVersion 造一个商品模板版本行：实例的 document_snapshot 必须指向它
// （document_snapshots.source_template_version_id 是 NOT NULL 外键）。
func seedProductTemplateVersion(t *testing.T, db *gorm.DB, templateID string) string {
	t.Helper()
	id := uuid.NewString()
	now := time.Now().UTC()
	if err := db.Exec("INSERT INTO content_template_versions (id, template_id, version, document, source_hash, created_by, create_time)"+
		" VALUES (?, ?, 1, '{}'::jsonb, '', ?, ?)", id, templateID, uuid.NewString(), now).Error; err != nil {
		t.Fatalf("造模板版本失败：%v", err)
	}
	return id
}

// seedPublishInstance 用生产迁移表造一条真实发布实例。
// 时间列已由迁移 205 改名为 create_time / update_time（不是 init 里的 created_at / updated_at）。
// 「已上线」只看 active_artifact_id 是否为 NULL；它是真实外键
// （init_builder_schema.sql 的 presentation_instances_active_artifact_fk 指向
// presentation_artifacts(id, presentation_instance_id)），所以要用真实产物行回填，不能塞假 uuid。
// 顺序：模板 → 模板版本 → 实例（active NULL）→ 快照 → 产物 → 回填实例 active。
func seedPublishInstance(t *testing.T, db *gorm.DB, projectID, productID string, active bool) {
	t.Helper()
	now := time.Now().UTC()
	templateID := seedProductTemplate(t, db, projectID, "商品模板 "+productID)
	templateVersionID := seedProductTemplateVersion(t, db, templateID)
	instanceID := uuid.NewString()
	if err := db.Exec("INSERT INTO presentation_instances (id, project_id, entity_type, entity_id, url_path, template_id,"+
		" stale, create_time, update_time) VALUES (?, ?, 'product', ?, ?, ?, false, ?, ?)",
		instanceID, projectID, productID, "/products/"+productID, templateID, now, now).Error; err != nil {
		t.Fatalf("造发布实例失败：%v", err)
	}
	if !active {
		return
	}
	snapshotID := uuid.NewString()
	if err := db.Exec("INSERT INTO document_snapshots (id, presentation_instance_id, source_template_version_id,"+
		" source_entity_revision_id, document, create_time) VALUES (?, ?, ?, ?, '{}'::jsonb, ?)",
		snapshotID, instanceID, templateVersionID, uuid.NewString(), now).Error; err != nil {
		t.Fatalf("造文档快照失败：%v", err)
	}
	artifactID := uuid.NewString()
	// 列以 presentation/model 的 ArtifactEntity 为准：build_input_manifest 已被迁移 305 删掉，lang 是唯一键的一段。
	if err := db.Exec("INSERT INTO presentation_artifacts (id, presentation_instance_id, snapshot_id, version, lang, source_hash,"+
		" build_input_hash, artifact_provider, artifact_key, artifact_hash, compiler_version,"+
		" registry_version, manifest, created_by, create_time) VALUES (?, ?, ?, 1, 'zh-CN', '', '', 'local', '', 'artifact-hash', '', '', '{}'::jsonb, ?, ?)",
		artifactID, instanceID, snapshotID, uuid.NewString(), now).Error; err != nil {
		t.Fatalf("造产物失败：%v", err)
	}
	if err := db.Exec("UPDATE presentation_instances SET active_artifact_id = ? WHERE id = ?", artifactID, instanceID).Error; err != nil {
		t.Fatalf("回填激活产物失败：%v", err)
	}
}

// TestPublishRecordsStatusFilterRunsBeforePagination 守住「状态筛选先于分页」：
// 命中行放在最末位，若先切页再过滤就会漏掉它；total 必须是筛选后的总数。
func TestPublishRecordsStatusFilterRunsBeforePagination(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "发布状态筛选顺序判据")
	seedListProductsBulk(t, db, projectID, 1, 100)

	var tailIDs []string
	if err := db.Raw("SELECT id FROM products WHERE project_id = ? ORDER BY sort DESC LIMIT 1", projectID).
		Scan(&tailIDs).Error; err != nil {
		t.Fatalf("取末位商品失败：%v", err)
	}
	if len(tailIDs) != 1 {
		t.Fatalf("夹具异常：末位商品应有 1 个，实际 %d 个", len(tailIDs))
	}
	liveID := tailIDs[0]
	seedPublishInstance(t, db, projectID, liveID, true)

	s := productservice.NewService(productmodel.NewModel(db), listPageProjectStub{
		rows: []projectdto.ProjectResp{{ID: projectID, Name: "发布状态筛选顺序判据"}},
	}, stubMasterDataChanges{})
	ctx := context.Background()

	t.Run("已上线筛选只留那一个", func(t *testing.T) {
		got, err := s.QueryPublishRecords(ctx, &productdto.ProductPublishRecordPageReq{
			ProjectID: projectID, Status: "active", Page: 1,
		})
		if err != nil {
			t.Fatalf("发布记录读模型失败：%v", err)
		}
		if got.Total != 1 || len(got.Records) != 1 || got.Records[0].ProductID != liveID {
			t.Fatalf("先切页再过滤会漏掉末位已上线商品：total=%d rows=%d（期望 total=1 且命中 %s）",
				got.Total, len(got.Records), liveID)
		}
		if !got.Records[0].Online || !got.Records[0].HasInstance {
			t.Fatalf("已上线行应 HasInstance+Online：%+v", got.Records[0])
		}
		if got.Page != 1 || got.Limit != productservice.ProductPublishRecordPageSize {
			t.Fatalf("分页事实未归一：%+v", got)
		}
		// 这个夹具只有实例与激活产物、没有发布行。发布真源在实例无发布行时仍给一行
		// （presentation/service/presentation_publish_record.go 的 instanceRow 保实例、空语言），
		// 所以这里合法地是一条「空语言事实」：Lang 为空、Online 仍由 active artifact 决定。
		// 但「没有发布就没有发布时间」，model 不许拿人造时间兜底（那会按会话时区输出、
		// 让 Go 的 RFC3339 解析失败）。
		if len(got.Records[0].Langs) != 1 {
			t.Fatalf("没有发布行时仍应有 1 条空语言事实（保实例、保 Online）：%+v", got.Records[0].Langs)
		}
		if got.Records[0].Langs[0].Lang != "" {
			t.Fatalf("没有发布行时语言码应为空，实际 %q", got.Records[0].Langs[0].Lang)
		}
		if !got.Records[0].Online {
			t.Fatalf("有激活产物即应 Online，与实际发布行无关：%+v", got.Records[0])
		}
		if !got.Records[0].PublishedAt.IsZero() {
			t.Fatalf("没有发布行时发布时间必须是零值，实际 %v", got.Records[0].PublishedAt)
		}
		if got.Records[0].ArtifactHash != "" {
			t.Fatalf("没有发布行时不该编造产物 hash，实际 %q", got.Records[0].ArtifactHash)
		}
	})

	t.Run("有发布行时发布时间是真实时刻", func(t *testing.T) {
		publishedAt := time.Date(2026, 10, 2, 3, 4, 5, 0, time.UTC)
		if err := db.Exec("INSERT INTO presentation_publications (presentation_id, lang, active_path, artifact_hash,"+
			" published_at, update_time) SELECT pi.id, 'zh-CN', pi.url_path, '', ?, ? FROM presentation_instances pi"+
			" WHERE pi.project_id = ? AND pi.entity_id = ?",
			publishedAt, publishedAt, projectID, liveID).Error; err != nil {
			t.Fatalf("写发布行失败：%v", err)
		}
		got, err := s.QueryPublishRecords(ctx, &productdto.ProductPublishRecordPageReq{
			ProjectID: projectID, Status: "active", Page: 1,
		})
		if err != nil {
			t.Fatalf("发布记录读模型失败：%v", err)
		}
		if len(got.Records) != 1 || len(got.Records[0].Langs) != 1 {
			t.Fatalf("应有且只有一条语言事实：%+v", got.Records)
		}
		if !got.Records[0].PublishedAt.Equal(publishedAt) {
			t.Fatalf("发布时间应等于发布行里的真实时刻 %v，实际 %v", publishedAt, got.Records[0].PublishedAt)
		}
	})

	t.Run("未建实例筛选按过滤后总数分页", func(t *testing.T) {
		first, err := s.QueryPublishRecords(ctx, &productdto.ProductPublishRecordPageReq{
			ProjectID: projectID, Status: "none", Page: 1,
		})
		if err != nil {
			t.Fatalf("none 首页失败：%v", err)
		}
		if first.Total != 99 || len(first.Records) != 20 || first.Page != 1 {
			t.Fatalf("none 首页事实错误：total=%d rows=%d page=%d（期望 99 / 20 / 1）",
				first.Total, len(first.Records), first.Page)
		}
		for _, row := range first.Records {
			if row.ProductID == liveID {
				t.Fatalf("已上线商品不该出现在未建实例档：%+v", row)
			}
		}
		last, err := s.QueryPublishRecords(ctx, &productdto.ProductPublishRecordPageReq{
			ProjectID: projectID, Status: "none", Page: 999,
		})
		if err != nil {
			t.Fatalf("none 越界页失败：%v", err)
		}
		if last.Total != 99 || last.Page != 5 || len(last.Records) != 19 {
			t.Fatalf("none 越界页应夹到第 5 页 19 行：total=%d page=%d rows=%d",
				last.Total, last.Page, len(last.Records))
		}
		seen := map[string]bool{}
		for _, row := range last.Records {
			if seen[row.ProductID] {
				t.Fatalf("跨页重复商品：%s", row.ProductID)
			}
			seen[row.ProductID] = true
		}
	})
}
