package pageservice

import (
	"context"

	pagedto "go_wp/internal/module/page/dto"
)

// List 列出全部页面（摘要投影，不含草稿文档；DraftDocument 为空）。
func (s *Service) List(ctx context.Context) (res []pagedto.PageResp, err error) {
	entities, err := s.model.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	res = make([]pagedto.PageResp, 0, len(entities))
	for i := range entities {
		res = append(res, *pageResp(&entities[i]))
	}
	return res, nil
}
