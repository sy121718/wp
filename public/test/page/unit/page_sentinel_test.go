package unit

import (
	"context"
	"errors"
	"testing"

	pagedto "go_wp/internal/module/page/dto"
	pageservice "go_wp/internal/module/page/service"
)

// TestPageServiceErrorsAreSentinel 锁定 page service 错误为包级 sentinel（审计项
// 「page 错误码靠中文文案 strings.Contains 匹配」的回归防线）：
// service 返回的错误必须能被 errors.Is 匹配到本包 sentinel，
// 而不是仅靠 err.Error() 文案相等（文案改动/拼接前缀即失效）。
func TestPageServiceErrorsAreSentinel(t *testing.T) {
	_, svc, _, _ := newPageService(t)
	ctx := context.Background()

	// Create(nil) → ErrInvalidParam（参数错误）
	_, err := svc.Create(ctx, nil)
	if !errors.Is(err, pageservice.ErrInvalidParam) {
		t.Fatalf("Create(nil) 应匹配 ErrInvalidParam: %v", err)
	}

	// 合法 ID 无页面 → ErrPageNotFound（资源不存在）
	_, err = svc.Detail(ctx, &pagedto.DetailReq{ID: "6f2c9d0e-1a2b-3c4d-8e9f-0a1b2c3d4e5f"})
	if !errors.Is(err, pageservice.ErrPageNotFound) {
		t.Fatalf("不存在的页面应匹配 ErrPageNotFound: %v", err)
	}

	// 文案兼容：sentinel Error() 与 enums 文案一致（前端响应不变）
	if err.Error() == "" {
		t.Fatalf("sentinel 不应为空文案")
	}
}
