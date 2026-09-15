package feature

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
)

// 产物测试的父行 ID（生产外键要求 projects / pages 行真实存在）。
const (
	artifactTestProjectID = "aaaaaaaa-0000-0000-0000-0000000000ff"
	artifactTestPageID    = "bbbbbbbb-0000-0000-0000-000000000001"
)

const recordManifest = `{"files":{"index.html":"hash-html","manifest.json":"hash-manifest"}}`

func newArtifactService(t *testing.T) *artifactservice.Service {
	t.Helper()
	db := support.NewMigratedPGTestDB(t)
	// page_artifacts.page_id → pages(id)、pages.project_id → projects(id) 都是真实外键，
	// 归档前必须补出真实父行。
	support.SeedProjectRow(t, db, artifactTestProjectID, "产物测试站点")
	if err := db.Exec(
		`INSERT INTO pages (id, project_id, kind, content_target_type, draft_path, draft_document, draft_version, stale, create_time, update_time)
		 VALUES (?, ?, 'home', 'none', ?, '{}'::jsonb, 1, false, NOW(), NOW())`,
		artifactTestPageID, artifactTestProjectID, "pages/"+artifactTestPageID+"/draft.json",
	).Error; err != nil {
		t.Fatalf("准备页面行失败：%v", err)
	}
	return artifactservice.NewService(artifactmodel.NewArtifactModel(db))
}

func validRecordReq() *artifactdto.RecordReq {
	return &artifactdto.RecordReq{
		ArtifactID:       "aaaaaaaa-0000-0000-0000-000000000001",
		PageID:           artifactTestPageID,
		Version:          1,
		SourceDocument:   json.RawMessage(`{"settings":{},"root":[]}`),
		SchemaVersion:    1,
		SourceHash:       "src-hash",
		BuildInputHash:   "input-hash",
		ArtifactProvider: "local",
		ArtifactKey:      "artifacts/artifact-hash",
		ArtifactHash:     "artifact-hash",
		CompilerVersion:  "internal-builder",
		RegistryVersion:  "test-1",
		Manifest:         json.RawMessage(recordManifest),
	}
}

func TestArtifactRecordAndDetail(t *testing.T) {
	svc := newArtifactService(t)
	ctx := context.Background()

	first, err := svc.Record(ctx, validRecordReq())
	if err != nil {
		t.Fatalf("归档产物失败: %v", err)
	}
	if first.PayloadState != "available" || first.ArtifactHash != "artifact-hash" || first.CreatedBy == "" {
		t.Fatalf("产物记录字段错误: %+v", first)
	}

	// 同 hash 重复归档：内容寻址幂等，返回同一条记录。
	repeated, err := svc.Record(ctx, validRecordReq())
	if err != nil {
		t.Fatalf("重复归档应幂等: %v", err)
	}
	if repeated.ID != first.ID || !repeated.CreatedAt.Truncate(time.Microsecond).Equal(first.CreatedAt.Truncate(time.Microsecond)) {
		t.Fatalf("重复归档返回了不同记录: %+v vs %+v", repeated, first)
	}

	detail, err := svc.Detail(ctx, &artifactdto.DetailReq{PageID: first.PageID, Hash: first.ArtifactHash})
	if err != nil {
		t.Fatalf("查询产物失败: %v", err)
	}
	if detail.ID != first.ID {
		t.Fatalf("详情与归档不一致: %+v", detail)
	}
}

func TestArtifactRecordClosures(t *testing.T) {
	svc := newArtifactService(t)
	ctx := context.Background()
	recorded, err := svc.Record(ctx, validRecordReq())
	if err != nil {
		t.Fatalf("归档产物失败: %v", err)
	}
	// 闭包表必须包含 manifest.files 的两个文件哈希。
	var closureCount int64
	if err := svc.Model().DB(ctx).Table("page_artifact_objects").
		Where("artifact_id = ?", recorded.ID).
		Count(&closureCount).Error; err != nil || closureCount != 2 {
		t.Fatalf("闭包条目应为 2: count=%d err=%v", closureCount, err)
	}
	var objectCount int64
	if err := svc.Model().DB(ctx).Table("content_objects").Count(&objectCount).Error; err != nil || objectCount != 2 {
		t.Fatalf("共享内容对象应为 2: count=%d err=%v", objectCount, err)
	}
}

func TestArtifactNotFoundMessage(t *testing.T) {
	svc := newArtifactService(t)
	// 生产 page_artifacts.page_id 是 uuid 类型：用合法但不存在于库中的 uuid 表达
	//「缺失产物」（非法字符串会先在类型转换处报错，测不到本意）。
	_, err := svc.Detail(context.Background(), &artifactdto.DetailReq{PageID: "cccccccc-0000-0000-0000-0000000000ff", Hash: "missing"})
	if err == nil || err.Error() != artifactenums.ErrArtifactNotFound {
		t.Fatalf("缺失产物应返回明确错误: %v", err)
	}
}
