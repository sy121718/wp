package presentationservice

// presentation_blocks.go — 内容模板内的全局块引用展开（core.globalref）。
//
// 内容模板是「完整文档层」，内部可以引用页眉/页脚/信任徽章等全局区块
// （docs/02-D §1.2：两者是包含关系，不是二选一）；构建期由本适配器把区块
// 文档内联进同一次编译输出，访问面仍是纯静态（无运行时拼接）。
//
// 与手工 Page 路径（page/service/page_assemble.go）同口径：
//   - 同一次编译内按块 ID 缓存（同一块被多次引用只查一次库）；
//   - 失败结果同样缓存，避免重复查库；
//   - reuse_mode=template 的块是「一次性复制」语义，不允许被引用展开，
//     命中即报错暴露（正常流程下副本已在插入时并入文档）。

import (
	"context"
	"fmt"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	blockcontract "go_wp/internal/module/block/contract"
)

// blockResolverAdapter 把 block 契约适配为 builder 的 core.BlockResolver。
type blockResolverAdapter struct {
	blocks blockcontract.BlockService
	ctx    context.Context
	// projectID 是块查询的必填 scope（block.Detail 用它做跨工程越权防护）：
	// 漏传只会得到「参数缺失」，而这里是降级路径 —— 构建照常完成、产物少一截。
	projectID string
	cache     map[string]*builder.Page
	errs      map[string]error
}

// newBlockResolverAdapter 构造单次编译的块解析适配器（缓存随编译实例存活）。
func newBlockResolverAdapter(blocks blockcontract.BlockService, ctx context.Context, projectID string) *blockResolverAdapter {
	return &blockResolverAdapter{
		blocks: blocks, ctx: ctx, projectID: projectID,
		cache: map[string]*builder.Page{}, errs: map[string]error{},
	}
}

// ResolveBlockRoot 实现 core.BlockResolver：按块 ID 返回块文档 root 节点。
func (a *blockResolverAdapter) ResolveBlockRoot(blockID string) ([]*core.Node, error) {
	page, err := a.blockPage(blockID)
	if err != nil {
		return nil, err
	}
	return page.Root, nil
}

// blockPage 解析块文档为 builder.Page（带缓存）。
func (a *blockResolverAdapter) blockPage(blockID string) (*builder.Page, error) {
	if page, ok := a.cache[blockID]; ok {
		return page, nil
	}
	if err, ok := a.errs[blockID]; ok {
		return nil, err
	}
	fail := func(err error) (*builder.Page, error) {
		a.errs[blockID] = err
		return nil, err
	}
	if a.blocks == nil {
		return fail(fmt.Errorf("全局块 %s 不可用（block 契约未装配）", blockID))
	}
	block, err := a.blocks.Detail(a.ctx, &blockcontract.DetailReq{ProjectID: a.projectID, ID: blockID})
	if err != nil || block == nil {
		return fail(fmt.Errorf("全局块 %s 不可用", blockID))
	}
	if block.ReuseMode == "template" {
		return fail(fmt.Errorf("全局块 %s 为一次性复制片段，不能被引用展开", blockID))
	}
	page, err := builder.ParsePage(block.Document)
	if err != nil {
		return fail(err)
	}
	a.cache[blockID] = page
	return page, nil
}

// 编译期断言：适配器实现 core.BlockResolver。
var _ core.BlockResolver = (*blockResolverAdapter)(nil)

// structureSlotOptions 把 settings.structure 快照转成编译期的结构槽位绑定（审计 VIS-001）。
//
// 与 page 模块同名函数同义：空绑定不产生 opt，产物与改造前逐字节一致。
func structureSlotOptions(s builder.StructureBindings) []builder.CompileOption {
	bindings := s.SlotBindings()
	if len(bindings) == 0 {
		return nil
	}
	slots := make([]builder.StructureSlot, 0, len(bindings))
	for _, slot := range builder.SortedSlots(bindings) {
		slots = append(slots, builder.StructureSlot{Slot: slot, BlockID: bindings[slot]})
	}
	return []builder.CompileOption{builder.WithStructureSlots(slots...)}
}
