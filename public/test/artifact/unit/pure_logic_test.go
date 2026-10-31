package unit

import (
	"context"
	"testing"

	artifactdto "go_wp/internal/module/artifact/dto"
	artifactenums "go_wp/internal/module/artifact/enums"
)

func TestArtifactDefaultCreator(t *testing.T) {
	svc := newService(t)

	t.Run("空CreatedBy回填零UUID", func(t *testing.T) {
		req := validReq()
		req.PageID = "bbbbbbbb-0000-0000-0000-000000000011"
		req.CreatedBy = ""
		res := mustRecord(t, svc, req)
		if res.CreatedBy != zeroUUID {
			t.Fatalf("空 CreatedBy 应回填零 UUID: %s", res.CreatedBy)
		}
	})

	t.Run("空白CreatedBy回填零UUID", func(t *testing.T) {
		req := validReq()
		req.PageID = "bbbbbbbb-0000-0000-0000-000000000012"
		req.ArtifactID = testArtifactID2
		req.ArtifactHash = "hash-blank-creator"
		req.CreatedBy = "   "
		res := mustRecord(t, svc, req)
		if res.CreatedBy != zeroUUID {
			t.Fatalf("空白 CreatedBy 应回填零 UUID: %s", res.CreatedBy)
		}
	})

	t.Run("非空CreatedBy原样保留", func(t *testing.T) {
		req := validReq()
		req.PageID = "bbbbbbbb-0000-0000-0000-000000000013"
		req.ArtifactID = testArtifactID3
		req.ArtifactHash = "hash-keep-creator"
		req.CreatedBy = testCreatedBy
		res := mustRecord(t, svc, req)
		if res.CreatedBy != testCreatedBy {
			t.Fatalf("非空 CreatedBy 应原样保留: %s", res.CreatedBy)
		}
	})
}

func TestArtifactMapPersistenceError(t *testing.T) {
	t.Run("闭包去重：manifest 重复哈希不再触发主键冲突", func(t *testing.T) {
		svc := newService(t)
		req := validReq()
		req.ArtifactID = testArtifactID2
		req.ArtifactHash = "hash-dup-closure"
		req.Manifest = []byte(`{"canonicalPath":"/x.html","files":{"a.js":"HX","b.js":"HX"}}`)

		res := mustRecord(t, svc, req)
		if n := closureCount(t, svc, res.ID); n != 1 {
			t.Fatalf("重复文件哈希应去重为 1 条闭包: %d", n)
		}
	})

	t.Run("生产唯一约束冲突映射为ErrArtifactMismatch", func(t *testing.T) {
		svc := newService(t)
		mustRecord(t, svc, validReq())
		reqB := validReq()
		reqB.ArtifactHash = artifactHashV2
		_, err := svc.Record(context.Background(), reqB)
		requireErrMsg(t, err, artifactenums.ErrArtifactMismatch)
	})
}

func TestArtifactFindByHash(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	t.Run("命中返回既有记录", func(t *testing.T) {
		first := mustRecord(t, svc, validReq())
		repeated, err := svc.Record(ctx, validReq())
		if err != nil {
			t.Fatalf("重复归档应命中既有记录: %v", err)
		}
		if repeated.ID != first.ID {
			t.Fatalf("findByHash 应命中同一条记录: %s vs %s", repeated.ID, first.ID)
		}
	})

	t.Run("未命中返回NotFound", func(t *testing.T) {
		_, err := svc.Detail(ctx, &artifactdto.DetailReq{PageID: testPageID, Hash: "absent-hash"})
		requireErrMsg(t, err, artifactenums.ErrArtifactNotFound)
	})
}

func TestArtifactToRespManifestUnparseable(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	raw := validReq()
	res, err := svc.Record(ctx, raw)
	if err != nil {
		t.Fatalf("Record 失败: %v", err)
	}
	if err := svc.Model().DB(ctx).
		Where("id = ?", res.ID).
		Update("manifest", []byte(`{"canonicalPath":123}`)).Error; err != nil {
		t.Fatalf("写入脏 manifest 失败: %v", err)
	}

	detail, err := svc.DetailByID(ctx, &artifactdto.DetailByIDReq{ID: res.ID})
	if err != nil {
		t.Fatalf("脏 manifest 下 Detail 不应失败: %v", err)
	}
	if detail.CanonicalPath != "" {
		t.Fatalf("非法 manifest 时 CanonicalPath 应为空: %s", detail.CanonicalPath)
	}
}
