package pageservice

import (
	"context"

	pagedto "go_wp/internal/module/page/dto"
)

// List 列出页面摘要（不含草稿文档；DraftDocument 为空）。
// themeID 为空时列全部；非空时只列挂在该主题下的页面。
func (s *Service) List(ctx context.Context, themeID string) (res []pagedto.PageResp, err error) {
	entities, err := s.model.ListAll(ctx, themeID)
	if err != nil {
		return nil, err
	}
	res = make([]pagedto.PageResp, 0, len(entities))
	for i := range entities {
		res = append(res, *pageResp(&entities[i]))
	}
	return res, nil
}
