package unit

// block_ref_guard_test.go — 块删除保护的引用明细（审计 ARCH-02）。
//
// ARCH-02 之前这里只有「有没有引用」这一个布尔：拒绝时用户拿到的是
// ErrBlockInUse 一句通用文案，而块引用散在页面文档 / 模板 / 实例快照的 JSONB 任意深度 ——
// 操作者没有任何线索去解除它。本文件钉住三件事：
//
//  1. 明细必须随拒绝一起返回（errors.As 取得到 *BlockInUseError，且带上类别与实体）；
//  2. 明细不影响强制删除（Force 仍然删得掉）；
//  3. 检查器缺失 / 报错时仍然拒绝（宁拒勿删，不把失败伪装成「没有引用」）。

import (
	"context"
	"errors"
	"strings"
	"testing"

	blockcontract "go_wp/internal/module/block/contract"
	blockdto "go_wp/internal/module/block/dto"
)

// TestBlockDeleteReportsReferenceDetail 被引用拒绝时错误必须带「哪一类引用、哪些实体」。
func TestBlockDeleteReportsReferenceDetail(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.svc.SetReferenceUsageChecker(func(context.Context, string) ([]blockcontract.BlockUsage, error) {
		return []blockcontract.BlockUsage{
			{Kind: blockcontract.UsageKindBlockDocument, ProjectID: e.projectID, EntityID: "outer-block", Label: "外层块"},
			{Kind: blockcontract.UsageKindContentTemplate, ProjectID: e.projectID, EntityID: "tpl-1", Label: "商品详情模板"},
		}, nil
	})
	b := e.createBlock(t, "内层块", "block")

	err := e.svc.Delete(ctx, &blockdto.DeleteReq{ID: b.ID})
	if !errors.Is(err, blockcontract.ErrBlockInUse) {
		t.Fatalf("期望 ErrBlockInUse，实际 %v", err)
	}
	var inUse *blockcontract.BlockInUseError
	if !errors.As(err, &inUse) {
		t.Fatalf("拒绝错误必须携带引用明细（*BlockInUseError），实际 %T: %v", err, err)
	}
	if len(inUse.Usages) != 2 {
		t.Fatalf("引用明细条数应为 2，实际 %d：%#v", len(inUse.Usages), inUse.Usages)
	}
	msg := err.Error()
	for _, want := range []string{string(blockcontract.UsageKindBlockDocument), "外层块",
		string(blockcontract.UsageKindContentTemplate), "商品详情模板"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("拒绝文案应能定位到 %q，实际 %q", want, msg)
		}
	}
	// 明细只是提示，不改变强制语义：Force 仍要删得掉。
	if err := e.svc.Delete(ctx, &blockdto.DeleteReq{ID: b.ID, Force: true}); err != nil {
		t.Fatalf("Force 删除不应被拦截: %v", err)
	}
}

// TestBlockDeleteCheckFailureStillRefuses 明细检查器报错时按「被引用」处理（宁拒勿删）。
func TestBlockDeleteCheckFailureStillRefuses(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.svc.SetReferenceUsageChecker(func(context.Context, string) ([]blockcontract.BlockUsage, error) {
		return nil, errors.New("反查失败")
	})
	b := e.createBlock(t, "查不出引用的块", "block")
	err := e.svc.Delete(ctx, &blockdto.DeleteReq{ID: b.ID})
	if !errors.Is(err, blockcontract.ErrBlockInUse) {
		t.Fatalf("检查失败必须按被引用处理（否则一次读取失败就放行了删除），实际 %v", err)
	}
}

// TestBlockDeleteWithoutCheckerStillRefuses 检查器未注入时同样按被引用处理。
//
// 这条是「宁拒勿删」的兜底：删除不可逆，宁可让装配缺陷表现为「删不掉」，
// 也不能表现为「安静地删掉了还有引用的块」。
func TestBlockDeleteWithoutCheckerStillRefuses(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.svc.SetReferenceChecker(nil) // newEnv 默认注入了「无引用」的检查器，这里显式撤掉
	b := e.createBlock(t, "未装配检查器的块", "block")
	if err := e.svc.Delete(ctx, &blockdto.DeleteReq{ID: b.ID}); !errors.Is(err, blockcontract.ErrBlockInUse) {
		t.Fatalf("检查器缺失时必须拒绝删除，实际 %v", err)
	}
}
