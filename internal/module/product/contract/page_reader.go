// page_reader.go — 商品后台页的读模型端口（一屏一个方法）。
//
// 消费者是商品模块自己的后台页面控制器（`inbound/http`），实现方是本模块的 service。
// 为什么单独收窄一条而不是把这些方法挂到 ProductService 上：ProductService 是本模块的
// **对外能力面**（别的模块、AI 工具、构建期数据源都在用它），而「商品新建页要取哪些数据、
// 按什么口径取、失败算 400 还是 500」是本模块页面自己的结论 —— 放上去等于把页面的
// 形状变成跨模块契约的一部分。
//
// 与 VariantAvailabilityPort 一样，端口只声明**读**方法：控制器拿到它也只能读，
// 写动作仍然只走各自的用例方法。
package productcontract

import (
	"context"

	productdto "go_wp/internal/module/product/dto"
)

// ProductPageReader 商品后台页的读模型端口（由本模块 service 实现）。
type ProductPageReader interface {
	// ProductNewPage 商品新建整页的读模型：工程清单 → 工程存在性校验 → 仓库选项 /
	// 属性组选项 / 每仓 SKU 候选 → 模板清单与默认模板，并给出这一屏的档位。
	//
	// 契约（控制器据此渲染，不再自己编排失败）：
	//   · 返回的视图**恒非 nil** —— 失败路径也要给齐模板必需键（缺键会让整页在那一行
	//     中断渲染，见 internal/templates/CLAUDE.md），控制器因此不必写 nil 判断；
	//   · 失败时视图带 LoadFailed / HTTPStatus（400 参数不合法 / 500 主数据没读到），
	//     err 同时返回（供控制器按当前语言归口失败槽文案与记日志）；
	//   · 失败后**一条派生取数都不发**（在非法工程上继续查只会把「工程不存在」渲染成
	//     「这个工程下没有可选项」）。
	ProductNewPage(ctx context.Context, req *productdto.ProductNewPageReq) (res *productdto.ProductNewPageView, err error)
}
