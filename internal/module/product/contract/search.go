package productcontract

// search.go — 商品检索的**收窄只读端口**（BIZ-2 站内搜索）。
//
// 与 VariantSnapshotPort / VariantAvailabilityLookupPort 同属「按消费方需要收窄」：
// 搜索片段只需要「按关键词取一批已上架商品」这一条只读能力，而 ProductService 有
// 三十来个方法（且含全部写方法）。接口形状即越权防护 —— 片段层拿不到写能力。

import "context"

// ProductSearchHit 一个商品的检索命中（只读展示事实）。
//
// 只给结果条目需要的那几样：id（供发布面反查线上路径）、名称、副标题、slug、主图。
// 描述 / 价格 / 库存都不进来：价格在变体上是运行期事实（详情页价格另有实时核对片段），
// 把价烘进搜索结果等于再发布一份会过期的副本。
type ProductSearchHit struct {
	ID           string
	Name         string
	Subtitle     string
	Slug         string
	DefaultImage string
}

// SearchPort 商品检索能力（只读、限量）。
type SearchPort interface {
	// SearchPublishedProducts 在工程内检索**已上架**商品，最多返回 limit 条
	// （limit ≤ 0 时由实现取默认上限）。projectID 为空时返回空结果（不报错）：
	// 片段参数来自 URL，缺工程上下文时宁可不返回，也不要跨工程搜。
	SearchPublishedProducts(ctx context.Context, projectID, keyword string, limit int) (hits []*ProductSearchHit, err error)
}
