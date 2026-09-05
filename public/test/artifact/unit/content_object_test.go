package unit

import (
	"context"
	"encoding/json"
	"testing"

	artifactdto "go_wp/internal/module/artifact/dto"
	artifactservice "go_wp/internal/module/artifact/service"
)

// locatorOf 查询 content_objects 中指定 hash 的 (provider, object_key) 定位；
// 返回 ok=false 表示该 hash 不存在。
func locatorOf(t *testing.T, svc *artifactservice.Service, hash string) (provider, objectKey string, ok bool) {
	t.Helper()
	var row struct {
		Provider  string
		ObjectKey string
	}
	// 沿用 helper 中既有写法：经 model.DB 切到 content_objects 表；
	// 用 Scan + 显式 Select 而非 First，避免绑定模型（PageArtifactEntity）
	// 的主键/全字段推断干扰（content_objects 无 id 列）。
	if err := svc.Model().DB(context.Background()).Table("content_objects").
		Select("provider", "object_key").
		Where("content_hash = ?", hash).
		Scan(&row).Error; err != nil {
		return "", "", false
	}
	return row.Provider, row.ObjectKey, true
}

// TestArtifactContentObjectFirstWriterWins 固化共享内容对象的 first-writer-wins 语义：
// content_objects 以首个写入该 hash 的 (provider, object_key) 为规范 Locator，
// 后续引用（即使 provider/object_key 不同）不更新、不覆盖 —— 内容寻址下
// 同一 hash 代表同一内容字节，位置应唯一（见 artifact_record.go ensureContentObject 注释）。
// 同时验证：page_artifacts 产物行各自保留自己的 provider/key，不受共享对象影响。
func TestArtifactContentObjectFirstWriterWins(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	// 首条记录：provider=local，files 含 hash-html / hash-manifest。
	mustRecord(t, svc, validReq())

	// 第二条记录：不同 provider（s3）+ 不同 key，但 manifest 复用 hash-html，
	// 并引入新 hash hash-s3-only。Version 换为 2，避免撞 UNIQUE(page_id, version)。
	reqB := validReq()
	reqB.ArtifactID = testArtifactID2
	reqB.Version = 2
	reqB.ArtifactHash = artifactHashV2
	reqB.ArtifactProvider = "s3"
	reqB.ArtifactKey = "bucket/artifacts/artifact-hash-b"
	reqB.Manifest = json.RawMessage(`{"canonicalPath":"/b.html","files":{"index.html":"hash-html","extra.js":"hash-s3-only"}}`)
	mustRecord(t, svc, reqB)

	// 共享 hash-html：保持首条记录的 local 定位，不被 s3 覆盖。
	provider, key, ok := locatorOf(t, svc, "hash-html")
	if !ok {
		t.Fatalf("hash-html 内容对象应存在")
	}
	if provider != "local" || key != "artifacts/artifact-hash-a" {
		t.Fatalf("first-writer-wins 被破坏：hash-html 应为首条 (local, artifacts/artifact-hash-a)，实际 (%s, %s)", provider, key)
	}

	// 首条独占 hash-manifest 保持原样。
	if provider, key, ok = locatorOf(t, svc, "hash-manifest"); !ok || provider != "local" || key != "artifacts/artifact-hash-a" {
		t.Fatalf("hash-manifest 应保持首条定位，实际 (%s, %s) ok=%v", provider, key, ok)
	}

	// 新 hash 以第二条的 provider 写入。
	if provider, key, ok = locatorOf(t, svc, "hash-s3-only"); !ok || provider != "s3" || key != "bucket/artifacts/artifact-hash-b" {
		t.Fatalf("新 hash 应以当前引用写入，实际 (%s, %s) ok=%v", provider, key, ok)
	}

	// 产物行各自的 provider/key 独立保留。
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

// TestArtifactContentObjectFirstWriterWinsOnReplace 固化替换路径下同样的语义：
// EnsureRecord 同版本替换（不同 hash/provider）时，与新闭包共享的旧 hash
// 仍保持首条定位，不被替换请求的 provider 覆盖。
func TestArtifactContentObjectFirstWriterWinsOnReplace(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	mustRecord(t, svc, validReq()) // v1: hash=artifact-hash-a, files: hash-html/hash-manifest, provider=local

	// 替换：同 version=1，hash 变为 v2，provider 变为 s3，但 manifest 复用 hash-html。
	reqB := validReq()
	reqB.ArtifactID = testArtifactID2
	reqB.ArtifactHash = artifactHashV2
	reqB.ArtifactProvider = "s3"
	reqB.ArtifactKey = "bucket/artifacts/artifact-hash-b"
	reqB.Manifest = json.RawMessage(`{"canonicalPath":"/b.html","files":{"index.html":"hash-html"}}`)
	if _, err := svc.EnsureRecord(ctx, reqB); err != nil {
		t.Fatalf("EnsureRecord 替换失败: %v", err)
	}

	// 共享 hash-html 保持首条 local 定位。
	provider, key, ok := locatorOf(t, svc, "hash-html")
	if !ok {
		t.Fatalf("hash-html 内容对象应存在")
	}
	if provider != "local" || key != "artifacts/artifact-hash-a" {
		t.Fatalf("替换路径下 first-writer-wins 被破坏：hash-html 应为 (local, artifacts/artifact-hash-a)，实际 (%s, %s)", provider, key)
	}
}
