package productservice

import (
	"context"
	"strings"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
)

const (
	publishAttemptPageSize = 10
	publishAttemptMaxSize  = 100
)

func (s *Service) ProductPublishAttempts(ctx context.Context, req *productdto.PublishAttemptListReq) (*productdto.PublishAttemptListResp, error) {
	if req == nil {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	scope, err := s.ResolveCatalogScope(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	if scope.ProjectID == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}

	productID := strings.TrimSpace(req.ProductID)
	pageNo, size := 1, publishAttemptPageSize
	if req.Size > 0 {
		size = req.Size
	}
	if req.Page > 0 {
		pageNo = req.Page
	}
	if productID == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	if size > publishAttemptMaxSize {
		size = publishAttemptMaxSize
	}

	want := publishAttemptMaxSize
	if pageNo <= publishAttemptMaxSize/size {
		want = pageNo * size
	}

	res := &productdto.PublishAttemptListResp{
		Rows:  []productdto.PublishAttemptRowResp{},
		Page:  pageNo,
		Size:  size,
		Ready: s.pagePublishAttempts != nil,
	}
	if !res.Ready {
		return res, nil
	}
	pg, err := s.pagePublishAttempts.PublishAttempts(ctx, scope.ProjectID, productID, want)
	if err != nil {
		return nil, err
	}
	res.Rows = pg.Rows
	res.Total = pg.Total
	res.HasMore = pg.HasMore
	return res, nil
}
