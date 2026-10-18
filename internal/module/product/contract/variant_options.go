// variant_options.go — 「商品 → 变体」下拉的只读端口（后台选 SKU 用）。
//
// 为什么单列一个口而不是让消费方调 List + Get：后台下拉要的是**全工程的变体清单**，
// 逐商品 Get 会让查询次数随商品数线性增长（100 个商品 = 101 次往返），
// 而页面本身只是画一个 select。端口给的是「一次批量取完」，实现方（product）
// 负责把查询压到与商品数无关的常数次。
//
// 只读：没有写方法。需要写变体的调用方用 ProductService（那是另一条边界）。
package productcontract

import "context"

// VariantOption 下拉里的一项：变体归属的商品名与 SKU 编码。
//
// ProductName 是**展示**用的（select 里要能看出是哪件商品），
// 但它同时是「这个变体属于哪个商品」的答案 —— 调用方不必再回查一次。
type VariantOption struct {
	VariantID   string
	ProductID   string
	SKUCode     string
	ProductName string
}

// VariantOptionsReader 全工程变体清单（按商品 sort、变体 sort 排序）。
//
// projectID 必填：空串由实现方按参数错误拒绝（不返回「全部工程」——
// 那会让一个工程的页面列出所有工程的 SKU）。
type VariantOptionsReader interface {
	ListVariantOptions(ctx context.Context, projectID string) ([]VariantOption, error)
}
