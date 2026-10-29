package service

import (
	"context"
	"strconv"
	"strings"

	blockdto "go_wp/internal/module/block/dto"
	workbenchdto "go_wp/internal/module/workbench/dto"
	workbenchenums "go_wp/internal/module/workbench/enums"
)

// BlockDraftForPreview 只读取编辑目标，确认工程与并发版本；草稿不进入任何持久化写链。
func (s *Service) BlockDraftForPreview(ctx context.Context, req *workbenchdto.BlockPreviewDraftReq) (*blockdto.BlockResp, error) {
	version, err := strconv.ParseInt(strings.TrimSpace(req.ExpectedVersion), 10, 64)
	if err != nil || version < 1 || strings.TrimSpace(req.ID) == "" || strings.TrimSpace(req.ProjectID) == "" || strings.TrimSpace(req.DraftDocument) == "" {
		return nil, workbenchenums.PreviewError(workbenchenums.ErrDraftDecodeFailed)
	}
	block, err := s.BlockByID(ctx, strings.TrimSpace(req.ID))
	if err != nil {
		return nil, err
	}
	if block == nil || block.ProjectID != strings.TrimSpace(req.ProjectID) {
		return nil, workbenchenums.PreviewError(workbenchenums.ErrBlockNotFound)
	}
	if block.Version != version {
		return nil, workbenchenums.PreviewError(workbenchenums.ErrDraftVersionStale)
	}
	return block, nil
}
