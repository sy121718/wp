package pageservice

import (
	"context"
	"strings"

	pagedto "go_wp/internal/module/page/dto"
	"go_wp/pkg/utils"
)

// List 列出页面摘要（不含草稿文档；DraftDocument 为空）。
// 必须带 projectID；themeID 为空时列该工程全部，非空时只列挂在该主题下的页面。
func (s *Service) List(ctx context.Context, req *pagedto.ListReq) (res []pagedto.PageResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, ErrInvalidParam
	}
	if err = s.requireProject(ctx, req.ProjectID); err != nil {
		return nil, err
	}
	entities, err := s.model.ListAll(ctx, req.ProjectID, req.ThemeID)
	if err != nil {
		return nil, err
	}
	res = make([]pagedto.PageResp, 0, len(entities))
	for i := range entities {
		res = append(res, *pageResp(&entities[i]))
	}
	return res, nil
}

// ListDrafts 列出全部未删除页面的草稿文档（多语言 P5c 翻译工作台的全站扫描）。
//
// 只读投影：供工作台按构建期同一套白名单（builder.CollectContentCandidates）
// 统计「同一译文还用在哪些页面」与「全站翻译完成度」。调用方负责缓存（一次扫描
// 读全站草稿 JSONB，代价见 docs/06-D §7.8 与 §15.12）。
func (s *Service) ListDrafts(ctx context.Context) (res []pagedto.PageDraftResp, err error) {
	entities, err := s.model.ListDraftDocuments(ctx)
	if err != nil {
		return nil, err
	}
	res = make([]pagedto.PageDraftResp, 0, len(entities))
	for i := range entities {
		res = append(res, pagedto.PageDraftResp{
			ID: entities[i].ID, ProjectID: entities[i].ProjectID,
			DraftPath: entities[i].DraftPath, DraftDocument: entities[i].DraftDocument,
			UpdatedAt: utils.NewJSONTime(entities[i].UpdatedAt),
		})
	}
	return res, nil
}
