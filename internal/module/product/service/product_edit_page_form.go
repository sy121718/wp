// product_edit_page_form.go — 编辑表单的回显值（状态选中项、属性组勾选态、
// 图集与数值字段的文本形态、捆绑容器标记）。
//
// 为什么在 service：这些都是「业务数据在表单里的表示」—— 换算是规则，不是渲染。
// 控制器各写一遍就会在两屏之间出现差异（同一商品，列表页与编辑页的状态选项不同），
// 而差异不会报错、只让运营以为数据不一致。
package productservice

import (
	"slices"
	"strings"

	productdto "go_wp/internal/module/product/dto"
	productmodel "go_wp/internal/module/product/model"
)

// IsBundleProduct 商品类型是否为捆绑容器（详情页据此决定要不要渲染「捆绑构成」）。
//
// 判据只认类型本身，不看「bundle_items 是否非空」—— 后者是迁移 238 之前的旧口径，
// 分不清「主体卖自己的 SKU」与「容器卖组合」（bundle 商品的价 0 变体就是这么来的）。
func IsBundleProduct(productType string) bool {
	return productType == productmodel.TypeBundle
}

// CheckedAttributeOptions 属性组勾选列表：给每组补一个 Checked（商品是否引用它）。
//
// 内嵌 AttributeOptionResp 而不是复制键：标签口径仍只有一处，
// 这里只加「这一行在本次商品上是什么状态」。
func CheckedAttributeOptions(options []productdto.AttributeOptionResp, selected []string) []productdto.AttributeCheckOptionResp {
	out := make([]productdto.AttributeCheckOptionResp, 0, len(options))
	for _, o := range options {
		out = append(out, productdto.AttributeCheckOptionResp{
			AttributeOptionResp: o,
			Checked:             slices.Contains(selected, o.ID),
		})
	}
	return out
}

// FillProductEditFormEcho 编辑表单的回显值。
//
// 商品读不出来时页面仍要完整：状态按「全部状态」那一档给默认值，其余回显位保持零值，
// 模板据此显示空表单而不是中断。
func FillProductEditFormEcho(view *productdto.ProductEditPageResp) {
	if view == nil {
		return
	}
	// 状态下拉：先按「全部状态」那一档给默认值 —— 商品读不出来时页面仍完整；读到了会覆盖。
	view.Statuses = ProductStatusOptions("")
	if !view.HasProduct || view.Detail == nil {
		return
	}
	// 行已由读模型装配（service/product_row.go）：副标题 / 单位 / 描述 HTML 都是行上的
	// 字段，这里只补「编辑表单要、列表与详情页不要」的那几样。
	view.Statuses = ProductStatusOptions(view.Detail.Status)
	// 属性组勾选态（详情页用的是逗号分隔的 id 输入框，编辑页是勾选列表）：
	// 可选值来自读模型给的候选，勾选态由商品已引用的 id 决定。
	// 候选读失败那一档走 EditBlocked（AttributeChecks 保持为空）。
	if view.AttributeOptionsErr == nil {
		view.AttributeChecks = CheckedAttributeOptions(view.AttributeOptions, view.Detail.AttributeIDs)
	}
	// 图集与数值字段：表单里是文本（textarea / input），按行与可空文本回填。
	view.ImagesText = strings.Join(view.Detail.Images, "\n")
	view.ImageAltsText = strings.Join(view.Detail.ImageAlts, "\n")
	view.WeightText = NullableNumberText(view.Detail.Weight)
	view.DefaultPriceText = NullableNumberText(view.Detail.DefaultPrice)
	// 捆绑容器：构成表在详情页只读展示，编辑入口在独立页 /admin/products/bundle ——
	// 这里只给一个链接标记（type=variant 时为假，模板据 isset 整块跳过）。
	view.IsBundle = IsBundleProduct(view.Detail.Type)
}
