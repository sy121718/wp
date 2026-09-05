package unit

import (
	"context"
	"encoding/json"
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
	// defaultCreator：CreatedBy 留空时应回填零 UUID。
	if res.CreatedBy != zeroUUID {
		t.Fatalf("CreatedBy 应回填零 UUID: %s", res.CreatedBy)
	}
	// toResp 从 manifest 提取 canonicalPath。
	if res.CanonicalPath != "/index.html" {
		t.Fatalf("CanonicalPath 提取失败: %s", res.CanonicalPath)
	}
	if res.ArtifactHash != artifactHashV1 {
		t.Fatalf("ArtifactHash 不一致: %s", res.ArtifactHash)
	}
	assertRecent(t, res.CreatedAt)
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
	// 同 page+hash 重复归档：内容寻址幂等，返回既有记录。
	repeated, err := svc.Record(ctx, validReq())
	if err != nil {
		t.Fatalf("重复归档应幂等成功: %v", err)
	}
	if repeated.ID != first.ID {
		t.Fatalf("重复归档返回了不同记录: %s vs %s", repeated.ID, first.ID)
	}
	if !repeated.CreatedAt.Truncate(time.Microsecond).Equal(first.CreatedAt.Truncate(time.Microsecond)) {
		t.Fatalf("重复归档 CreatedAt 不一致: %v vs %v", repeated.CreatedAt, first.CreatedAt)
	}
	// 数据库中只有一行。
	if n := artifactRowCount(t, svc); n != 1 {
		t.Fatalf("重复归档后行数应为 1: %d", n)
	}
	// 闭包也不应重复。
	if n := closureCount(t, svc, first.ID); n != 2 {
		t.Fatalf("闭包应为 2 条: %d", n)
	}
}

func TestArtifactRecordClosures(t *testing.T) {
	svc := newService(t)

	recorded := mustRecord(t, svc, validReq())
	// manifest.files 的两个文件哈希 → 两条闭包 + 两个共享内容对象。
	if n := closureCount(t, svc, recorded.ID); n != 2 {
		t.Fatalf("闭包应为 2 条: %d", n)
	}
	if n := contentObjectCount(t, svc); n != 2 {
		t.Fatalf("content_objects 应为 2 行: %d", n)
	}
	if !contentObjectExists(t, svc, "hash-html") || !contentObjectExists(t, svc, "hash-manifest") {
		t.Fatalf("content_objects 应包含 manifest 中的文件哈希")
	}
	// 共享内容对象：第二条记录复用同一批 hash 时不重复建对象。
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

// TestArtifactRecordInvalidInputs 覆盖 service 层缺失参数校验时的行为。
// 注：DTO 的 binding:"required" 只在 HTTP 绑定层生效，service 直接调用时不校验。
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

	t.Run("空对象Manifest被接受但无闭包", func(t *testing.T) {
		// 行为记录：{"files":{}} 的 Files 是非 nil 空 map，可通过校验；
		// 与 {"files":null} 被拒绝不一致（见 bug 报告，低严重度）。
		req := validReq()
		req.ArtifactID = testArtifactID2
		req.ArtifactHash = "hash-empty-files"
		req.Manifest = json.RawMessage(`{"files":{}}`)
		res := mustRecord(t, svc, req)
		if n := closureCount(t, svc, res.ID); n != 0 {
			t.Fatalf("空 files 闭包应为 0: %d", n)
		}
	})

	t.Run("空ArtifactHash可入库", func(t *testing.T) {
		// 行为记录/bug：artifact_hash 未校验非空，空字符串能成功入库，
		// 破坏内容寻址语义（后续按该空 hash 可查出记录）。
		req := validReq()
		req.ArtifactHash = ""
		res, err := svc.Record(ctx, req)
		if err != nil {
			t.Fatalf("空 ArtifactHash 当前行为是插入成功（bug 证据）: %v", err)
		}
		if res.ArtifactHash != "" {
			t.Fatalf("入库 hash 应为空: %q", res.ArtifactHash)
		}
	})

	t.Run("空ArtifactID报底层DB错误", func(t *testing.T) {
		// 行为记录/bug：空 ID 走 uuid 列插入触发 PG 22P02，
		// 错误未归一化为 ErrInvalidArtifact。
		req := validReq()
		req.ArtifactID = ""
		_, err := svc.Record(ctx, req)
		requireAnyErr(t, err, "空 ArtifactID")
		if err != nil && err.Error() == artifactenums.ErrInvalidArtifact {
			t.Fatalf("空 ArtifactID 实际未走参数校验")
		}
	})

	t.Run("空PageID报底层DB错误", func(t *testing.T) {
		// 行为记录/bug：空 PageID 在 findByHash 查询 uuid 列时即报 22P02。
		req := validReq()
		req.PageID = ""
		_, err := svc.Record(ctx, req)
		requireAnyErr(t, err, "空 PageID")
		if err != nil && err.Error() == artifactenums.ErrInvalidArtifact {
			t.Fatalf("空 PageID 实际未走参数校验")
		}
	})

	t.Run("零值Version可入库", func(t *testing.T) {
		// 行为记录/bug：Version 无 min 校验，0 能入库（DTO 的 min=1 仅 HTTP 层生效）。
		req := validReq()
		req.ArtifactID = testArtifactID3
		req.ArtifactHash = "hash-version-zero"
		req.Version = 0
		res, err := svc.Record(ctx, req)
		if err != nil {
			t.Fatalf("零值 Version 当前行为是插入成功（bug 证据）: %v", err)
		}
		if res.Version != 0 {
			t.Fatalf("入库 Version 应为 0: %d", res.Version)
		}
	})

	t.Run("nilSourceDocument报底层DB错误", func(t *testing.T) {
		// 行为记录/bug：SourceDocument 为 nil 时 jsonb NOT NULL 约束报错，
		// 错误未归一化为 ErrInvalidArtifact。
		req := validReq()
		req.SourceDocument = nil
		_, err := svc.Record(ctx, req)
		requireAnyErr(t, err, "nil SourceDocument")
		if err != nil && err.Error() == artifactenums.ErrInvalidArtifact {
			t.Fatalf("nil SourceDocument 实际未走参数校验")
		}
	})
}

// TestArtifactRecordDuplicateFileHashInManifest 覆盖"manifest 内不同路径引用同一
// 文件哈希"场景：去重后的闭包（artifact_id, content_hash 复合主键）会撞键失败。
func TestArtifactRecordDuplicateFileHashInManifest(t *testing.T) {
	svc := newService(t)
	req := validReq()
	req.Manifest = json.RawMessage(`{"canonicalPath":"/dup.html","files":{"index.html":"H","app.js":"H"}}`)

	_, err := svc.Record(context.Background(), req)
	// 行为记录/bug：合法输入（两个路径共享同一内容哈希）导致归档失败。
	requireErrMsg(t, err, artifactenums.ErrArtifactMismatch)
	// 事务应回滚：不留半条记录。
	if n := artifactRowCount(t, svc); n != 0 {
		t.Fatalf("失败归档不应留行: %d", n)
	}
}

func TestArtifactRecordEmptyFileHashSkipped(t *testing.T) {
	svc := newService(t)
	req := validReq()
	req.Manifest = json.RawMessage(`{"canonicalPath":"/e.html","files":{"a":"","b":"hash-b","c":"   "}}`)

	res := mustRecord(t, svc, req)
	// 空白 hash 条目被跳过：只有 hash-b 一条闭包。
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
	req.ArtifactHash = "big-" + strings.Repeat("a", 64*1024) // 64KB hash 字符串
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

// TestArtifactRecordNonUUIDInputs 覆盖非 UUID 格式输入：uuid 列直接报 22P02，
// 错误未归一化为业务错误。
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
