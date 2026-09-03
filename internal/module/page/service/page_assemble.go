package pageservice

// 装配编译（方案 C，021_blocks.sql）：内核 CompileFn 注入。
// 页面文档 settings.structure 快照了主题的页眉/页脚块绑定，
// 构建时在此拉取块文档分别编译，HTML/CSS 拼接进页面产物——
// 访问面保持纯静态（无运行时拼接），块内容变更通过 stale 传播触发重建。
// 页面文档内的 core.globalref 节点经 BlockResolver 同样内联展开。

import (
	"context"
	"encoding/json"
	"fmt"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	blockdto "go_wp/internal/module/block/dto"
	"go_wp/internal/pipeline"
)

// assembleCompile 装配感知编译：页眉块 + 页面主体 + 页脚块。
// 内容引用面只存 URL 快照，构建期零解析（不查媒体库）。
// 无绑定无引用时输出与默认编译字节一致（hash 兼容历史产物）；
// 块文档缺失/非法降级为空片段，不阻塞构建主链。
func (s *Service) assembleCompile(docJSON []byte) ([]byte, error) {
	ctx := context.Background()
	page, err := builder.ParsePage(docJSON)
	if err != nil {
		return pipeline.DefaultCompile(docJSON)
	}
	resolver := blockResolverAdapter{s: s}
	opts := []builder.CompileOption{builder.WithBlockResolver(resolver)}
	compiled, err := builder.Compile(page, opts...)
	if err != nil {
		return nil, err
	}
	structure, err := parseStructureBindings(docJSON)
	if err != nil {
		structure = builder.StructureBindings{}
	}
	headerHTML, headerCSS := s.compileBlockFragment(ctx, structure.HeaderBlockID)
	footerHTML, footerCSS := s.compileBlockFragment(ctx, structure.FooterBlockID)
	// 页眉在主体前、页脚在主体后；三段 CSS 为独立规则集，顺序拼接。
	compiled.HTML = headerHTML + compiled.HTML + footerHTML
	compiled.CSS = headerCSS + compiled.CSS + footerCSS
	return []byte(builder.RenderDocument(compiled)), nil
}

// parseStructureBindings 从页面文档读取全局块绑定快照（无该键时返回零值）。
func parseStructureBindings(docJSON []byte) (b builder.StructureBindings, err error) {
	var page struct {
		Settings struct {
			Structure builder.StructureBindings `json:"structure"`
		} `json:"settings"`
	}
	if err = json.Unmarshal(docJSON, &page); err != nil {
		return b, err
	}
	return page.Settings.Structure, nil
}

// blockRootResolverAdapter 适配 block 契约为 builder 的 BlockResolver
// （core.globalref 构建期展开引用块内容）。
type blockResolverAdapter struct {
	s *Service
}

// ResolveBlockRoot 按块 ID 返回块文档 root 节点。
func (a blockResolverAdapter) ResolveBlockRoot(blockID string) ([]*core.Node, error) {
	block, err := a.s.blocks.Detail(context.Background(), &blockdto.DetailReq{ID: blockID})
	if err != nil || block == nil {
		return nil, fmt.Errorf("全局块 %s 不可用", blockID)
	}
	page, err := builder.ParsePage(block.Document)
	if err != nil {
		return nil, err
	}
	return page.Root, nil
}
