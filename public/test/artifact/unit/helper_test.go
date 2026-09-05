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

// newService 按任务要求用 AutoMigrate 建表后创建 service。
// PG 不可用时 t.Skip（由调用方遵守测试基建约定）。
func newService(t *testing.T) *artifactservice.Service {
	t.Helper()
	db := newMigratedDB(t, false)
	return artifactservice.NewService(artifactmodel.NewArtifactModel(db))
}

// newServiceWithProdConstraint 在 AutoMigrate 之外补建生产 DDL 中的
// UNIQUE(page_id, version) 约束，用于验证生产 schema 语义下 service 的行为。
func newServiceWithProdConstraint(t *testing.T) *artifactservice.Service {
	t.Helper()
	db := newMigratedDB(t, true)
	return artifactservice.NewService(artifactmodel.NewArtifactModel(db))
}

func newMigratedDB(t *testing.T, withProdConstraint bool) *gorm.DB {
	t.Helper()
	// 测试基建：NewPGTestDB 语义的隔离 schema 连接（见 local_pg_test.go）。
	// 有意保留 local_pg 基建而非切回 support.NewPGTestDB：本包 6 处断言
	// ErrArtifactMismatch 的测试依赖 gorm.Config{TranslateError:true}（对齐生产
	// pkg/database），而 support 未开启 TranslateError——唯一约束冲突返回原始
	// PG 23505，mapPersistenceError 的 gorm.ErrDuplicatedKey 分支不命中
	// （实证结论见 local_pg_test.go 头部说明）。
	db, err := newLocalPGDB(t)
	if err != nil {
		t.Skipf("本地 PostgreSQL 不可用，跳过测试：%v", err)
	}
	if err := db.AutoMigrate(
		&artifactmodel.PageArtifactEntity{},
		&artifactmodel.ContentObjectEntity{},
		&artifactmodel.PageArtifactObjectEntity{},
	); err != nil {
		t.Fatalf("AutoMigrate 建表失败: %v", err)
	}
	if withProdConstraint {
		// PageArtifactEntity 的 gorm 标签已声明 uniqueIndex:uk_page_version
		// （page_id, version 复合），AutoMigrate 即生成该约束；此处再以生产 DDL
		// （public/migrations/init_builder_schema.sql:238）同款语句幂等补建，
		// 显式对齐生产 schema 语义。
		if err := db.Exec(
			`CREATE UNIQUE INDEX IF NOT EXISTS uq_page_artifacts_page_version
			 ON page_artifacts (page_id, version)`,
		).Error; err != nil {
			t.Fatalf("补建唯一约束失败: %v", err)
		}
	}
	return db
}

// validReq 构造合法归档请求。
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
