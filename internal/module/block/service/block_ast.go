package blockservice

// block_ast.go — 块文档（AST）克隆与校验。
// 文档结构以 builder 的页面文档格式为准，校验只拦结构性错误，不碰内容语义。

import (
	"context"
	"strings"

	"encoding/json"
	"go_wp/internal/builder"
	blockdto "go_wp/internal/module/block/dto"
)

// CloneAST 复制块文档为独立 AST（docs/02-D §5.2「插入-复制」动作）：
// 解析 → 公共克隆（重生成全部 Node ID）→ 返回。副本与源块脱钩，此后互不影响。
// 与 blueprint 的整页初始化复用同一 ClonePageWithNewIDs 机制（片段层级 vs 完整文档层级）。
func (s *Service) CloneAST(ctx context.Context, req *blockdto.CloneReq) (res *blockdto.CloneResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, ErrParamRequired
	}
	entity, err := s.getExistingBlock(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	page, err := builder.ParsePage(entity.Document)
	if err != nil {
		return nil, ErrInvalidDoc
	}
	out, err := json.Marshal(builder.ClonePageWithNewIDs(page))
	if err != nil {
		return nil, ErrInvalidDoc
	}
	return &blockdto.CloneResp{Document: out}, nil
}

// validateDocument 校验块文档：与页面文档同构（root 组件树），复用页面校验器。
func validateDocument(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{"settings":{"layout":{"mode":"full"}},"root":[]}`)
	}
	page, err := builder.ParsePage(raw)
	if err != nil {
		return nil, ErrInvalidDoc
	}
	if err = builder.ValidatePage(page); err != nil {
		return nil, ErrInvalidDoc
	}
	out, err := json.Marshal(page)
	if err != nil {
		return nil, ErrInvalidDoc
	}
	return out, nil
}
