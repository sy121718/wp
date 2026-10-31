package unit

import (
	"context"
	json "go_wp/pkg/jsonx"
	"testing"

	artifactdto "go_wp/internal/module/artifact/dto"
	artifactenums "go_wp/internal/module/artifact/enums"
)

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

func TestArtifactEnsureRecordRebuildSameVersion(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	first := mustRecord(t, svc, validReq())

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

	if n := closureCount(t, svc, rebuilt.ID); n != 1 {
		t.Fatalf("重建后闭包应为 1 条: %d", n)
	}

	if !contentObjectExists(t, svc, "hash-bundle-b") {
		t.Fatalf("重建后内容对象应包含 hash-bundle-b（manifest.files 的 value）；不应将文件名误当作内容哈希")
	}

	if n := closureCount(t, svc, rebuilt.ID); n != 1 {
		t.Fatalf("新闭包数量错误: %d", n)
	}
}

func TestArtifactEnsureRecordNilRequest(t *testing.T) {
	svc := newService(t)
	_, err := svc.EnsureRecord(context.Background(), nil)
	requireErrMsg(t, err, artifactenums.ErrInvalidArtifact)
}

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

	detail, err := svc.DetailByID(ctx, &artifactdto.DetailByIDReq{ID: testArtifactID})
	if err != nil {
		t.Fatalf("原记录应可查询: %v", err)
	}
	if detail.ArtifactHash != artifactHashV1 {
		t.Fatalf("原记录 hash 不应被改动: %s", detail.ArtifactHash)
	}
}

func TestArtifactEnsureRecordRebuildDedupesSharedHash(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	mustRecord(t, svc, validReq())

	reqC := validReq()
	reqC.ArtifactID = testArtifactID2
	reqC.ArtifactHash = "hash-c"
	reqC.Manifest = json.RawMessage(`{"canonicalPath":"/c.html","files":{"x.js":"H3","y.js":"H3"}}`)

	rebuilt, err := svc.EnsureRecord(ctx, reqC)
	if err != nil {
		t.Fatalf("重复内容哈希应去重成功，不应报错: %v", err)
	}

	if n := closureCount(t, svc, rebuilt.ID); n != 1 {
		t.Fatalf("重复哈希去重后闭包应为 1 条: %d", n)
	}

	if !contentObjectExists(t, svc, "H3") {
		t.Fatalf("内容对象 H3 应存在")
	}
}

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

	if !contentObjectExists(t, svc, "hash-bundle-b") {
		t.Fatalf("bug：重建路径应以 manifest.files 的 value 写入内容对象，当前写入的是文件名（key）")
	}

	if contentObjectExists(t, svc, "bundle.js") {
		t.Fatalf("bug 证据：文件名 bundle.js 被误写为内容对象哈希")
	}
}

func TestArtifactEnsureRecordTransactionAtomic(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	mustRecord(t, svc, validReq())

	reqD := validReq()
	reqD.ArtifactID = testArtifactID2
	reqD.ArtifactHash = "hash-d"
	reqD.Manifest = json.RawMessage(`{"canonicalPath":"/d.html","files":{"x.js":"H4","y.js":"H4"}}`)

	rebuilt, err := svc.EnsureRecord(ctx, reqD)
	if err != nil {
		t.Fatalf("重建失败: %v", err)
	}

	detail, err := svc.DetailByID(ctx, &artifactdto.DetailByIDReq{ID: testArtifactID})
	if err != nil {
		t.Fatalf("重建后原记录应可查询: %v", err)
	}
	if detail.ArtifactHash != artifactHashV1 || rebuilt.ArtifactHash != "hash-d" || closureCount(t, svc, testArtifactID) != 2 {
		t.Fatalf("新旧产物事实应独立: old=%s new=%s", detail.ArtifactHash, rebuilt.ArtifactHash)
	}

	if n := closureCount(t, svc, rebuilt.ID); n != 1 {
		t.Fatalf("去重后闭包应为 1 条: %d", n)
	}
	if !contentObjectExists(t, svc, "H4") {
		t.Fatalf("内容对象 H4 应存在")
	}
}
