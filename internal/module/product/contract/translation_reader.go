package productcontract

import (
	"context"

	productdto "go_wp/internal/module/product/dto"
)

// ProductTranslationsReader 商品译文工作台（/admin/products/translations）的屏幕读模型与保存编排。
//
// 为什么不并进 ProductService：这个接口的消费者只有译文页处理器，而 ProductService 是对外
// 能力面（AI 工具、其他模块的只读端口都挂在它上面）。多一条屏幕读模型就多一个「别的模块
// 也能调到」的入口，而这一屏的产物是**页面视图**（模板键的形状），不是业务结论。
//
// 两个方法都在同一个接口里（同 ProductPageReader 的理由）：处理器拿到的是一份能力，
// 少实现一个方法就是运行期断言失败 → 整页静默走兜底出口。
type ProductTranslationsReader interface {
	// ProductTranslationsPage 工作台整屏：工程选项 → 站点语言（源语言 + 目标语言）→
	// 商品域候选 → 现有译文 → 分组装配。工程清单读不到时返回 error（调用方走提示页出口）。
	//
	// 其余取数各自降级：语言清单不可读回退站点默认语言、译文明细读不到按「全部未译」渲染 ——
	// 它们不构成「这一页打不开」，只是少了一部分内容。
	ProductTranslationsPage(ctx context.Context, req *productdto.ProductTranslationsPageReq) (res *productdto.ProductTranslationsPageView, err error)

	// SaveProductTranslations 保存译文（整表提交）的编排：重建数据 → 目标语言白名单校验 →
	// 逐行校验（全部通过才写库，不留「部分成功」的中间态）→ 变更判定（幂等，重复保存零写入）
	// → 写入 → 按依赖标记待重建。
	//
	// 返回的视图里 Saved / Written 是保存结论（控制器据此走提示页），Errors 是逐行结论
	//（非空时控制器重渲本页）。只有「重建数据」失败才返回 error。
	SaveProductTranslations(ctx context.Context, req *productdto.SaveProductTranslationsReq) (res *productdto.ProductTranslationsPageView, err error)
}
