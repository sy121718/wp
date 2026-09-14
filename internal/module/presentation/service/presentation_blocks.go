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
	"go_wp/internal/templates"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

// blockResolverAdapter 把 block 契约适配为 builder 的 core.BlockResolver。
type blockResolverAdapter struct {
	blocks blockcontract.BlockService
	ctx    context.Context
	cache  map[string]*builder.Page
	errs   map[string]error
}

// newBlockResolverAdapter 构造单次编译的块解析适配器（缓存随编译实例存活）。
func newBlockResolverAdapter(blocks blockcontract.BlockService, ctx context.Context) *blockResolverAdapter {
	return &blockResolverAdapter{
		blocks: blocks, ctx: ctx,
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
	block, err := a.blocks.Detail(a.ctx, &blockcontract.DetailReq{ID: blockID})
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

// compileBlockFragment 编译 settings.structure 绑定的页眉/页脚块（与 page 路径同口径）。
// lang / translator 复用本次模板编译已构造的取词器，块内不再单独查库。
func (s *Service) compileBlockFragment(ctx context.Context, blockID, lang string, translator *i18n.ContentTranslator) (html, css string) {
	if blockID == "" || s.blocks == nil {
		return "", ""
	}
	block, err := s.blocks.Detail(ctx, &blockcontract.DetailReq{ID: blockID})
	if err != nil || block == nil || len(block.Document) == 0 {
		logger.Scene("build").With("block", blockID).Error(err, "页眉/页脚块不可用")
		return "", ""
	}
	page, err := builder.ParsePage(block.Document)
	if err != nil {
		logger.Scene("build").With("block", blockID).Error(err, "块文档解析失败")
		return "", ""
	}
	set, serr := templates.NewEmbeddedComponentSet()
	if serr != nil {
		logger.Scene("build").With("block", blockID).Error(serr, "组件模板 Set 加载失败")
		return "", ""
	}
	opts := []builder.CompileOption{
		builder.WithContext(ctx), builder.WithComponentSet(set),
		builder.WithLanguage(lang), builder.WithTranslator(i18n.Snapshot(lang)),
	}
	if translator != nil {
		opts = append(opts, builder.WithContentTranslator(translator))
	}
	compiled, err := builder.Compile(page, opts...)
	if err != nil {
		logger.Scene("build").With("block", blockID).Error(err, "块编译失败")
		return "", ""
	}
	return compiled.HTML, compiled.CSS
}
