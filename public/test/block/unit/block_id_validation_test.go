package unit

// block_id_validation_test.go — 非法 id 的处理口径（回归资产）。
//
// 背景（实测）：POST /api/block/clone 传一个随手写的 id（"nonexistent"）会返回 500
// 「系统内部错误」。根因是 blocks.id 是 uuid 列，查询落到 PG 时报
// invalid input syntax for type uuid —— 那不是 gorm.ErrRecordNotFound，
// handler 只能按「未知错误」映射成 500，而真正的原因只是 id 写错了。
//
// 这条测试**不需要数据库**：非法 id 在 service 层就被判成「不存在」，
// 根本不会走到 model（所以下面可以传 nil 依赖）。

import (
	"context"
	"errors"
	"testing"

	blockdto "go_wp/internal/module/block/dto"
	blockservice "go_wp/internal/module/block/service"
)

func TestBlockCloneRejectsMalformedID(t *testing.T) {
	// nil model：本用例只在「id 不合法」这条提前返回的路径上跑，不会碰持久化。
	svc := blockservice.NewService(nil, nil)
	ctx := context.Background()

	cases := []struct {
		name string
		id   string
	}{
		{"plain word", "nonexistent"},
		{"almost uuid", "510c699a-e684-4d08-9974"},
		{"uuid with extra chars", "510c699a-e684-4d08-9974-018b406061b9-x"},
		{"sql-ish payload", "1 OR 1=1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.CloneAST(ctx, &blockdto.CloneReq{ID: tc.id})
			if !errors.Is(err, blockservice.ErrNotFound) {
				t.Fatalf("非法 id %q 应判「块不存在」（404），实际 %v", tc.id, err)
			}
		})
	}

	// 空 id 走的是另一条路（参数校验），保持与既有语义一致。
	if _, err := svc.CloneAST(ctx, &blockdto.CloneReq{ID: "   "}); !errors.Is(err, blockservice.ErrParamRequired) {
		t.Fatalf("空 id 应报参数错误，实际 %v", err)
	}
}
