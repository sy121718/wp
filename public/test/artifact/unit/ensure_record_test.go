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

// TestArtifactEnsureRecordReplaceDedupesSharedHash 覆盖替换路径的重复内容哈希：
// manifest.files 两个文件名共享同一内容哈希（内容重复）时，闭包按内容寻址
// 去重——同一哈希只归档一条闭包 + 一条内容对象，不撞 (artifact_id, content_hash)
// 复合主键，不报错，且 content_object 在事务内写入、无孤儿对象残留。
func TestArtifactEnsureRecordReplaceDedupesSharedHash(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	mustRecord(t, svc, validReq()) // version=1, hash=v1, objects: hash-html/hash-manifest

	reqC := validReq()
	reqC.ArtifactID = testArtifactID2
	reqC.ArtifactHash = "hash-c"
	reqC.Manifest = json.RawMessage(`{"canonicalPath":"/c.html","files":{"x.js":"H3","y.js":"H3"}}`)

	replaced, err := svc.EnsureRecord(ctx, reqC)
	if err != nil {
		t.Fatalf("重复内容哈希应去重成功，不应报错: %v", err)
	}
	// 闭包去重：两个文件名共享同一哈希，只归档一条闭包。
	if n := closureCount(t, svc, replaced.ID); n != 1 {
		t.Fatalf("重复哈希去重后闭包应为 1 条: %d", n)
	}
	// 内容对象只写一次，无孤儿对象（事务内写入，替换失败会一并回滚）。
	if !contentObjectExists(t, svc, "H3") {
		t.Fatalf("内容对象 H3 应存在")
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

// TestArtifactEnsureRecordTransactionAtomic 覆盖替换事务提交后的一致性：
// 元数据、闭包删除、闭包新建、内容对象在同一个事务中，提交后三者一致——
// 替换成功时旧闭包被删除、新闭包（去重）与内容对象（事务内幂等写入）齐全。
func TestArtifactEnsureRecordTransactionAtomic(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	mustRecord(t, svc, validReq())

	// 重复哈希 H4：去重后闭包 1 条、内容对象 1 个，无主键冲突。
	reqD := validReq()
	reqD.ArtifactID = testArtifactID2
	reqD.ArtifactHash = "hash-d"
	reqD.Manifest = json.RawMessage(`{"canonicalPath":"/d.html","files":{"x.js":"H4","y.js":"H4"}}`)

	replaced, err := svc.EnsureRecord(ctx, reqD)
	if err != nil {
		t.Fatalf("替换失败: %v", err)
	}

	// 元数据一致：同一行被替换，hash 更新。
	detail, err := svc.DetailByID(ctx, &artifactdto.DetailByIDReq{ID: testArtifactID})
	if err != nil {
		t.Fatalf("替换后原记录应可查询: %v", err)
	}
	if detail.ArtifactHash != "hash-d" {
		t.Fatalf("替换后 hash 应为 hash-d: %s", detail.ArtifactHash)
	}
	// 闭包去重为 1 条，内容对象存在且无孤儿。
	if n := closureCount(t, svc, replaced.ID); n != 1 {
		t.Fatalf("去重后闭包应为 1 条: %d", n)
	}
	if !contentObjectExists(t, svc, "H4") {
		t.Fatalf("内容对象 H4 应存在")
	}
}
