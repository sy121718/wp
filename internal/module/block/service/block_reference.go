package blockservice

// block_reference.go — 全局块的引用检查端口与引用明细。
// 块被页面/其他块引用时禁止删除，依赖由编排层注入的回调反查（block 不反向依赖 page）。

import (
	"context"

	blockcontract "go_wp/internal/module/block/contract"
	"go_wp/pkg/logger"
)

// SetReferenceChecker 注入引用检查器（**布尔形式**：只回答「有没有引用」）。
//
// 与 SetStalePropagator 同模式（打破「block 先于 page 装配」的顺序约束）。
// 生产装配用的是 SetReferenceUsageChecker（明细形式，两处都注入时以它为准）；
// 本 setter 当前的调用方是**测试** —— 用例用它注入一个固定的引用判定，
// 或用 SetReferenceChecker(nil) 撤掉默认检查器让用例独立跑。
// 生产新增调用方之前先确认：明细检查器为什么不够。
func (s *Service) SetReferenceChecker(r func(ctx context.Context, blockID string) (bool, error)) {
	s.referenced = r
}

// SetReferenceUsageChecker 注入引用明细检查器（生产装配用；见 SetReferenceChecker）。
//
// 与 SetReferenceChecker 的关系：两处都注入时以本检查器为准（它能回答「哪些实体」，
// 布尔检查器只能回答「有没有」）—— 不做「两个都查、取或」是因为那会把两套口径
// 拼在一起，一处漏改就变成「明明被引用却放行」。
func (s *Service) SetReferenceUsageChecker(r func(ctx context.Context, blockID string) ([]blockcontract.BlockUsage, error)) {
	s.usages = r
}

// blockRefState 取「仍被哪些源码引用」：明细检查器优先，退回布尔检查器。
//
// 两个方向的失败都朝**拒绝**走（删除不可逆，宁拒勿删）：
//   - 检查器报错：按被引用处理（拿不到引用清单不能当成没有引用）；
//   - 检查器未注入：同样视为被引用。
//
// 返回的 usages 可能为空而 referenced 为 true（布尔检查器命中 / 检查失败）：
// 那种情况下拒绝依然成立，只是提示退回到通用文案。
func (s *Service) blockRefState(ctx context.Context, blockID string) (usages []blockcontract.BlockUsage, referenced bool) {
	if s.usages != nil {
		list, err := s.usages(ctx, blockID)
		if err != nil {
			logger.Scene("block").With("block_id", blockID).Error(err, "块引用明细检查失败，按被引用处理")
			return nil, true
		}
		return list, len(list) > 0
	}
	if s.referenced != nil {
		ok, err := s.referenced(ctx, blockID)
		if err != nil {
			logger.Scene("block").With("block_id", blockID).Error(err, "块引用检查失败，按被引用处理")
			return nil, true
		}
		return nil, ok
	}
	return nil, true
}

// ListBlockSourceRefs 列出引用该块的**其它全局块**（审计 ARCH-02：嵌套块引用）。
//
// 逐工程扇出（DB-009）：块 id 说不出工程，而 blocks 带 FORCE 策略 —— 漏作用域时
// 这条查询静默返回空，删除保护会据此放行。
func (s *Service) ListBlockSourceRefs(ctx context.Context, blockID string) (out []blockcontract.BlockUsage, err error) {
	ids, err := s.projectIDs(ctx)
	if err != nil {
		return nil, err
	}
	for _, projectID := range ids {
		if ctx.Err() != nil {
			break
		}
		rows, rerr := s.model.ListBlockDocumentRefs(ctx, projectID, blockID)
		if rerr != nil {
			return nil, rerr
		}
		for i := range rows {
			out = append(out, blockcontract.BlockUsage{
				Kind: blockcontract.UsageKindBlockDocument, ProjectID: projectID,
				EntityID: rows[i].ID, Label: rows[i].Name,
			})
		}
	}
	return out, nil
}
