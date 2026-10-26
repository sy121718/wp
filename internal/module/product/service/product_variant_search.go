package productservice

import (
	"context"
	"strings"

	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
)

// product_variant_options.go — 后台选品下拉的候选变体（跨商品）。
//
// 消费者是订单代客建单页的候选区（internal/module/order/inbound/http/order_create_page.go）：
// 运营输入商品名缩小范围，从结果里挑一行入单。与「商品 → 变体」那条读路径的分工：
// 这里要的是**跨商品平铺**的列表，所以先按关键词取商品、再批量取这些商品的变体，
// 一次 SQL 拿全（不按商品逐个查，避免 N+1）。

// ListVariantOptions 可选的变体清单（只列启用中的变体）。
//
// 为什么只列启用中的：停用的 SKU 挂进订单会变成「选得了又发不出」的死行 ——
// 要卖先回商品页把它启用。这条口径与前台可售集合一致，不另立一套。
func (s *Service) ListVariantOptions(ctx context.Context, req *productdto.ListVariantOptionReq) (list []*productdto.VariantOptionResp, err error) {
	if req == nil {
		req = &productdto.ListVariantOptionReq{}
	}
	projectID, perr := s.resolveProjectID(ctx, req.ProjectID)
	if perr != nil {
		return nil, perr
	}
	products, lerr := s.m.List(ctx, projectID, strings.TrimSpace(req.Keyword), "", maxPageSize, 0)
	if lerr != nil {
		return nil, lerr
	}
	if len(products) == 0 {
		return []*productdto.VariantOptionResp{}, nil
	}
	ids := make([]string, 0, len(products))
	nameOf := make(map[string]string, len(products))
	for _, p := range products {
		ids = append(ids, p.ID)
		nameOf[p.ID] = p.Name
	}
	variants, verr := s.m.ListVariantsByProducts(ctx, ids)
	if verr != nil {
		return nil, verr
	}
	list = make([]*productdto.VariantOptionResp, 0, len(variants))
	for _, v := range variants {
		if !v.Enabled {
			continue
		}
		list = append(list, &productdto.VariantOptionResp{
			VariantID: v.ID, SKUCode: v.SKUCode, ProductID: v.ProductID,
			ProductName: nameOf[v.ProductID], Price: v.Price, CostPrice: v.CostPrice, Enabled: v.Enabled,
		})
	}
	return list, nil
}

// ListAllVariantOptions 全工程启用中的变体清单（库存 / 采购下拉的数据源）。
//
// 与 ListVariantOptions 的分工只在**筛选**：那条按关键词收窄（订单代客建单页），
// 这条要全量（下拉里漏掉的 SKU 等于不存在）。取数与装配共用同一套批量读法。
func (s *Service) ListAllVariantOptions(ctx context.Context, projectID string) ([]productcontract.VariantOption, error) {
	// 空工程**不解释成「全部工程」**：那会让一个工程的页面列出所有工程的 SKU。
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	list, err := s.ListVariantOptions(ctx, &productdto.ListVariantOptionReq{ProjectID: projectID})
	if err != nil {
		return nil, err
	}
	out := make([]productcontract.VariantOption, 0, len(list))
	for _, o := range list {
		out = append(out, productcontract.VariantOption{
			VariantID: o.VariantID, ProductID: o.ProductID, SKUCode: o.SKUCode,
			ProductName: o.ProductName, CostPrice: o.CostPrice,
		})
	}
	return out, nil
}
