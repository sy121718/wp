// product_req.go — product 模块入参（inbound 绑定用）。
package productdto

import "encoding/json"

// CreateReq 新建商品。
//
// 关联关系一律走 JSON 数组（图片 URL / 分类 id / 标签 id / 关联商品 id / 捆绑 BOM），
// 见 spec §数据结构精简原则：不为每类关系单开关联表。
type CreateReq struct {
	ProjectID      string          `json:"projectId"`
	Name           string          `json:"name" binding:"required"`
	Subtitle       string          `json:"subtitle"`
	Description    json.RawMessage `json:"description"`
	Slug           string          `json:"slug"`
	Unit           string          `json:"unit"`
	Weight         *float64        `json:"weight"`
	SEOTitle       string          `json:"seoTitle"`
	SEODescription string          `json:"seoDescription"`
	Images         []string        `json:"images"`
	// ImageAlts 图集 alt 文本（issue #12）：与 Images 逐位对应，元素可为空串。
	// 单独成字段而不是把 alt 塞进 URL 字符串：alt 是作者填写的文本，参与内容翻译
	// （语境 product.imageAlts），URL 永不翻译（验收 4/5）。
	ImageAlts []string `json:"imageAlts"`
	// AttributeIDs 引用的属性组 id（issue #7）：只存引用，组与值的定义唯一一份，
	// 同一属性组可被多个商品复用。
	AttributeIDs []string `json:"attributeIds"`
	CategoryIDs  []string `json:"categoryIds"`
	// PrimaryCategoryID 主分类（issue #10）。主分类必然是附属分类之一：
	// 显式指定但它不在 CategoryIDs 里时由服务端自动纳入，不变量始终成立。
	PrimaryCategoryID string          `json:"primaryCategoryId"`
	TagIDs            []string        `json:"tagIds"`
	RelatedIDs        []string        `json:"relatedIds"`
	BundleItems       json.RawMessage `json:"bundleItems"`
	// BrandID 品牌引用（issue #10）：必须是同工程内真实存在的品牌，空串即不指定。
	BrandID      string          `json:"brandId"`
	DefaultPrice *float64        `json:"defaultPrice"`
	DefaultImage string          `json:"defaultImage"`
	Metadata     json.RawMessage `json:"metadata"`
}

// UpdateReq 修改商品（含 slug 改名；变体单独接口）。
type UpdateReq struct {
	ID             string          `json:"id" binding:"required"`
	Name           *string         `json:"name"`
	Subtitle       *string         `json:"subtitle"`
	Description    json.RawMessage `json:"description"`
	Slug           *string         `json:"slug"`
	Status         *string         `json:"status"`
	Unit           *string         `json:"unit"`
	Weight         *float64        `json:"weight"`
	SEOTitle       *string         `json:"seoTitle"`
	SEODescription *string         `json:"seoDescription"`
	Images         []string        `json:"images"`
	// ImageAlts 图集 alt 文本（issue #12）；nil 表示本次不改，空数组表示全部清空
	// （与 Images 的「整体替换」语义一致）。
	ImageAlts []string `json:"imageAlts"`
	// AttributeIDs 引用的属性组 id（issue #7）；nil 表示本次不改引用，
	// 空数组表示解绑全部（与 CategoryIDs / TagIDs 同为「整体替换」语义）。
	AttributeIDs []string `json:"attributeIds"`
	CategoryIDs  []string `json:"categoryIds"`
	// PrimaryCategoryID 主分类（issue #10）：nil 表示本次不改，指向空串表示解绑。
	// 附属分类被整体替换且新列表里没有原主分类时，主分类自动解绑（不变量维护）。
	PrimaryCategoryID *string         `json:"primaryCategoryId"`
	TagIDs            []string        `json:"tagIds"`
	RelatedIDs        []string        `json:"relatedIds"`
	BundleItems       json.RawMessage `json:"bundleItems"`
	BrandID           *string         `json:"brandId"`
	DefaultPrice      *float64        `json:"defaultPrice"`
	DefaultImage      *string         `json:"defaultImage"`
	Metadata          json.RawMessage `json:"metadata"`
}

// GetReq 按 ID 查询。
type GetReq struct {
	ID string `form:"id" binding:"required"`
}

// ListReq 商品列表（分页 + 可选过滤）。
type ListReq struct {
	ProjectID string `form:"projectId"`
	Keyword   string `form:"keyword"`
	Status    string `form:"status"`
	Page      int    `form:"page"`
	Size      int    `form:"size"`
}

// DeleteReq 删除商品（连带其变体）。
type DeleteReq struct {
	ID string `json:"id" binding:"required"`
}

// CreateVariantReq 为商品新增一个变体。
//
// 未填字段由 service 逐字段判空后继承商品级默认值（空值以 NULL 判定，0 与 false 视为已填）。
type CreateVariantReq struct {
	ProductID    string          `json:"productId" binding:"required"`
	SKUCode      string          `json:"skuCode"`
	Barcode      string          `json:"barcode"`
	Price        *float64        `json:"price"`
	ComparePrice *float64        `json:"comparePrice"`
	CostPrice    *float64        `json:"costPrice"`
	Image        string          `json:"image"`
	OptionValues json.RawMessage `json:"optionValues"`
	Enabled      *bool           `json:"enabled"`
	Sort         int             `json:"sort"`
}

// UpdateVariantReq 修改变体（编辑路径不做默认值填充）。
type UpdateVariantReq struct {
	ID           string          `json:"id" binding:"required"`
	SKUCode      *string         `json:"skuCode"`
	Barcode      *string         `json:"barcode"`
	Price        *float64        `json:"price"`
	ComparePrice *float64        `json:"comparePrice"`
	CostPrice    *float64        `json:"costPrice"`
	Image        *string         `json:"image"`
	OptionValues json.RawMessage `json:"optionValues"`
	Enabled      *bool           `json:"enabled"`
	Sort         *int            `json:"sort"`
}

// DeleteVariantReq 删除变体。
type DeleteVariantReq struct {
	ID string `json:"id" binding:"required"`
}

// VariantSelectionReq 一组被勾选的属性值（issue #8）。
//
// AttributeID 必须是该商品已引用、且 IsVariation=true 的属性组；ValueIDs 是组内
// 被勾选的属性值 id（只允许组内**启用**的值）。重复的值 id 会在生成时去重，
// 因此「重复勾选」不会产生重复变体。
type VariantSelectionReq struct {
	AttributeID string   `json:"attributeId"`
	ValueIDs    []string `json:"valueIds"`
}

// GenerateVariantsReq 按勾选的属性值生成全部变体组合（issue #8）。
//
// 三条调用路径共用同一份语义（后台表单 / 批量生成 / 导入接口）：
//   - Selections 里出现的属性组按勾选的值参与组合（顺序固定为组内定义顺序，
//     重复勾选同一个值会被去重）；
//   - Selections 里没出现的属性组取**全部启用值**（勾了颜色不勾尺码 = 颜色按勾选的来、
//     尺码取全部）—— 生成的组合恒覆盖全部参与变体的维度，不会产出只有部分维度的
//     「半截组合」；
//   - Selections 为空 = 无表单路径：全部参与变体的属性组 × 全部启用值。
//
// 保护性上限：参与维度与组合总数分别有上限，超过时整体拒绝，一条变体都不写。
// 已存在的规格组合一律跳过（幂等），因此重复提交不会产生重复变体。
type GenerateVariantsReq struct {
	ProductID  string                `json:"productId" binding:"required"`
	Selections []VariantSelectionReq `json:"selections"`
}
