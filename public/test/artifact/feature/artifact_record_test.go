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

const recordManifest = `{"files":{"index.html":"hash-html","manifest.json":"hash-manifest"}}`

func newArtifactService(t *testing.T) *artifactservice.Service {
	t.Helper()
	db, err := support.NewPGTestDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用，跳过产物链路测试：%v", err)
	}
	for _, statement := range []string{
		`CREATE TABLE page_artifacts (id TEXT PRIMARY KEY, page_id TEXT NOT NULL, version INTEGER NOT NULL, source_document JSON NOT NULL, page_document_schema_version INTEGER NOT NULL, source_hash TEXT NOT NULL, build_input_manifest JSON NOT NULL, build_input_hash TEXT NOT NULL, artifact_provider TEXT NOT NULL, artifact_key TEXT NOT NULL, artifact_hash TEXT NOT NULL, compiler_version TEXT NOT NULL, registry_version TEXT NOT NULL, manifest JSON NOT NULL, payload_state TEXT NOT NULL, payload_deleted_at TIMESTAMPTZ, note TEXT NOT NULL, created_by TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL, UNIQUE(page_id, version), UNIQUE(id, page_id))`,
		`CREATE TABLE content_objects (content_hash TEXT PRIMARY KEY, provider TEXT NOT NULL, object_key TEXT NOT NULL, byte_size INTEGER NOT NULL, created_at TIMESTAMPTZ NOT NULL, deleted_at TIMESTAMPTZ)`,
		`CREATE TABLE page_artifact_objects (artifact_id TEXT NOT NULL, content_hash TEXT NOT NULL, PRIMARY KEY(artifact_id, content_hash))`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("创建测试表失败: %v", err)
		}
	}
	return artifactservice.NewService(artifactmodel.NewArtifactModel(db))
}

func validRecordReq() *artifactdto.RecordReq {
	return &artifactdto.RecordReq{
		ArtifactID:       "aaaaaaaa-0000-0000-0000-000000000001",
		PageID:           "bbbbbbbb-0000-0000-0000-000000000001",
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
	_, err := svc.Detail(context.Background(), &artifactdto.DetailReq{PageID: "missing", Hash: "missing"})
	if err == nil || err.Error() != artifactenums.ErrArtifactNotFound {
		t.Fatalf("缺失产物应返回明确错误: %v", err)
	}
}
