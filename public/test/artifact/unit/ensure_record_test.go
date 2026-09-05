package unit

import (
	"context"
	"encoding/json"
	"testing"

	artifactdto "go_wp/internal/module/artifact/dto"
	artifactenums "go_wp/internal/module/artifact/enums"
)

// TestArtifactEnsureRecordNewCreates 覆盖新建路径：无同 hash、无同版本记录。
func TestArtifactEnsureRecordNewCreates(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	res, err := svc.EnsureRecord(ctx, validReq())
	if err != nil {
		t.Fatalf("EnsureRecord 新建失败: %v", err)
	}
	if res.ID != testArtifactID {
		t.Fatalf("ID 不一致: %s", res.ID)
	}
	if n := artifactRowCount(t, svc); n != 1 {
		t.Fatalf("行数应为 1: %d", n)
	}
	if n := closureCount(t, svc, res.ID); n != 2 {
		t.Fatalf("闭包应为 2 条: %d", n)
	}
}

// TestArtifactEnsureRecordExistingHashReturns 覆盖已存在路径：同 page+hash
// 直接返回现记录，不新建、不替换。
func TestArtifactEnsureRecordExistingHashReturns(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	first := mustRecord(t, svc, validReq())
	again, err := svc.EnsureRecord(ctx, validReq())
	if err != nil {
		t.Fatalf("EnsureRecord 已存在应幂等返回: %v", err)
	}
	if again.ID != first.ID {
		t.Fatalf("已存在记录应返回原 ID: %s vs %s", again.ID, first.ID)
	}
	if n := artifactRowCount(t, svc); n != 1 {
		t.Fatalf("已存在路径不应新建行: %d", n)
	}
}

// TestArtifactEnsureRecordReplaceSameVersion 覆盖替换路径：同 (page, version)
// 不同 hash（编译器升级）时替换该行产物指针并重建闭包。
func TestArtifactEnsureRecordReplaceSameVersion(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	first := mustRecord(t, svc, validReq()) // version=1, hash=v1, files: hash-html/hash-manifest

	reqB := validReq()
	reqB.ArtifactID = testArtifactID2
	reqB.ArtifactHash = artifactHashV2
	reqB.SourceHash = "src-hash-b"
	reqB.BuildInputHash = "input-hash-b"
	reqB.ArtifactKey = "artifacts/artifact-hash-b"
	reqB.CompilerVersion = "internal-builder-v2"
	reqB.Manifest = json.RawMessage(`{"canonicalPath":"/v2.html","files":{"bundle.js":"hash-bundle-b"}}`)

	replaced, err := svc.EnsureRecord(ctx, reqB)
	if err != nil {
		t.Fatalf("EnsureRecord 替换失败: %v", err)
	}
	// 同一行被替换：ID 不变、hash/关键字段更新。
	if replaced.ID != first.ID {
		t.Fatalf("替换应保留行 ID: %s vs %s", replaced.ID, first.ID)
	}
	if replaced.ArtifactHash != artifactHashV2 {
		t.Fatalf("替换后 hash 应为 v2: %s", replaced.ArtifactHash)
	}
	if n := artifactRowCount(t, svc); n != 1 {
		t.Fatalf("替换后行数应仍为 1: %d", n)
	}
	// 旧闭包删除、新闭包写入。
	if n := closureCount(t, svc, replaced.ID); n != 1 {
		t.Fatalf("替换后闭包应为 1 条: %d", n)
	}
	// 新内容对象应以 manifest.files 的 value（内容哈希）写入。
	// bug 证据（artifact_record.go:172）：EnsureRecord 替换路径用
	// `for fileHash := range parsedManifest.Files` 遍历的是 map 的 key（文件名），
	// 导致内容对象/闭包实际写入 "bundle.js" 而非 "hash-bundle-b"。
	if !contentObjectExists(t, svc, "hash-bundle-b") {
		t.Fatalf("替换后内容对象应包含 hash-bundle-b（manifest.files 的 value）；当前写入的是文件名（manifest key）—— EnsureRecord 替换路径误用 key 作内容哈希")
	}
	// 旧闭包引用清理：旧文件哈希不再属于该产物。
	if n := closureCount(t, svc, replaced.ID); n != 1 {
		t.Fatalf("旧闭包应被删除: %d", n)
	}
}

func TestArtifactEnsureRecordNilRequest(t *testing.T) {
	svc := newService(t)
	_, err := svc.EnsureRecord(context.Background(), nil)
	requireErrMsg(t, err, artifactenums.ErrInvalidArtifact)
}

// TestArtifactEnsureRecordReplaceInvalidManifest 覆盖替换路径的输入校验：
// 非法 manifest 时返回 ErrInvalidArtifact，且原记录不被破坏。
func TestArtifactEnsureRecordReplaceInvalidManifest(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	mustRecord(t, svc, validReq())

	reqBad := validReq()
	reqBad.ArtifactID = testArtifactID2
	reqBad.ArtifactHash = artifactHashV2
	reqBad.Manifest = json.RawMessage(`not-json`)

	_, err := svc.EnsureRecord(ctx, reqBad)
	requireErrMsg(t, err, artifactenums.ErrInvalidArtifact)
	// 原记录保持原值。
	detail, err := svc.DetailByID(ctx, &artifactdto.DetailByIDReq{ID: testArtifactID})
	if err != nil {
		t.Fatalf("原记录应可查询: %v", err)
	}
	if detail.ArtifactHash != artifactHashV1 {
		t.Fatalf("原记录 hash 不应被改动: %s", detail.ArtifactHash)
	}
}

// TestArtifactEnsureRecordReplaceRollbackAndOrphanObject 覆盖替换路径的事务行为：
// 闭包重建撞复合主键（manifest 内重复文件 hash）→ ReplaceArtifactContent 事务
// 回滚（元数据与旧闭包恢复），但 content_object 在事务外已写入 → 孤立对象残留。
func TestArtifactEnsureRecordReplaceRollbackAndOrphanObject(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	mustRecord(t, svc, validReq()) // version=1, hash=v1, objects: hash-html/hash-manifest

	reqC := validReq()
	reqC.ArtifactID = testArtifactID2
	reqC.ArtifactHash = "hash-c"
	reqC.Manifest = json.RawMessage(`{"canonicalPath":"/c.html","files":{"x.js":"H3","y.js":"H3"}}`)

	_, err := svc.EnsureRecord(ctx, reqC)
	// 预期（修复 bug 后）：两个路径共享同一内容哈希 → 两条闭包引用同一哈希 →
	// 撞 page_artifact_objects 复合主键 → ErrArtifactMismatch 且事务回滚；
	// 同时 ensureContentObject 在事务外已写入 H3 → 孤立对象残留。
	// 当前 bug 行为：替换路径把文件名（manifest key）当内容哈希写入闭包，
	// 两条闭包 (id,"x.js")/(id,"y.js") 不冲突，替换“成功”且无错误（见 bug 报告）。
	requireErrMsg(t, err, artifactenums.ErrArtifactMismatch)

	// 事务回滚：元数据 hash 仍是 v1，行数 1。
	if n := artifactRowCount(t, svc); n != 1 {
		t.Fatalf("替换失败后行数应仍为 1: %d", n)
	}
	detail, err := svc.DetailByID(ctx, &artifactdto.DetailByIDReq{ID: testArtifactID})
	if err != nil {
		t.Fatalf("原记录应可查询: %v", err)
	}
	if detail.ArtifactHash != artifactHashV1 {
		t.Fatalf("回滚后 hash 应恢复 v1: %s", detail.ArtifactHash)
	}
	// 旧闭包恢复。
	if n := closureCount(t, svc, testArtifactID); n != 2 {
		t.Fatalf("回滚后闭包应恢复 2 条: %d", n)
	}
	// bug 证据：ensureContentObject 在事务外执行，H3 已写入 content_objects 成为孤立对象。
	if !contentObjectExists(t, svc, "H3") {
		t.Fatalf("bug 证据缺失：替换失败后 H3 内容对象应残留（事务外写入）")
	}
}

// TestArtifactEnsureRecordReplaceUsesContentHashValue 直接断言替换路径的闭包
// 以 manifest.files 的 value（内容哈希）为准，而非 key（文件名）。
// 当前实现（artifact_record.go:172 `for fileHash := range parsedManifest.Files`）
// 遍历的是 map 的 key，闭包 content_hash 被写入文件名 —— 正确语义应为 value。
func TestArtifactEnsureRecordReplaceUsesContentHashValue(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	mustRecord(t, svc, validReq())
	reqB := validReq()
	reqB.ArtifactID = testArtifactID2
	reqB.ArtifactHash = artifactHashV2
	reqB.Manifest = json.RawMessage(`{"canonicalPath":"/v2.html","files":{"bundle.js":"hash-bundle-b"}}`)
	if _, err := svc.EnsureRecord(ctx, reqB); err != nil {
		t.Fatalf("EnsureRecord 替换失败: %v", err)
	}
	// 期望：内容对象与闭包以 value（hash-bundle-b）为准。
	if !contentObjectExists(t, svc, "hash-bundle-b") {
		t.Fatalf("bug：替换路径应以 manifest.files 的 value 写入内容对象，当前写入的是文件名（key）")
	}
	// 反证：文件名（key）不应被当作内容哈希写入。
	if contentObjectExists(t, svc, "bundle.js") {
		t.Fatalf("bug 证据：文件名 bundle.js 被误写为内容对象哈希")
	}
}

// TestArtifactEnsureRecordTransactionAtomic 覆盖替换事务的原子性：
// 元数据、闭包删除、闭包新建在同一个事务中，任一失败整体回滚。
func TestArtifactEnsureRecordTransactionAtomic(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	mustRecord(t, svc, validReq())

	// 替换路径闭包重建时两条闭包引用同一哈希 H4 → 撞复合主键，
	// 验证 ReplaceArtifactContent 事务内 Update/Delete/Create 整体回滚。
	// （当前 bug：替换路径误用文件名作哈希，两条闭包不冲突，替换“成功”；
	// 修复后此处应得到 ErrArtifactMismatch，见 bug 报告。）
	reqD := validReq()
	reqD.ArtifactID = testArtifactID2
	reqD.ArtifactHash = "hash-d"
	reqD.Manifest = json.RawMessage(`{"canonicalPath":"/d.html","files":{"x.js":"H4","y.js":"H4"}}`)

	_, err := svc.EnsureRecord(ctx, reqD)
	requireErrMsg(t, err, artifactenums.ErrArtifactMismatch)

	// 事务整体回滚：原记录完整。
	detail, err := svc.DetailByID(ctx, &artifactdto.DetailByIDReq{ID: testArtifactID})
	if err != nil {
		t.Fatalf("原记录应可查询: %v", err)
	}
	if detail.ArtifactHash != artifactHashV1 {
		t.Fatalf("原子性破坏：hash 应为 v1: %s", detail.ArtifactHash)
	}
	if detail.Version != 1 {
		t.Fatalf("原子性破坏：version 应为 1: %d", detail.Version)
	}
}
