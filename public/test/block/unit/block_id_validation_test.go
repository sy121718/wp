package unit

import (
	"context"
	"errors"
	"testing"

	blockdto "go_wp/internal/module/block/dto"
	blockservice "go_wp/internal/module/block/service"
)

func TestBlockCloneRejectsMalformedID(t *testing.T) {

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

	if _, err := svc.CloneAST(ctx, &blockdto.CloneReq{ID: "   "}); !errors.Is(err, blockservice.ErrParamRequired) {
		t.Fatalf("空 id 应报参数错误，实际 %v", err)
	}
}
