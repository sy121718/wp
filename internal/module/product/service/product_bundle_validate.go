// product_bundle_validate.go — 捆绑整单的硬校验与实时算价（issue #20 验收 3/4/5/6）。
//
// 一个入口服务两个消费者：前台的即时反馈（fragment 片段）与后端的硬拦截（API）。
// 两者调用的是**同一份校验**，所以「绕过前端直接构造请求」得到的结论与前台完全一致 ——
// 这正是验收 4 要的「前后端都拦」：前端拦的是体验，后端拦的是正确性。
//
// 校验是纯读的：不写库、不预占库存、不留痕（加购 / 下单 / 预占属订单域）。
package productservice

import (
	"context"
	"errors"
	"strings"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
)

// BundleValidate 供 runtimefragment 与后台预览复用的校验入口（契约的 BundleConfiguratorPort）。
//
// 直接就是 ValidateBundleSelection 的别名语义：片段处理器拿到的是同一个结论。
func (s *Service) BundleConfiguratorData(ctx context.Context, productID string) (res *productdto.BundleConfigResp, err error) {
	return s.GetBundleConfig(ctx, &productdto.GetBundleConfigReq{ProductID: productID})
}

// ValidateBundleSelection 整单校验 + 算价。
//
// 拒绝顺序刻意从「配置之外」到「配置之内」再到「库存」：先认出根本不属于这个套餐的 SKU，
// 再逐项判数量区间，最后看库存。这样错误信息总是最具体的那一条，
// 而不是被一个笼统的「选择不合法」盖住。
func (s *Service) ValidateBundleSelection(ctx context.Context, req *productdto.ValidateBundleSelectionReq) (res *productdto.BundleSelectionResp, err error) {
	if req == nil || strings.TrimSpace(req.ProductID) == "" {
		return nil, errors.New(productenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	e, gerr := s.m.Get(ctx, strings.TrimSpace(req.ProductID), projectID)
	if gerr != nil {
		return nil, mapNotFound(gerr)
	}
	cfg := decodeBundleConfig(e.BundleItems)
	if len(cfg.Options) == 0 {
		return nil, errors.New(productenums.ErrBundleNotConfigured)
	}

	// 入参索引：同一 SKU 重复出现一律拒绝（数量必须是一项一条，避免「合并还是不合并」的歧义）。
	chosen := make(map[string]int, len(req.Items))
	for _, it := range req.Items {
		vid := strings.TrimSpace(it.VariantID)
		if vid == "" {
			return nil, errors.New(productenums.ErrBundleVariantRequired)
		}
		if _, dup := chosen[vid]; dup {
			return nil, errors.New(productenums.ErrBundleVariantDuplicated)
		}
		if it.Qty < 0 || it.Qty > productdto.BundleMaxQtyLimit {
			return nil, errors.New(productenums.ErrBundleQtyInvalid)
		}
		chosen[vid] = it.Qty
	}
	optByVariant := make(map[string]productdto.BundleOption, len(cfg.Options))
	for _, o := range cfg.Options {
		optByVariant[o.VariantID] = o
	}
	for vid := range chosen {
		if _, ok := optByVariant[vid]; !ok {
			return nil, errors.New(productenums.ErrBundleVariantNotInConfig)
		}
	}

	// 逐项校验 + 汇总总件数。
	selected := make([]productdto.BundleSelectItem, 0, len(cfg.Options))
	total := 0
	for _, o := range cfg.Options {
		qty, ok := chosen[o.VariantID]
		if !ok {
			if o.Required {
				return nil, errors.New(productenums.ErrBundleOptionRequired)
			}
			continue
		}
		if qty == 0 && !o.Required && o.MinQty == 0 {
			// 可选项填 0 = 不选这一项（不是「选了 0 件」）。
			continue
		}
		if qty < o.MinQty {
			return nil, errors.New(productenums.ErrBundleQtyBelowMin)
		}
		if o.MaxQty > 0 && qty > o.MaxQty {
			return nil, errors.New(productenums.ErrBundleQtyAboveMax)
		}
		total += qty
		selected = append(selected, productdto.BundleSelectItem{VariantID: o.VariantID, Qty: qty})
	}
	if total < cfg.MinTotalQty {
		return nil, errors.New(productenums.ErrBundleTotalBelowMin)
	}
	if cfg.MaxTotalQty > 0 && total > cfg.MaxTotalQty {
		return nil, errors.New(productenums.ErrBundleTotalAboveMax)
	}

	// 库存：可用量只读真源（端口未注入即 fail-closed，绝不按「无限制」放行）。
	ids := make([]string, 0, len(selected))
	for _, it := range selected {
		ids = append(ids, it.VariantID)
	}
	avail, aerr := s.availableQuantities(ctx, e.ProjectID, ids)
	if aerr != nil {
		return nil, aerr
	}
	for _, it := range selected {
		if it.Qty > avail[it.VariantID] {
			return nil, errors.New(productenums.ErrBundleQtyAboveStock)
		}
	}

	// 展开结果：订单侧将来必须快照这份结果（哪些 SKU、各多少），而不是引用商品当前的 BOM。
	details, derr := s.bundleSelectedItems(ctx, projectID, ids, avail)
	if derr != nil {
		return nil, derr
	}
	res = &productdto.BundleSelectionResp{
		ProductID:  e.ID,
		Items:      make([]*productdto.BundleSelectedItem, 0, len(selected)),
		TotalQty:   total,
		TotalPrice: s.bundleBasePrice(ctx, e),
	}
	for _, it := range selected {
		d := details[it.VariantID]
		if d == nil {
			return nil, errors.New(productenums.ErrBundleVariantNotFound)
		}
		d.Qty = it.Qty
		if d.CostPrice != nil {
			res.TotalCost += *d.CostPrice * float64(it.Qty)
		}
		res.Items = append(res.Items, d)
	}
	return res, nil
}

// bundleSelectedItems 批量组装展开行的 SKU 快照（价格与成本取当前值）。
//
// projectID 由调用方给出（ValidateBundleSelection 已解析）：SKU 快照要回填商品名，
// 读的是 products（迁移 215 名单），缺作用域时商品名整列为空。
func (s *Service) bundleSelectedItems(ctx context.Context, projectID string, variantIDs []string, avail map[string]int) (out map[string]*productdto.BundleSelectedItem, err error) {
	out = make(map[string]*productdto.BundleSelectedItem, len(variantIDs))
	if len(variantIDs) == 0 {
		return out, nil
	}
	variants, verr := s.m.ListVariantsByIDs(ctx, variantIDs)
	if verr != nil {
		return nil, verr
	}
	productIDs := make([]string, 0, len(variants))
	for _, v := range variants {
		productIDs = append(productIDs, v.ProductID)
	}
	owners, oerr := s.m.ListProductsByIDs(ctx, productIDs, projectID)
	if oerr != nil {
		return nil, oerr
	}
	nameOf := make(map[string]string, len(owners))
	for _, p := range owners {
		nameOf[p.ID] = p.Name
	}
	for _, v := range variants {
		out[v.ID] = &productdto.BundleSelectedItem{
			VariantID:   v.ID,
			SKUCode:     v.SKUCode,
			ProductName: nameOf[v.ProductID],
			// 成员单价恒为 0：成员价**不参与任何对外金额**（套餐只有容器价，见产品类型不变量）。
			// 这里不是「查不到价」，而是「成员在套餐里没有价」—— 订单行照此快照即天然不会
			// 把成员价加进合计；成员原价只在后台配置器（BundleOptionDetail.ItemPrice）可见。
			UnitPrice: 0,
			// 成员挂牌价单独放 MemberPrice：它只是参考值，绝不参与金额计算。
			MemberPrice: v.Price,
			// 成本保留：后台毛利口径要它（TotalCost = 成员成本合计 × 数量），不对访客露出。
			CostPrice: v.CostPrice,
			Available: avail[v.ID],
		}
	}
	return out, nil
}

// 编译期断言：片段侧只需要两个方法，单独一个窄接口（见 contract/product_bundle.go）。
var _ interface {
	BundleConfiguratorData(ctx context.Context, productID string) (*productdto.BundleConfigResp, error)
} = (*Service)(nil)
