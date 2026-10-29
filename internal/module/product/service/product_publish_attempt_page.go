package productservice

// 按工程与商品查询发布历史；界面形态由前端格式化。

import (
	"context"
	"strings"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
)

// 发布历史面板的条数档位（与 presentation 侧的 10 / 100 对齐）。
const (
	publishAttemptPageSize = 10
	publishAttemptMaxSize  = 100
)

// ProductPublishAttempts 读一个商品最近的发布尝试。
//
// 失败档位与发布记录页一致：工程清单读不到 500；?project=<不存在的 id> 与空 productId
// 是 400（参数不合法，重试没用）；台账读不到 500（这一屏每行都是事实，静默空表会把
// 「读失败」说成「一次都没发过」）。
//
// 端口未装配**不算失败**：返回 Ready=false，面板据此显示「发布历史没接上」——
// 那是装配缺陷的可见提示，与「读不到」是两档。
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

	productID := ""
	pageNo, size := 1, publishAttemptPageSize
	if req != nil {
		productID = strings.TrimSpace(req.ProductID)
		if req.Size > 0 {
			size = req.Size
		}
		if req.Page > 0 {
			pageNo = req.Page
		}
	}
	if productID == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	if size > publishAttemptMaxSize {
		size = publishAttemptMaxSize
	}
	// 「加载更多」= 取到第 N 页为止的全部行（累计），前端按 id 去重后追加。
	//
	// 为什么不是 OFFSET 分页：台账只增不删，翻页期间的写入会让 OFFSET 跳行/重行；
	// 而取「最新 N*size 条」在只增表上恒为同一批前缀 —— 重复请求不会得到不同的行，
	// 前端按 id 去重后追加即幂等。代价是每次多传已看过的行，历史总量在几十条量级。
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
