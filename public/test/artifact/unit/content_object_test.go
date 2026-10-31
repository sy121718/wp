package unit

import (
	"context"
	json "go_wp/pkg/jsonx"
	"testing"

	artifactdto "go_wp/internal/module/artifact/dto"
	artifactservice "go_wp/internal/module/artifact/service"
)

func locatorOf(t *testing.T, svc *artifactservice.Service, hash string) (provider, objectKey string, ok bool) {
	t.Helper()
	var row struct {
		Provider  string
		ObjectKey string
	}

	if err := svc.Model().DB(context.Background()).Table("content_objects").
		Select("provider", "object_key").
		Where("content_hash = ?", hash).
		Scan(&row).Error; err != nil {
		return "", "", false
	}
	return row.Provider, row.ObjectKey, true
}

func TestArtifactContentObjectFirstWriterWins(t *testing.T) {

	svc := newService(t)
	ctx := context.Background()

	mustRecord(t, svc, validReq())

	reqB := validReq()
	reqB.ArtifactID = testArtifactID2
	reqB.Version = 2
	reqB.ArtifactHash = artifactHashV2
	reqB.ArtifactProvider = "s3"
	reqB.ArtifactKey = "bucket/artifacts/artifact-hash-b"
	reqB.Manifest = json.RawMessage(`{"canonicalPath":"/b.html","files":{"index.html":"hash-html","extra.js":"hash-s3-only"}}`)
	mustRecord(t, svc, reqB)

	provider, key, ok := locatorOf(t, svc, "hash-html")
	if !ok {
		t.Fatalf("hash-html 内容对象应存在")
	}
	if provider != "local" || key != "artifacts/artifact-hash-a/index.html" {
		t.Fatalf("first-writer-wins 被破坏：hash-html 应为首条 (local, artifacts/artifact-hash-a/index.html)，实际 (%s, %s)", provider, key)
	}

	if provider, key, ok = locatorOf(t, svc, "hash-manifest"); !ok || provider != "local" || key != "artifacts/artifact-hash-a/manifest.json" {
		t.Fatalf("hash-manifest 应保持首条定位，实际 (%s, %s) ok=%v", provider, key, ok)
	}

	if provider, key, ok = locatorOf(t, svc, "hash-s3-only"); !ok || provider != "s3" || key != "bucket/artifacts/artifact-hash-b/extra.js" {
		t.Fatalf("新 hash 应以当前引用写入，实际 (%s, %s) ok=%v", provider, key, ok)
	}

	d1, err := svc.DetailByID(ctx, &artifactdto.DetailByIDReq{ID: testArtifactID})
	if err != nil {
		t.Fatalf("查询首条产物失败: %v", err)
	}
	if d1.ArtifactProvider != "local" || d1.ArtifactKey != "artifacts/artifact-hash-a" {
		t.Fatalf("首条产物行 provider/key 应保留自身值: (%s, %s)", d1.ArtifactProvider, d1.ArtifactKey)
	}
	d2, err := svc.DetailByID(ctx, &artifactdto.DetailByIDReq{ID: testArtifactID2})
	if err != nil {
		t.Fatalf("查询第二条产物失败: %v", err)
	}
	if d2.ArtifactProvider != "s3" || d2.ArtifactKey != "bucket/artifacts/artifact-hash-b" {
		t.Fatalf("第二条产物行 provider/key 应保留自身值: (%s, %s)", d2.ArtifactProvider, d2.ArtifactKey)
	}
}

func TestArtifactContentObjectFirstWriterWinsOnRebuild(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	mustRecord(t, svc, validReq())

	reqB := validReq()
	reqB.ArtifactID = testArtifactID2
	reqB.ArtifactHash = artifactHashV2
	reqB.ArtifactProvider = "s3"
	reqB.ArtifactKey = "bucket/artifacts/artifact-hash-b"
	reqB.Manifest = json.RawMessage(`{"canonicalPath":"/b.html","files":{"index.html":"hash-html"}}`)
	if _, err := svc.EnsureRecord(ctx, reqB); err != nil {
		t.Fatalf("EnsureRecord 重建失败: %v", err)
	}

	provider, key, ok := locatorOf(t, svc, "hash-html")
	if !ok {
		t.Fatalf("hash-html 内容对象应存在")
	}
	if provider != "local" || key != "artifacts/artifact-hash-a/index.html" {
		t.Fatalf("重建路径下 first-writer-wins 被破坏：hash-html 应为 (local, artifacts/artifact-hash-a/index.html)，实际 (%s, %s)", provider, key)
	}
}
