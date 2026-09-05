package unit

import (
	"context"
	"testing"

	artifactdto "go_wp/internal/module/artifact/dto"
	artifactenums "go_wp/internal/module/artifact/enums"
)

// TestArtifactDefaultCreator 经 Record 间接覆盖 defaultCreator：
// 空白 CreatedBy → 零 UUID；非空 → 原样保留。
func TestArtifactDefaultCreator(t *testing.T) {
	svc := newService(t)

	t.Run("空CreatedBy回填零UUID", func(t *testing.T) {
		req := validReq()
		req.CreatedBy = ""
		res := mustRecord(t, svc, req)
		if res.CreatedBy != zeroUUID {
			t.Fatalf("空 CreatedBy 应回填零 UUID: %s", res.CreatedBy)
		}
	})

	t.Run("空白CreatedBy回填零UUID", func(t *testing.T) {
		req := validReq()
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
		req.ArtifactID = testArtifactID3
		req.ArtifactHash = "hash-keep-creator"
		req.CreatedBy = testCreatedBy
		res := mustRecord(t, svc, req)
		if res.CreatedBy != testCreatedBy {
			t.Fatalf("非空 CreatedBy 应原样保留: %s", res.CreatedBy)
		}
	})
}

// TestArtifactMapPersistenceError 间接覆盖 mapPersistenceError：
// gorm.ErrDuplicatedKey（manifest 内重复文件 hash 撞闭包复合主键、或
// 生产唯一约束冲突）→ ErrArtifactMismatch；其余错误原样透传。
func TestArtifactMapPersistenceError(t *testing.T) {
	t.Run("闭包复合主键冲突映射为ErrArtifactMismatch", func(t *testing.T) {
		svc := newService(t)
		req := validReq()
		req.ArtifactID = testArtifactID2
		req.ArtifactHash = "hash-dup-closure"
		req.Manifest = []byte(`{"canonicalPath":"/x.html","files":{"a.js":"HX","b.js":"HX"}}`)
		_, err := svc.Record(context.Background(), req)
		requireErrMsg(t, err, artifactenums.ErrArtifactMismatch)
	})

	t.Run("生产唯一约束冲突映射为ErrArtifactMismatch", func(t *testing.T) {
		svc := newServiceWithProdConstraint(t)
		mustRecord(t, svc, validReq())
		reqB := validReq()
		reqB.ArtifactID = testArtifactID2
		reqB.ArtifactHash = artifactHashV2
		_, err := svc.Record(context.Background(), reqB)
		requireErrMsg(t, err, artifactenums.ErrArtifactMismatch)
	})
}

// TestArtifactFindByHash 经 Detail / Record 幂等间接覆盖 findByHash：
// 命中返回 exists=true；未命中 exists=false（转为 ErrArtifactNotFound）。
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

// TestArtifactToRespManifestUnparseable 覆盖 toResp 对非法 manifest 的容错：
// 数据库中存在非法 manifest 时 Detail 仍返回记录，CanonicalPath 为空，
// 解析失败仅告警不报错（不回读校验失败）。
func TestArtifactToRespManifestUnparseable(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	// 直接写一条 manifest 为合法 JSON 但 canonicalPath 类型不匹配的记录，
	// 绕过 service 校验，模拟历史脏数据（Unmarshal 到 struct 会失败）。
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
