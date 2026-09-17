package unit

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	artifactdto "go_wp/internal/module/artifact/dto"
	artifactenums "go_wp/internal/module/artifact/enums"
	artifactmodel "go_wp/internal/module/artifact/model"
	artifactservice "go_wp/internal/module/artifact/service"
	"go_wp/public/test/support"

	"gorm.io/gorm"
)

// 测试用常量：UUID 必须符合 PG uuid 列格式。
const (
	testArtifactID  = "aaaaaaaa-0000-0000-0000-000000000001"
	testArtifactID2 = "aaaaaaaa-0000-0000-0000-000000000002"
	testArtifactID3 = "aaaaaaaa-0000-0000-0000-000000000003"
	testPageID      = "bbbbbbbb-0000-0000-0000-000000000001"
	testCreatedBy   = "cccccccc-0000-0000-0000-000000000001"
	zeroUUID        = "00000000-0000-0000-0000-000000000000"
	manifestJSON    = `{"canonicalPath":"/index.html","files":{"index.html":"hash-html","manifest.json":"hash-manifest"}}`
	artifactHashV1  = "artifact-hash-a"
	artifactHashV2  = "artifact-hash-b"
)

// newService 建隔离测试库并装配 service。
//
// 表结构来自生产迁移（page_artifacts / content_objects / page_artifact_objects 三表、
// UNIQUE(page_id, version, lang) 与两条外键都由生产 DDL 建立）—— 过去这里是 AutoMigrate
// 建表 + 逐条手抄约束，抄漏一条就让测试跑在一套不存在的约束上：「同一产物的第二个内容对象
// 撞唯一键」这类真实缺陷会被静默放过（本项目已实证过一次）。
//
// TranslateError 对齐生产连接配置（pkg/database）：service 的 mapPersistenceError 依赖
// gorm.ErrDuplicatedKey 把唯一键冲突归一化为 ErrArtifactMismatch，默认连接返回原始
// PG 23505、该分支不命中。
func newService(t *testing.T) *artifactservice.Service {
	t.Helper()
	db := support.NewMigratedPGTestDBTranslateError(t)
	seedArtifactFixtures(t, db)
	return artifactservice.NewService(artifactmodel.NewArtifactModel(db))
}

// testProjectID 夹具工程 ID（pages.project_id → projects(id) 需要真实父行）。
const testProjectID = "dddddddd-0000-0000-0000-000000000001"

// testPageIDs 本包用到的全部页面 ID。
//
// 生产 schema 里 page_artifacts.page_id → pages(id)，每个被写入的 page 都必须先有真实父行；
// 过去这里用 AutoMigrate 自建表、表上没有外键，所以「父行不存在」这件事从没暴露过。
var testPageIDs = []string{
	"bbbbbbbb-0000-0000-0000-000000000001",
	"bbbbbbbb-0000-0000-0000-000000000002",
	"bbbbbbbb-0000-0000-0000-000000000011",
	"bbbbbbbb-0000-0000-0000-000000000012",
	"bbbbbbbb-0000-0000-0000-000000000013",
}

// seedArtifactFixtures 插入用例需要的真实父行（projects + pages）。
func seedArtifactFixtures(t *testing.T, db *gorm.DB) {
	t.Helper()
	support.SeedProjectRow(t, db, testProjectID, "artifact 测试站点")
	for _, pageID := range testPageIDs {
		if err := db.Exec(
			`INSERT INTO pages (id, project_id, kind, content_target_type, draft_path, draft_document, create_time, update_time)
			 VALUES (?, ?, 'home', 'none', ?, '{}'::jsonb, NOW(), NOW())`,
			pageID, testProjectID, "/artifact-test-"+pageID,
		).Error; err != nil {
			t.Fatalf("准备页面行 %s 失败：%v", pageID, err)
		}
	}
}

// validReq 构造合法归档请求。
// 有意不填 Lang：存量调用方（历史代码）不传语言时必须落到站点默认语言，
// 这本身是一条兼容性断言；显式语言场景见 ensure_record_lang_test.go。
func validReq() *artifactdto.RecordReq {
	return &artifactdto.RecordReq{
		ArtifactID:       testArtifactID,
		PageID:           testPageID,
		Version:          1,
		SourceDocument:   json.RawMessage(`{"settings":{},"root":[]}`),
		SchemaVersion:    1,
		SourceHash:       "src-hash",
		BuildInputHash:   "input-hash",
		ArtifactProvider: "local",
		ArtifactKey:      "artifacts/artifact-hash-a",
		ArtifactHash:     artifactHashV1,
		CompilerVersion:  "internal-builder",
		RegistryVersion:  "test-1",
		Manifest:         json.RawMessage(manifestJSON),
		CreatedBy:        "",
	}
}

// mustRecord 断言 Record 成功并返回响应。
func mustRecord(t *testing.T, svc *artifactservice.Service, req *artifactdto.RecordReq) *artifactdto.ArtifactResp {
	t.Helper()
	res, err := svc.Record(context.Background(), req)
	if err != nil {
		t.Fatalf("Record 应成功: %v", err)
	}
	return res
}

// requireErrMsg 断言错误非空且消息完全一致。
func requireErrMsg(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("期望错误 %q，实际无错误", want)
	}
	if got := err.Error(); got != want {
		t.Fatalf("期望错误 %q，实际 %q", want, got)
	}
}

// requireAnyErr 断言错误非空（不校验具体消息，用于"错误未归一化"类断言）。
func requireAnyErr(t *testing.T, err error, desc string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s：期望错误，实际成功", desc)
	}
}

// assertRecent 断言时间戳非零且接近当前时间。
func assertRecent(t *testing.T, ts time.Time) {
	t.Helper()
	if ts.IsZero() {
		t.Fatalf("时间戳为零值")
	}
	if d := time.Since(ts); d < -time.Minute || d > 10*time.Minute {
		t.Fatalf("时间戳不在合理范围: %v (now=%v)", ts, time.Now())
	}
}

// artifactRowCount 统计 page_artifacts 总行数。
func artifactRowCount(t *testing.T, svc *artifactservice.Service) int64 {
	t.Helper()
	var n int64
	if err := svc.Model().DB(context.Background()).Count(&n).Error; err != nil {
		t.Fatalf("统计 page_artifacts 失败: %v", err)
	}
	return n
}

// closureCount 统计某产物的对象闭包行数。
func closureCount(t *testing.T, svc *artifactservice.Service, artifactID string) int64 {
	t.Helper()
	var n int64
	if err := svc.Model().DB(context.Background()).Table("page_artifact_objects").
		Where("artifact_id = ?", artifactID).Count(&n).Error; err != nil {
		t.Fatalf("统计闭包失败: %v", err)
	}
	return n
}

// contentObjectCount 统计 content_objects 总行数。
func contentObjectCount(t *testing.T, svc *artifactservice.Service) int64 {
	t.Helper()
	var n int64
	if err := svc.Model().DB(context.Background()).Table("content_objects").
		Count(&n).Error; err != nil {
		t.Fatalf("统计 content_objects 失败: %v", err)
	}
	return n
}

// contentObjectExists 断言 content_objects 中存在指定 hash。
func contentObjectExists(t *testing.T, svc *artifactservice.Service, hash string) bool {
	t.Helper()
	var n int64
	if err := svc.Model().DB(context.Background()).Table("content_objects").
		Where("content_hash = ?", hash).Count(&n).Error; err != nil {
		t.Fatalf("查询 content_objects 失败: %v", err)
	}
	return n > 0
}

// assertEnums 引用 enums 常量，确保测试编译期即锚定业务消息。
func assertEnums(t *testing.T) {
	t.Helper()
	for _, msg := range []string{
		artifactenums.ErrArtifactNotFound,
		artifactenums.ErrArtifactMismatch,
		artifactenums.ErrInvalidArtifact,
		artifactenums.ErrInvalidParam,
	} {
		if msg == "" {
			t.Fatalf("enums 常量不应为空")
		}
	}
}
