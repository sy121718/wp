package unit

import (
	"context"
	json "go_wp/pkg/jsonx"
	"strconv"
	"strings"
	"testing"
	"time"

	artifactenums "go_wp/internal/module/artifact/enums"
)

func TestArtifactRecordSuccess(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	res, err := svc.Record(ctx, validReq())
	if err != nil {
		t.Fatalf("归档产物失败: %v", err)
	}
	if res.ID != testArtifactID {
		t.Fatalf("ID 不一致: %s", res.ID)
	}
	if res.PageID != testPageID {
		t.Fatalf("PageID 不一致: %s", res.PageID)
	}
	if res.Version != 1 {
		t.Fatalf("Version 不一致: %d", res.Version)
	}
	if res.PayloadState != "available" {
		t.Fatalf("PayloadState 应为 available: %s", res.PayloadState)
	}

	if res.CreatedBy != zeroUUID {
		t.Fatalf("CreatedBy 应回填零 UUID: %s", res.CreatedBy)
	}

	if res.CanonicalPath != "/index.html" {
		t.Fatalf("CanonicalPath 提取失败: %s", res.CanonicalPath)
	}
	if res.ArtifactHash != artifactHashV1 {
		t.Fatalf("ArtifactHash 不一致: %s", res.ArtifactHash)
	}
	assertRecent(t, res.CreatedAt.Time())
}

func TestArtifactRecordCreatedByPreserved(t *testing.T) {
	svc := newService(t)
	req := validReq()
	req.CreatedBy = testCreatedBy

	res := mustRecord(t, svc, req)
	if res.CreatedBy != testCreatedBy {
		t.Fatalf("CreatedBy 应原样保留: %s", res.CreatedBy)
	}
}

func TestArtifactRecordDedupByIdempotent(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	first := mustRecord(t, svc, validReq())

	repeated, err := svc.Record(ctx, validReq())
	if err != nil {
		t.Fatalf("重复归档应幂等成功: %v", err)
	}
	if repeated.ID != first.ID {
		t.Fatalf("重复归档返回了不同记录: %s vs %s", repeated.ID, first.ID)
	}
	if !repeated.CreatedAt.Time().Truncate(time.Microsecond).Equal(first.CreatedAt.Time().Truncate(time.Microsecond)) {
		t.Fatalf("重复归档 CreatedAt 不一致: %v vs %v", repeated.CreatedAt, first.CreatedAt)
	}

	if n := artifactRowCount(t, svc); n != 1 {
		t.Fatalf("重复归档后行数应为 1: %d", n)
	}

	if n := closureCount(t, svc, first.ID); n != 2 {
		t.Fatalf("闭包应为 2 条: %d", n)
	}
}

func TestArtifactRecordClosures(t *testing.T) {
	svc := newService(t)

	recorded := mustRecord(t, svc, validReq())

	if n := closureCount(t, svc, recorded.ID); n != 2 {
		t.Fatalf("闭包应为 2 条: %d", n)
	}
	if n := contentObjectCount(t, svc); n != 2 {
		t.Fatalf("content_objects 应为 2 行: %d", n)
	}
	if !contentObjectExists(t, svc, "hash-html") || !contentObjectExists(t, svc, "hash-manifest") {
		t.Fatalf("content_objects 应包含 manifest 中的文件哈希")
	}

	mustRecord(t, svc, validReq())
	if n := contentObjectCount(t, svc); n != 2 {
		t.Fatalf("重复归档后 content_objects 应仍为 2 行: %d", n)
	}
}

func TestArtifactRecordNilRequest(t *testing.T) {
	svc := newService(t)
	_, err := svc.Record(context.Background(), nil)
	requireErrMsg(t, err, artifactenums.ErrInvalidArtifact)
}

func TestArtifactRecordInvalidInputs(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	t.Run("非法ManifestJSON返回ErrInvalidArtifact", func(t *testing.T) {
		req := validReq()
		req.Manifest = json.RawMessage(`not-json`)
		_, err := svc.Record(ctx, req)
		requireErrMsg(t, err, artifactenums.ErrInvalidArtifact)
	})

	t.Run("嵌套对象Manifest返回ErrInvalidArtifact", func(t *testing.T) {
		req := validReq()
		req.Manifest = json.RawMessage(`{"files":{"a":{"b":"c"}}}`)
		_, err := svc.Record(ctx, req)
		requireErrMsg(t, err, artifactenums.ErrInvalidArtifact)
	})

	t.Run("files为null返回ErrInvalidArtifact", func(t *testing.T) {
		req := validReq()
		req.Manifest = json.RawMessage(`{"files":null}`)
		_, err := svc.Record(ctx, req)
		requireErrMsg(t, err, artifactenums.ErrInvalidArtifact)
	})

	t.Run("空对象Manifest被拒绝", func(t *testing.T) {

		req := validReq()
		req.ArtifactID = testArtifactID2
		req.ArtifactHash = "hash-empty-files"
		req.Manifest = json.RawMessage(`{"files":{}}`)
		_, err := svc.Record(ctx, req)
		requireErrMsg(t, err, artifactenums.ErrInvalidArtifact)

		if n := artifactRowCount(t, svc); n != 0 {
			t.Fatalf("空 files 被拒后不应留行: %d", n)
		}
	})

	t.Run("空ArtifactHash拒绝", func(t *testing.T) {

		req := validReq()
		req.ArtifactHash = ""
		_, err := svc.Record(ctx, req)
		if err == nil {
			t.Fatalf("空 ArtifactHash 应被拒绝")
		}
	})

	t.Run("空ArtifactID报底层DB错误", func(t *testing.T) {

		req := validReq()
		req.ArtifactID = ""
		_, err := svc.Record(ctx, req)
		if err == nil || err.Error() != artifactenums.ErrInvalidArtifact {
			t.Fatalf("空 ArtifactID 应返回 ErrInvalidArtifact，实际: %v", err)
		}
	})

	t.Run("空PageID拒绝", func(t *testing.T) {

		req := validReq()
		req.PageID = ""
		_, err := svc.Record(ctx, req)
		if err == nil || err.Error() != artifactenums.ErrInvalidArtifact {
			t.Fatalf("空 PageID 应返回 ErrInvalidArtifact，实际: %v", err)
		}
	})

	t.Run("零值Version拒绝", func(t *testing.T) {

		req := validReq()
		req.ArtifactID = testArtifactID3
		req.ArtifactHash = "hash-version-zero"
		req.Version = 0
		_, err := svc.Record(ctx, req)
		if err == nil || err.Error() != artifactenums.ErrInvalidArtifact {
			t.Fatalf("零值 Version 应返回 ErrInvalidArtifact，实际: %v", err)
		}
	})

	t.Run("nilSourceDocument报底层DB错误", func(t *testing.T) {

		req := validReq()
		req.SourceDocument = nil
		_, err := svc.Record(ctx, req)
		requireAnyErr(t, err, "nil SourceDocument")
		if err != nil && err.Error() == artifactenums.ErrInvalidArtifact {
			t.Fatalf("nil SourceDocument 实际未走参数校验")
		}
	})
}

func TestArtifactRecordDuplicateFileHashInManifest(t *testing.T) {
	svc := newService(t)
	req := validReq()
	req.Manifest = json.RawMessage(`{"canonicalPath":"/dup.html","files":{"index.html":"H","app.js":"H"}}`)

	res, err := svc.Record(context.Background(), req)

	if err != nil {
		t.Fatalf("重复文件哈希应归档成功: %v", err)
	}
	if n := artifactRowCount(t, svc); n != 1 {
		t.Fatalf("应留下 1 条归档记录: %d", n)
	}
	if n := closureCount(t, svc, res.ID); n != 1 {
		t.Fatalf("重复哈希应去重为 1 条闭包: %d", n)
	}
}

func TestArtifactRecordEmptyFileHashSkipped(t *testing.T) {
	svc := newService(t)
	req := validReq()
	req.Manifest = json.RawMessage(`{"canonicalPath":"/e.html","files":{"a":"","b":"hash-b","c":"   "}}`)

	res := mustRecord(t, svc, req)

	if n := closureCount(t, svc, res.ID); n != 1 {
		t.Fatalf("闭包应为 1 条（空白 hash 跳过）: %d", n)
	}
}

func TestArtifactRecordSpecialCharsHash(t *testing.T) {
	svc := newService(t)
	req := validReq()
	req.ArtifactID = testArtifactID2
	req.ArtifactHash = "hash-with-'quote\"-\\-unicode-中文-🚀"
	req.Manifest = json.RawMessage(`{"canonicalPath":"/s.html","files":{"a":"'quote\"\\中文🚀"}}`)

	res := mustRecord(t, svc, req)
	if !contentObjectExists(t, svc, "'quote\"\\中文🚀") {
		t.Fatalf("特殊字符 hash 内容对象应写入")
	}
	if n := closureCount(t, svc, res.ID); n != 1 {
		t.Fatalf("闭包应为 1 条: %d", n)
	}
}

func TestArtifactRecordOversizeHash(t *testing.T) {
	svc := newService(t)
	req := validReq()
	req.ArtifactID = testArtifactID2
	req.ArtifactHash = "big-" + strings.Repeat("a", 64*1024)
	req.Manifest = json.RawMessage(`{"canonicalPath":"/big.html","files":{"a":"` +
		strings.Repeat("b", 64*1024) + `"}}`)

	res := mustRecord(t, svc, req)
	if res.ArtifactHash == "" {
		t.Fatalf("超长 hash 应入库")
	}
	if n := closureCount(t, svc, res.ID); n != 1 {
		t.Fatalf("闭包应为 1 条: %d", n)
	}
}

func TestArtifactRecordOversizeManifest(t *testing.T) {
	svc := newService(t)
	req := validReq()
	req.ArtifactID = testArtifactID2
	req.ArtifactHash = "hash-big-manifest"

	files := make(map[string]string, 200)
	for i := 0; i < 200; i++ {
		files["f"+strconv.Itoa(i)] = "content-hash-" + strconv.Itoa(i)
	}
	manifest, err := json.Marshal(map[string]any{"canonicalPath": "/m.html", "files": files})
	if err != nil {
		t.Fatalf("构造 manifest 失败: %v", err)
	}
	req.Manifest = manifest

	res := mustRecord(t, svc, req)
	if n := closureCount(t, svc, res.ID); n != 200 {
		t.Fatalf("闭包应为 200 条: %d", n)
	}
}

func TestArtifactRecordNonUUIDInputs(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	t.Run("非UUIDPageID报底层DB错误", func(t *testing.T) {
		req := validReq()
		req.PageID = "not-a-uuid"
		_, err := svc.Record(ctx, req)
		requireAnyErr(t, err, "非 UUID PageID")
		if err != nil && err.Error() == artifactenums.ErrInvalidArtifact {
			t.Fatalf("非 UUID PageID 实际未走参数校验")
		}
	})

	t.Run("非UUIDCreatedBy报底层DB错误", func(t *testing.T) {
		req := validReq()
		req.ArtifactID = testArtifactID2
		req.ArtifactHash = "hash-bad-creator"
		req.CreatedBy = "not-a-uuid"
		_, err := svc.Record(ctx, req)
		requireAnyErr(t, err, "非 UUID CreatedBy")
		if err != nil && err.Error() == artifactenums.ErrInvalidArtifact {
			t.Fatalf("非 UUID CreatedBy 实际未走参数校验")
		}
	})
}
