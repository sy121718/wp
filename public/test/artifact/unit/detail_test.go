package unit

import (
	"context"
	"testing"

	artifactdto "go_wp/internal/module/artifact/dto"
	artifactenums "go_wp/internal/module/artifact/enums"
)

func TestArtifactDetailSuccess(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	recorded := mustRecord(t, svc, validReq())
	detail, err := svc.Detail(ctx, &artifactdto.DetailReq{PageID: testPageID, Hash: artifactHashV1})
	if err != nil {
		t.Fatalf("Detail 查询失败: %v", err)
	}
	if detail.ID != recorded.ID {
		t.Fatalf("Detail 与归档不一致: %s vs %s", detail.ID, recorded.ID)
	}
	if detail.CanonicalPath != "/index.html" {
		t.Fatalf("CanonicalPath 应回读: %s", detail.CanonicalPath)
	}
	if detail.PayloadState != "available" {
		t.Fatalf("PayloadState 应回读: %s", detail.PayloadState)
	}
}

func TestArtifactDetailNotFound(t *testing.T) {
	svc := newService(t)
	_, err := svc.Detail(context.Background(), &artifactdto.DetailReq{PageID: testPageID, Hash: "no-such-hash"})
	requireErrMsg(t, err, artifactenums.ErrArtifactNotFound)
}

func TestArtifactDetailEmptyInputs(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	t.Run("nil请求返回ErrInvalidParam", func(t *testing.T) {
		_, err := svc.Detail(ctx, nil)
		requireErrMsg(t, err, artifactenums.ErrInvalidParam)
	})

	t.Run("空PageID返回ErrInvalidParam", func(t *testing.T) {
		_, err := svc.Detail(ctx, &artifactdto.DetailReq{PageID: "", Hash: "h"})
		requireErrMsg(t, err, artifactenums.ErrInvalidParam)
	})

	t.Run("空白PageID返回ErrInvalidParam", func(t *testing.T) {
		_, err := svc.Detail(ctx, &artifactdto.DetailReq{PageID: "   ", Hash: "h"})
		requireErrMsg(t, err, artifactenums.ErrInvalidParam)
	})

	t.Run("空Hash返回ErrInvalidParam", func(t *testing.T) {
		_, err := svc.Detail(ctx, &artifactdto.DetailReq{PageID: testPageID, Hash: ""})
		requireErrMsg(t, err, artifactenums.ErrInvalidParam)
	})

	t.Run("空白Hash返回ErrInvalidParam", func(t *testing.T) {
		_, err := svc.Detail(ctx, &artifactdto.DetailReq{PageID: testPageID, Hash: " \t "})
		requireErrMsg(t, err, artifactenums.ErrInvalidParam)
	})
}

// TestArtifactDetailInvalidUUID 覆盖非 UUID PageID：uuid 列查询报 22P02，
// 错误未归一化为 ErrArtifactNotFound。
func TestArtifactDetailInvalidUUID(t *testing.T) {
	svc := newService(t)
	_, err := svc.Detail(context.Background(), &artifactdto.DetailReq{PageID: "not-a-uuid", Hash: "h"})
	requireAnyErr(t, err, "非 UUID PageID 的 Detail")
	if err != nil && err.Error() == artifactenums.ErrArtifactNotFound {
		t.Fatalf("非 UUID PageID 实际未走查询归一化")
	}
}

func TestArtifactDetailByIDSuccess(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	recorded := mustRecord(t, svc, validReq())
	detail, err := svc.DetailByID(ctx, &artifactdto.DetailByIDReq{ID: testArtifactID})
	if err != nil {
		t.Fatalf("DetailByID 查询失败: %v", err)
	}
	if detail.ID != recorded.ID {
		t.Fatalf("DetailByID 与归档不一致: %s vs %s", detail.ID, recorded.ID)
	}
	if detail.ArtifactHash != artifactHashV1 {
		t.Fatalf("ArtifactHash 应回读: %s", detail.ArtifactHash)
	}
}

func TestArtifactDetailByIDNotFound(t *testing.T) {
	svc := newService(t)
	// 合法 UUID 但不存在 → ErrRecordNotFound → ErrArtifactNotFound。
	_, err := svc.DetailByID(context.Background(), &artifactdto.DetailByIDReq{ID: "ffffffff-0000-0000-0000-0000000000ff"})
	requireErrMsg(t, err, artifactenums.ErrArtifactNotFound)
}

func TestArtifactDetailByIDEmptyInputs(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	t.Run("nil请求返回ErrInvalidParam", func(t *testing.T) {
		_, err := svc.DetailByID(ctx, nil)
		requireErrMsg(t, err, artifactenums.ErrInvalidParam)
	})

	t.Run("空ID返回ErrInvalidParam", func(t *testing.T) {
		_, err := svc.DetailByID(ctx, &artifactdto.DetailByIDReq{ID: ""})
		requireErrMsg(t, err, artifactenums.ErrInvalidParam)
	})

	t.Run("空白ID返回ErrInvalidParam", func(t *testing.T) {
		_, err := svc.DetailByID(ctx, &artifactdto.DetailByIDReq{ID: "  "})
		requireErrMsg(t, err, artifactenums.ErrInvalidParam)
	})
}

// TestArtifactDetailByIDInvalidUUID 覆盖非 UUID ID：uuid 列查询报 22P02，
// 错误未归一化为 ErrArtifactNotFound。
func TestArtifactDetailByIDInvalidUUID(t *testing.T) {
	svc := newService(t)
	_, err := svc.DetailByID(context.Background(), &artifactdto.DetailByIDReq{ID: "not-a-uuid"})
	requireAnyErr(t, err, "非 UUID ID 的 DetailByID")
	if err != nil && err.Error() == artifactenums.ErrArtifactNotFound {
		t.Fatalf("非 UUID ID 实际未走查询归一化")
	}
}
