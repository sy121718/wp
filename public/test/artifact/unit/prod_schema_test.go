package unit

import (
	"context"
	"testing"

	artifactenums "go_wp/internal/module/artifact/enums"
)

// TestArtifactRecordHashConflictWithProdConstraint 在"生产 DDL 语义"（补建
// UNIQUE(page_id, version, lang)，迁移 061）下验证：同 (page, version, lang)
// 不同 hash 的 Record 撞唯一键 → mapPersistenceError 归一化为 ErrArtifactMismatch。
func TestArtifactRecordHashConflictWithProdConstraint(t *testing.T) {
	svc := newServiceWithProdConstraint(t)
	ctx := context.Background()

	mustRecord(t, svc, validReq()) // version=1, hash=v1

	reqB := validReq()
	reqB.ArtifactID = "aaaaaaaa-0000-0000-0000-000000000002"
	reqB.ArtifactHash = artifactHashV2

	_, err := svc.Record(ctx, reqB)
	requireErrMsg(t, err, artifactenums.ErrArtifactMismatch)
	// 行数不变。
	if n := artifactRowCount(t, svc); n != 1 {
		t.Fatalf("冲突后行数应仍为 1: %d", n)
	}
}

// TestArtifactRecordSameVersionDifferentHashNoConstraint 揭示 model 缺陷：
// PageArtifactEntity 的 gorm 标签未声明 uniqueIndex，AutoMigrate 生成的 schema
// 缺少生产 DDL 中的 UNIQUE(page_id, version, lang)。纯 AutoMigrate 下同版本同语言
// 不同 hash 的 Record 会插入第二行，破坏"同版本同语言恰好一行"的替换语义。
func TestArtifactRecordSameVersionDifferentHashNoConstraint(t *testing.T) {
	svc := newService(t) // 纯 AutoMigrate，无唯一约束
	ctx := context.Background()

	mustRecord(t, svc, validReq()) // version=1, hash=v1

	reqB := validReq()
	reqB.ArtifactID = "aaaaaaaa-0000-0000-0000-000000000002"
	reqB.ArtifactHash = artifactHashV2

	// 修复语义：model 标签已对齐生产 UNIQUE(page_id, version, lang)，
	// AutoMigrate 亦生成唯一键，同版本同语言第二行插入必须失败。
	if _, err := svc.Record(ctx, reqB); err == nil {
		t.Fatalf("同版本不同 hash 插入应被唯一约束拒绝")
	}
	if n := artifactRowCount(t, svc); n != 1 {
		t.Fatalf("应保持 1 行同版本记录: %d", n)
	}
	// 连锁影响：findByHash 用 First，同 (page, hash) 唯一性依赖约束，
	// 同版本多行时 Detail 返回的行不确定。此处仅验证行数行为。
}
