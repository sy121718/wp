package unit

import (
	"context"
	"errors"
	"testing"

	pagedto "go_wp/internal/module/page/dto"
	pageservice "go_wp/internal/module/page/service"
)

func TestPageServiceErrorsAreSentinel(t *testing.T) {
	_, svc, _, projectID := newPageService(t)
	ctx := context.Background()

	_, err := svc.Create(ctx, nil)
	if !errors.Is(err, pageservice.ErrInvalidParam) {
		t.Fatalf("Create(nil) 应匹配 ErrInvalidParam: %v", err)
	}

	_, err = svc.Detail(ctx, &pagedto.DetailReq{ProjectID: projectID, ID: "6f2c9d0e-1a2b-3c4d-8e9f-0a1b2c3d4e5f"})
	if !errors.Is(err, pageservice.ErrPageNotFound) {
		t.Fatalf("不存在的页面应匹配 ErrPageNotFound: %v", err)
	}

	if err.Error() == "" {
		t.Fatalf("sentinel 不应为空文案")
	}
}
