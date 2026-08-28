package pageservice

// 装配编译（方案 C，021_blocks.sql）：内核 CompileFn 注入。
// 页面文档 settings.structure 快照了主题的页眉/页脚块绑定，
// 构建时在此拉取块文档分别编译，HTML/CSS 拼接进页面产物——
// 访问面保持纯静态（无运行时拼接），块内容变更通过 stale 传播触发重建。

import (
	"context"
	"encoding/json"

	"go_wp/internal/builder"
	"go_wp/internal/pipeline"
)

// assembleCompile 装配感知编译：页眉块 + 页面主体 + 页脚块。
// 输出确定性：文档字节 + 构建时刻的块文档状态共同决定产物（hash 涵盖块内容）。
// 块绑定缺失/文档非法时退回默认编译（装配为可选增强，不阻塞构建主链）。
func (s *Service) assembleCompile(docJSON []byte) ([]byte, error) {
	ctx := context.Background()
	structure, err := parseStructureBindings(docJSON)
	if err != nil || (structure.HeaderBlockID == "" && structure.FooterBlockID == "") {
		return pipeline.DefaultCompile(docJSON)
	}
	page, err := builder.ParsePage(docJSON)
	if err != nil {
		return pipeline.DefaultCompile(docJSON)
	}
	compiled, err := builder.Compile(page)
	if err != nil {
		return nil, err
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
