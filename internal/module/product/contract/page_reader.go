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
//
// 实现方是本模块 service（编译期断言 `var _ ProductPageReader = (*Service)(nil)`
// 在 `service/product_page.go`）；消费方是 `inbound/http/` 的整页控制器
// （新建 / 列表 / 详情 / 编辑 / 建表单 / 定价工具 / 属性库 / 详情页模板 / 捆绑配置 /
// 分类 / 品牌 / 标签；译文工作台带写方法，单独一条端口见 translation_reader.go），
// 那里只能是**运行期**断言 ——
// 控制器持有的是 `ProductService`，读模型不在它的能力面上（放上去等于把页面的形状
// 变成跨模块契约的一部分，见上）。
//
// **这些方法必须在同一个接口里**：控制器一次断言拿到整份读模型，少实现一个就是断言失败 →
// 整页走 500 兜底页（症状是「这一屏的取数根本没被调到」，桩与中间层的包装类型都踩过）。
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

	// ProductListPage 商品列表整页的读模型：工程清单 → 工程存在性校验 → 分类树 / 品牌 /
	// 标签 / 仓库清单 → 列表与总数（同一份过滤条件）→ 逐行详情 → 属性组选项 / 每仓 SKU
	// 候选 → 定价规则清单，并给出这一屏的档位。
	//
	// 契约同上（视图恒非 nil / 失败带 LoadFailed 与 HTTPStatus / 失败后不发派生取数），
	// 另加一条：**逐行详情取不到就整页失败**，不做静默跳过 —— 少一行而仍写着「共 N 条」，
	// 用户会以为商品被删了。
	ProductListPage(ctx context.Context, req *productdto.ProductListPageReq) (res *productdto.ProductListPageView, err error)

	// ProductDetailPage 商品详情整页的读模型：工程清单 → 工程存在性校验 → 仓库清单 →
	// 商品详情 → 分类树 / 品牌 / 标签，并给出这一屏的档位。
	//
	// 契约同上，另加一条**三档**（这三个此前混在一句 `derr == nil && detail != nil` 里）：
	//   · 404 业务上的「不存在」（含没给 product 参数）—— **不进失败槽**，走模板自带的空态，
	//     仍然 no-store（商品可能刚被建回来，重试要真的回到服务端）；
	//   · 400 参数不合法（?project=<不存在的 id> / id 形态不对）—— 进失败槽 + no-store；
	//   · 500 其余（数据库 / 连接 / 超时 / 畸形返回）—— 进失败槽 + no-store。
	ProductDetailPage(ctx context.Context, req *productdto.ProductDetailPageReq) (res *productdto.ProductDetailPageView, err error)

	// ProductEditPage 商品编辑整页的读模型：工程清单 → 工程存在性校验 → 分类树 / 品牌 /
	// 标签 / 仓库清单 → 商品详情（三档，判据同 ProductDetailPage）→ 属性组候选，
	// 并给出这一屏的档位。
	//
	// 契约同上，另加一条：**属性组候选读失败不让整页失败** —— 它是表单里的一块选择器。
	// 原始错误随视图带出（AttributeOptionsErr），由控制器决定撤掉保存入口并归口文案。
	//
	// 表单回显也不在这里：校验失败重渲染要用**这次提交的值**覆盖读回来的值，
	// 而提交值在请求体里 —— 那是控制器的事（inbound/http 的 formEchoFrom）。
	ProductEditPage(ctx context.Context, req *productdto.ProductEditPageReq) (res *productdto.ProductEditPageView, err error)

	// ProductCreateForm 商品建表单的取数：仓库清单 → 属性组候选 → 每仓 SKU 候选。
	//
	// 「表单」不是另一种响应形态 —— 后端给数据、Jet 渲染 HTML：这份视图撑的是**用数据渲染出来的
	// 那一段 HTML**，首屏与提交失败后的重渲染共用同一份装配（所以取数编排只有一处）。
	//
	// 契约同上面四条（视图恒非 nil / 失败带 LoadFailed 与 HTTPStatus），但没有三档：
	// 主数据读不到就是这个表单撑不起来，调用方退回整页提示 —— 不留半个表单给用户重填。
	ProductCreateForm(ctx context.Context, req *productdto.ProductCreateFormReq) (res *productdto.ProductCreateFormView, err error)

	// ProductPricingPage 定价工具页的读模型：工程清单 → 规则与尾数处理参考表 →
	// 改价表单初值（或本次提交的回显值）→ 试算结果 → 留痕台账。
	//
	// 与上面五条**不同：这一屏没有取数失败档** —— 三处取数各自降级（工程清单读不到 → 空下拉；
	// 参考表是注册表快照、本身无 error；台账读不到 → 空台账），页面主体（改价表单）始终
	// 撑得起来。视图仍实现 PageResult，但 HTTPStatus 非 0 只会来自「读模型没接上」这类
	// 装配缺陷（控制器那一半给 500 + no-store），正常路径恒 200 且可缓存。
	//
	// Form / Preview / ErrMsg 由控制器填（它们是写路径的产物，读模型不认识 gin.Context）。
	ProductPricingPage(ctx context.Context, req *productdto.ProductPricingPageReq) (res *productdto.ProductPricingPageView, err error)
	// ProductAttributesPage 属性库整页的读模型：工程清单 → 工程存在性校验 → 计数 →
	// 页码收敛 → 取页，并给出这一屏的档位。
	//
	// 契约同上（视图恒非 nil / 失败带 LoadFailed 与 HTTPStatus / 失败后不发派生取数），
	// 另加一条**先计数再取页**：反过来时越界页码会让取数层返回空页，而分页条按收敛后的
	// 页码渲染 ——「表格为空、分页条却显示第 2 页」是自相矛盾的组合。
	//
	// 行装配不在这里：每行的两个抽屉表单要 csrf 与取词函数（gin.Context 的产物），
	// 所以只交原料（AttributeDetails），成品行由控制器装（同 ProductListPage）。
	ProductAttributesPage(ctx context.Context, req *productdto.ProductAttributesPageReq) (res *productdto.ProductAttributesPageView, err error)

	// ProductDetailTemplatePage 「详情页模板」整页的读模型：工程清单 → 工程存在性校验 →
	// 商品详情（三档）→ 模板清单（主数据）→ 默认模板与绑定实例的标注，并给出这一屏的档位。
	//
	// 契约同上，另加三条这一屏特有的：
	//   · 模板清单读不到 = 整页装载失败，且**一条派生取数都不发**（默认模板 / 绑定实例 /
	//     发布路径都从清单派生，清单没到之前发出去只会在主错误之外再叠一层错误）；
	//   · 默认模板解析失败、绑定实例读不到**都不算失败**（前者按「没有默认」、后者按
	//     「还没有实例」渲染）—— 凡是「顺带知道一下」的事实都不参与失败判定，
	//     否则发布链路一抖整页就红，而页面此时正需要让人点「首次发布」；
	//   · 没给 product 参数时置 NoProduct 并**停止取数**（菜单入口形态，控制器据此走
	//     引导出口；它不是 400 —— 本页本来就有「还没选商品」的入口）。
	ProductDetailTemplatePage(ctx context.Context, req *productdto.ProductDetailTemplatePageReq) (res *productdto.ProductDetailTemplatePageView, err error)

	// ProductCategoriesPage 分类管理页的读模型：工程清单 → 参数档位 → 本页分类树（已 DFS 扁平）
	// → 工程内全量分类（父级下拉的原料）→ 缺失父级的事实。
	//
	// 两块分类原料**分开交**（CategoryDetails 与 PickFlat）：页面表格要本页的树，父级下拉要
	// 全量 —— 树状列表之后没有面包屑兜底，缺项会直接表现为「新建子分类时选不到父级」。
	ProductCategoriesPage(ctx context.Context, req *productdto.ProductCategoriesPageReq) (res *productdto.ProductCategoriesPageView, err error)

	// ProductBrandsPage 品牌管理页的读模型：工程清单 → 参数档位 → 计数 → 页码收敛 → 取页。
	ProductBrandsPage(ctx context.Context, req *productdto.ProductBrandsPageReq) (res *productdto.ProductBrandsPageView, err error)

	// ProductTagsPage 标签管理页的读模型：工程清单 → 参数档位 → 规则类型清单（全页一次）→
	// 计数 → 页码收敛 → 取页。命中商品**不在首屏取**（审计 PERF-02），由展开区按页拉片段。
	ProductTagsPage(ctx context.Context, req *productdto.ProductTagsPageReq) (res *productdto.ProductTagsPageView, err error)

	// ProductBundlePage 捆绑配置整页的读模型：工程清单 → 工程存在性校验 → 工程商品 →
	// 捆绑配置（三档）→ 候选 SKU → 来源属性 / 仓库清单 / 逐仓 SKU 候选，并给出这一屏的档位。
	//
	// 契约同上，另加两条这一屏特有的：
	//   · 成员表行、候选下拉、三种来源面板的选项都在这条读模型里装（它们是纯函数，
	//     输入是读回来的原料，一条也不需要 gin.Context）；
	//   · 失败档**照样装配**（Rows / CandidateSKUs / Cfg* / 来源面板都给齐），并置
	//     SaveBlocked —— 写失败之后重读又失败时，成员清单是这一屏里最贵的内容，
	//     不能因为页面重建失败就把它丢掉；保存入口撤掉即可，不是把整屏清空。
	//
	// Submitted 由控制器填（写路径产物，在请求体里，读模型不认识 gin.Context）：
	// 非 nil 时成员清单一律按它渲染，而不是按库里读回来的配置。
	ProductBundlePage(ctx context.Context, req *productdto.ProductBundlePageReq) (res *productdto.ProductBundlePageView, err error)
}
