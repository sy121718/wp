package feature

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdatamodel "go_wp/internal/module/masterdata/model"
	masterdataservice "go_wp/internal/module/masterdata/service"
	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectcontract "go_wp/internal/module/project/contract"
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/public/test/support"
)

type scanProjectStub struct {
	projectcontract.ProjectService
	rows []projectdto.ProjectResp
}

func (s scanProjectStub) List(context.Context) ([]projectdto.ProjectResp, error) { return s.rows, nil }

func newScanService(t *testing.T, db *gorm.DB, projectID string, changes masterdatacontract.MasterDataService) *productservice.Service {
	t.Helper()
	if changes == nil {
		changes = masterdataservice.NewService(masterdatamodel.NewModel(db), scanProjectStub{
			rows: []projectdto.ProjectResp{{ID: projectID, Name: "发布记录整页读取"}},
		})
	}
	return productservice.NewService(productmodel.NewModel(db), scanProjectStub{
		rows: []projectdto.ProjectResp{{ID: projectID, Name: "发布记录整页读取"}},
	}, changes)
}

// seedScanProducts 造 rows 个商品：sort 与 create_time 同步递增（0..rows-1），
// 生产顺序是 sort ASC 后 create_time DESC，因此扫描位置 i 的商品就是 ids[i]。
func seedScanProducts(t *testing.T, db *gorm.DB, projectID string, rows int) []string {
	t.Helper()
	ids := make([]string, 0, rows)
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < rows; i++ {
		id := uuid.NewString()
		ids = append(ids, id)
		if err := db.Exec(
			`INSERT INTO products (id, project_id, name, slug, sku_code, status, sort, create_time, update_time)
			 VALUES (?, ?, ?, ?, ?, 'published', ?, ?, now())`,
			id, projectID, fmt.Sprintf("批次商品 %04d", i), fmt.Sprintf("scan-%04d", i),
			fmt.Sprintf("SKU-SCAN-%04d", i), i, base.Add(time.Duration(i)*time.Minute)).Error; err != nil {
			t.Fatalf("插入商品 %d 失败：%v", i, err)
		}
	}
	return ids
}

// seedProductTemplate 建一个商品模板：presentation_instances.template_id 是 NOT NULL 外键，
// 夹具必须给真实模板行，不能写 null。
func seedProductTemplate(t *testing.T, db *gorm.DB, projectID, name string) string {
	t.Helper()
	id := uuid.NewString()
	now := time.Now().UTC()
	if err := db.Exec(
		`INSERT INTO content_templates (id, project_id, name, entity_type, draft_document, create_time, update_time)
		 VALUES (?, ?, ?, 'product', '{}'::jsonb, ?, ?)`,
		id, projectID, name, now, now).Error; err != nil {
		t.Fatalf("建商品模板失败：%v", err)
	}
	return id
}

// seedProductTemplateVersion 造一个模板版本行：文档快照必须指向它。
func seedProductTemplateVersion(t *testing.T, db *gorm.DB, templateID string) string {
	t.Helper()
	id := uuid.NewString()
	if err := db.Exec(
		`INSERT INTO content_template_versions (id, template_id, version, document, source_hash, created_by, create_time)
		 VALUES (?, ?, 1, '{}'::jsonb, '', ?, now())`,
		id, templateID, uuid.NewString()).Error; err != nil {
		t.Fatalf("建模板版本失败：%v", err)
	}
	return id
}

// seedInstance 建一个商品发布实例；online 时给上 active_artifact_id（Go 侧 Active 的真源）。
// active_artifact_id 是真实外键（presentation_instances_active_artifact_fk 指向
// presentation_artifacts(id, presentation_instance_id)），所以在线档要造完整链：
// 模板 → 模板版本 → 实例（active NULL）→ 快照 → 产物 → 回填 active，不能塞假 uuid。
func seedInstance(t *testing.T, db *gorm.DB, projectID, entityID string, online bool) {
	t.Helper()
	templateID := seedProductTemplate(t, db, projectID, "商品模板 "+entityID)
	templateVersionID := seedProductTemplateVersion(t, db, templateID)
	instanceID := uuid.NewString()
	if err := db.Exec(
		`INSERT INTO presentation_instances (id, project_id, entity_type, entity_id, instance_role, url_path,
			template_id, stale, published_at, create_time, update_time)
		 VALUES (?, ?, 'product', ?, 'detail', ?, ?, false, now(), now(), now())`,
		instanceID, projectID, entityID, "/products/"+entityID, templateID).Error; err != nil {
		t.Fatalf("建发布实例失败：%v", err)
	}
	if !online {
		return
	}
	snapshotID := uuid.NewString()
	if err := db.Exec(
		`INSERT INTO document_snapshots (id, presentation_instance_id, source_template_version_id,
			source_entity_revision_id, document, create_time) VALUES (?, ?, ?, ?, '{}'::jsonb, now())`,
		snapshotID, instanceID, templateVersionID, uuid.NewString()).Error; err != nil {
		t.Fatalf("建文档快照失败：%v", err)
	}
	artifactID := uuid.NewString()
	if err := db.Exec(
		// 列以 presentation/model 的 ArtifactEntity 为准：build_input_manifest 已被迁移 305 删掉。
		`INSERT INTO presentation_artifacts (id, presentation_instance_id, snapshot_id, version, lang, source_hash,
			build_input_hash, artifact_provider, artifact_key, artifact_hash, compiler_version,
			registry_version, manifest, created_by, create_time)
		 VALUES (?, ?, ?, 1, 'zh-CN', '', '', 'local', '', 'artifact-hash', '', '', '{}'::jsonb, ?, now())`,
		artifactID, instanceID, snapshotID, uuid.NewString()).Error; err != nil {
		t.Fatalf("建产物失败：%v", err)
	}
	if err := db.Exec(`UPDATE presentation_instances SET active_artifact_id = ? WHERE id = ?`,
		artifactID, instanceID).Error; err != nil {
		t.Fatalf("回填激活产物失败：%v", err)
	}
	if err := db.Exec(
		// 时间列在迁移 205 已改名为 update_time（不是 init 里的 updated_at）。
		`INSERT INTO presentation_publications (presentation_id, lang, active_path, artifact_hash, published_at, update_time)
		 VALUES (?, 'zh-CN', ?, '', now(), now())`,
		instanceID, "/products/"+entityID).Error; err != nil {
		t.Fatalf("写发布行失败：%v", err)
	}
}

func TestPublishRecordPageKeepsExactTotalAndClampsPage(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "发布记录整页读取")

	const rows = 500
	ids := seedScanProducts(t, db, projectID, rows)
	for _, id := range ids[:120] {
		seedInstance(t, db, projectID, id, true)
	}

	s := newScanService(t, db, projectID, nil)
	ctx := context.Background()

	first, err := s.QueryPublishRecords(ctx, &productdto.ProductPublishRecordPageReq{ProjectID: projectID, Status: "active", Page: 1})
	if err != nil {
		t.Fatalf("已上线首页失败：%v", err)
	}
	if first.Total != 120 {
		t.Fatalf("有 120 个商品已上线，total 应为 120，实际 %d", first.Total)
	}
	if len(first.Records) != productservice.ProductPublishRecordPageSize {
		t.Fatalf("首页应有 %d 行，实际 %d 行", productservice.ProductPublishRecordPageSize, len(first.Records))
	}
	for _, row := range first.Records {
		if !row.HasInstance || !row.Online {
			t.Fatalf("active 档的行必须是「有实例且已上线」：%+v", row)
		}
	}

	// 越界页夹到末页，且 total 与本页行回答同一批行（同一条语句）。
	last, err := s.QueryPublishRecords(ctx, &productdto.ProductPublishRecordPageReq{ProjectID: projectID, Status: "active", Page: 9999})
	if err != nil {
		t.Fatalf("越界页失败：%v", err)
	}
	if last.Total != 120 || last.Page != 6 {
		t.Fatalf("越界页应夹到第 6 页：total=%d page=%d（期望 120 / 6）", last.Total, last.Page)
	}
	if len(last.Records) != productservice.ProductPublishRecordPageSize {
		t.Fatalf("整除时末页应满 %d 行，实际 %d 行", productservice.ProductPublishRecordPageSize, len(last.Records))
	}
}

func TestPublishRecordPageStatusBucketsAreExhaustive(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "发布记录三档互斥")

	ids := seedScanProducts(t, db, projectID, 10)
	for _, id := range ids[:3] {
		seedInstance(t, db, projectID, id, true)
	}
	for _, id := range ids[3:6] {
		seedInstance(t, db, projectID, id, false)
	}

	s := newScanService(t, db, projectID, nil)
	totals := map[string]int64{}
	for _, status := range []string{"active", "draft", "none"} {
		got, err := s.QueryPublishRecords(context.Background(), &productdto.ProductPublishRecordPageReq{
			ProjectID: projectID, Status: status, Page: 1,
		})
		if err != nil {
			t.Fatalf("%s 档失败：%v", status, err)
		}
		totals[status] = got.Total
	}
	if totals["active"] != 3 || totals["draft"] != 3 || totals["none"] != 4 {
		t.Fatalf("三档应互斥且穷尽 10 个商品：%v", totals)
	}
}

func TestPublishRecordPageEmptySetKeepsPageOne(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "发布记录空档")

	seedScanProducts(t, db, projectID, 30)
	s := newScanService(t, db, projectID, nil)

	got, err := s.QueryPublishRecords(context.Background(), &productdto.ProductPublishRecordPageReq{ProjectID: projectID, Status: "active"})
	if err != nil {
		t.Fatalf("空档查询失败：%v", err)
	}
	if got.Total != 0 || len(got.Records) != 0 || got.Page != 1 {
		t.Fatalf("没有任何已上线商品时应给 total=0、0 行、页 1，实际 total=%d rows=%d page=%d",
			got.Total, len(got.Records), got.Page)
	}
}

func TestPublishRecordPageStaysInProject(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	mine := uuid.NewString()
	other := uuid.NewString()
	support.SeedProjectRow(t, db, mine, "我的工程")
	support.SeedProjectRow(t, db, other, "别人的工程")

	seedScanProducts(t, db, mine, 5)
	foreign := seedScanProducts(t, db, other, 7)
	for _, id := range foreign {
		seedInstance(t, db, other, id, true)
	}

	s := newScanService(t, db, mine, nil)
	got, err := s.QueryPublishRecords(context.Background(), &productdto.ProductPublishRecordPageReq{ProjectID: mine, Status: "active"})
	if err != nil {
		t.Fatalf("工程隔离查询失败：%v", err)
	}
	if got.Total != 0 || len(got.Records) != 0 {
		t.Fatalf("别人的工程有 7 个在线商品，本工程应 0 行，实际 total=%d rows=%d", got.Total, len(got.Records))
	}
}
