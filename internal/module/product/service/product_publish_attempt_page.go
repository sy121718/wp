package productservice

// product_publish_attempt_page.go — 发布记录页的「发布历史」面板取数（迁移 671）。
//
// 这一格回答的是「这个商品都试过哪几次发布、谁点的、为什么没成」。台账在 presentation
// 模块（presentation_publish_attempts），商品侧只做工程解析与入参归一，翻译在
// outbound/publishrecord。
//
// 交 JSON 而不是 HTML 片段（internal/templates/CLAUDE.md §json_swap）：前端用页面里
// 已渲染的 Jet <template> 填 DOM，后端只给事实（状态给码值，时间给已格式化的文本 ——
// 前端与模板都没有第二份时间格式化实现）。

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
	projects, err := s.project.List(ctx)
	if err != nil {
		return nil, err
	}
	selected := ""
	if req != nil {
		selected = strings.TrimSpace(req.ProjectID)
	}
	if selected == "" && len(projects) > 0 {
		selected = projects[0].ID
	}
	if selected == "" || !ProjectExists(projects, selected) {
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
	want := pageNo * size
	if want > publishAttemptMaxSize {
		want = publishAttemptMaxSize
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
	pg, err := s.pagePublishAttempts.PublishAttempts(ctx, selected, productID, want)
	if err != nil {
		return nil, err
	}
	res.Rows = pg.Rows
	res.Total = pg.Total
	res.HasMore = pg.HasMore
	return res, nil
}
