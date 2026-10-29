package unit

import (
	"context"
	json "go_wp/pkg/jsonx"
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

// TestArtifactEnsureRecordRebuildSameVersion 同草稿版本新内容新增身份，保留旧行与闭包。
func TestArtifactEnsureRecordRebuildSameVersion(t *testing.T) {
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

	rebuilt, err := svc.EnsureRecord(ctx, reqB)
	if err != nil {
		t.Fatalf("EnsureRecord 重建失败: %v", err)
	}
	// 新内容有独立身份，旧产物及闭包不受影响。
	if rebuilt.ID == first.ID {
		t.Fatalf("重建应使用新的行 ID: %s vs %s", rebuilt.ID, first.ID)
	}
	old, err := svc.DetailByID(ctx, &artifactdto.DetailByIDReq{ID: first.ID})
	if err != nil || old.ArtifactHash != first.ArtifactHash || old.ArtifactKey != first.ArtifactKey || closureCount(t, svc, first.ID) != 2 {
		t.Fatalf("重建改写旧产物: %+v %v", old, err)
	}
	if rebuilt.ArtifactHash != artifactHashV2 {
		t.Fatalf("重建后 hash 应为 v2: %s", rebuilt.ArtifactHash)
	}
	if n := artifactRowCount(t, svc); n != 2 {
		t.Fatalf("重建后新旧产物应各一行: %d", n)
	}
	// 新闭包独立写入。
	if n := closureCount(t, svc, rebuilt.ID); n != 1 {
		t.Fatalf("重建后闭包应为 1 条: %d", n)
	}
	// 新内容对象应以 manifest.files 的 value（内容哈希）写入。

	if !contentObjectExists(t, svc, "hash-bundle-b") {
		t.Fatalf("重建后内容对象应包含 hash-bundle-b（manifest.files 的 value）；不应将文件名误当作内容哈希")
	}
	// 新产物的闭包仅归属新内容。
	if n := closureCount(t, svc, rebuilt.ID); n != 1 {
		t.Fatalf("新闭包数量错误: %d", n)
	}
}

func TestArtifactEnsureRecordNilRequest(t *testing.T) {
	svc := newService(t)
	_, err := svc.EnsureRecord(context.Background(), nil)
	requireErrMsg(t, err, artifactenums.ErrInvalidArtifact)
}

// TestArtifactEnsureRecordRebuildInvalidManifest 覆盖重建路径的输入校验：
// 非法 manifest 时返回 ErrInvalidArtifact，且原记录不被破坏。
func TestArtifactEnsureRecordRebuildInvalidManifest(t *testing.T) {
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

// TestArtifactEnsureRecordRebuildDedupesSharedHash 覆盖重建路径的重复内容哈希：
// manifest.files 两个文件名共享同一内容哈希（内容重复）时，闭包按内容寻址
// 去重——同一哈希只归档一条闭包 + 一条内容对象，不撞 (artifact_id, content_hash)
// 复合主键，不报错，且 content_object 在事务内写入、无孤儿对象残留。
func TestArtifactEnsureRecordRebuildDedupesSharedHash(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	mustRecord(t, svc, validReq()) // version=1, hash=v1, objects: hash-html/hash-manifest

	reqC := validReq()
	reqC.ArtifactID = testArtifactID2
	reqC.ArtifactHash = "hash-c"
	reqC.Manifest = json.RawMessage(`{"canonicalPath":"/c.html","files":{"x.js":"H3","y.js":"H3"}}`)

	rebuilt, err := svc.EnsureRecord(ctx, reqC)
	if err != nil {
		t.Fatalf("重复内容哈希应去重成功，不应报错: %v", err)
	}
	// 闭包去重：两个文件名共享同一哈希，只归档一条闭包。
	if n := closureCount(t, svc, rebuilt.ID); n != 1 {
		t.Fatalf("重复哈希去重后闭包应为 1 条: %d", n)
	}
	// 内容对象只写一次，无孤儿对象（事务内写入，重建失败会一并回滚）。
	if !contentObjectExists(t, svc, "H3") {
		t.Fatalf("内容对象 H3 应存在")
	}
}

// TestArtifactEnsureRecordRebuildUsesContentHashValue 直接断言重建路径的闭包
// 以 manifest.files 的 value（内容哈希）为准，而非 key（文件名）。

func TestArtifactEnsureRecordRebuildUsesContentHashValue(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	mustRecord(t, svc, validReq())
	reqB := validReq()
	reqB.ArtifactID = testArtifactID2
	reqB.ArtifactHash = artifactHashV2
	reqB.Manifest = json.RawMessage(`{"canonicalPath":"/v2.html","files":{"bundle.js":"hash-bundle-b"}}`)
	if _, err := svc.EnsureRecord(ctx, reqB); err != nil {
		t.Fatalf("EnsureRecord 重建失败: %v", err)
	}
	// 期望：内容对象与闭包以 value（hash-bundle-b）为准。
	if !contentObjectExists(t, svc, "hash-bundle-b") {
		t.Fatalf("bug：重建路径应以 manifest.files 的 value 写入内容对象，当前写入的是文件名（key）")
	}
	// 反证：文件名（key）不应被当作内容哈希写入。
	if contentObjectExists(t, svc, "bundle.js") {
		t.Fatalf("bug 证据：文件名 bundle.js 被误写为内容对象哈希")
	}
}

// TestArtifactEnsureRecordTransactionAtomic 覆盖重建事务提交后的一致性：
// 元数据、新闭包、内容对象同事务；旧产物闭包保留，新产物闭包按内容去重。
func TestArtifactEnsureRecordTransactionAtomic(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	mustRecord(t, svc, validReq())

	// 重复哈希 H4：去重后闭包 1 条、内容对象 1 个，无主键冲突。
	reqD := validReq()
	reqD.ArtifactID = testArtifactID2
	reqD.ArtifactHash = "hash-d"
	reqD.Manifest = json.RawMessage(`{"canonicalPath":"/d.html","files":{"x.js":"H4","y.js":"H4"}}`)

	rebuilt, err := svc.EnsureRecord(ctx, reqD)
	if err != nil {
		t.Fatalf("重建失败: %v", err)
	}

	// 原记录保持原值，新记录持有新内容。
	detail, err := svc.DetailByID(ctx, &artifactdto.DetailByIDReq{ID: testArtifactID})
	if err != nil {
		t.Fatalf("重建后原记录应可查询: %v", err)
	}
	if detail.ArtifactHash != artifactHashV1 || rebuilt.ArtifactHash != "hash-d" || closureCount(t, svc, testArtifactID) != 2 {
		t.Fatalf("新旧产物事实应独立: old=%s new=%s", detail.ArtifactHash, rebuilt.ArtifactHash)
	}
	// 闭包去重为 1 条，内容对象存在且无孤儿。
	if n := closureCount(t, svc, rebuilt.ID); n != 1 {
		t.Fatalf("去重后闭包应为 1 条: %d", n)
	}
	if !contentObjectExists(t, svc, "H4") {
		t.Fatalf("内容对象 H4 应存在")
	}
}
