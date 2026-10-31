package unit

import (
	"context"
	json "go_wp/pkg/jsonx"
	"testing"
	"time"

	artifactdto "go_wp/internal/module/artifact/dto"
	artifactenums "go_wp/internal/module/artifact/enums"
	artifactmodel "go_wp/internal/module/artifact/model"
	artifactservice "go_wp/internal/module/artifact/service"
	"go_wp/public/test/support"

	"gorm.io/gorm"
)

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

func newService(t *testing.T) *artifactservice.Service {
	t.Helper()
	db := support.NewMigratedPGTestDBTranslateError(t)
	seedArtifactFixtures(t, db)
	return artifactservice.NewService(artifactmodel.NewArtifactModel(db))
}

const testProjectID = "dddddddd-0000-0000-0000-000000000001"

var testPageIDs = []string{
	"bbbbbbbb-0000-0000-0000-000000000001",
	"bbbbbbbb-0000-0000-0000-000000000002",
	"bbbbbbbb-0000-0000-0000-000000000011",
	"bbbbbbbb-0000-0000-0000-000000000012",
	"bbbbbbbb-0000-0000-0000-000000000013",
}

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

func mustRecord(t *testing.T, svc *artifactservice.Service, req *artifactdto.RecordReq) *artifactdto.ArtifactResp {
	t.Helper()
	res, err := svc.Record(context.Background(), req)
	if err != nil {
		t.Fatalf("Record 应成功: %v", err)
	}
	return res
}

func requireErrMsg(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("期望错误 %q，实际无错误", want)
	}
	if got := err.Error(); got != want {
		t.Fatalf("期望错误 %q，实际 %q", want, got)
	}
}

func requireAnyErr(t *testing.T, err error, desc string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s：期望错误，实际成功", desc)
	}
}

func assertRecent(t *testing.T, ts time.Time) {
	t.Helper()
	if ts.IsZero() {
		t.Fatalf("时间戳为零值")
	}
	if d := time.Since(ts); d < -time.Minute || d > 10*time.Minute {
		t.Fatalf("时间戳不在合理范围: %v (now=%v)", ts, time.Now())
	}
}

func artifactRowCount(t *testing.T, svc *artifactservice.Service) int64 {
	t.Helper()
	var n int64
	if err := svc.Model().DB(context.Background()).Count(&n).Error; err != nil {
		t.Fatalf("统计 page_artifacts 失败: %v", err)
	}
	return n
}

func closureCount(t *testing.T, svc *artifactservice.Service, artifactID string) int64 {
	t.Helper()
	var n int64
	if err := svc.Model().DB(context.Background()).Table("page_artifact_objects").
		Where("artifact_id = ?", artifactID).Count(&n).Error; err != nil {
		t.Fatalf("统计闭包失败: %v", err)
	}
	return n
}

func contentObjectCount(t *testing.T, svc *artifactservice.Service) int64 {
	t.Helper()
	var n int64
	if err := svc.Model().DB(context.Background()).Table("content_objects").
		Count(&n).Error; err != nil {
		t.Fatalf("统计 content_objects 失败: %v", err)
	}
	return n
}

func contentObjectExists(t *testing.T, svc *artifactservice.Service, hash string) bool {
	t.Helper()
	var n int64
	if err := svc.Model().DB(context.Background()).Table("content_objects").
		Where("content_hash = ?", hash).Count(&n).Error; err != nil {
		t.Fatalf("查询 content_objects 失败: %v", err)
	}
	return n > 0
}

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
