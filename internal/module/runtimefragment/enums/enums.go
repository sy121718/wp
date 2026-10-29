// Package runtimefragmentenums 访客片段（Runtime Fragment）Go 侧取词用的 i18n key 常量。
//
// # 为什么这个包存在
//
// 片段的访客文案是 `site.fragment.*` 词条（迁移 409 及之前若干批 seed）。收口前，取词调用点
// 把 key 直接写成字符串字面量（`r.tr` 的第一个实参是裸 key 串）：
// 同一个 key 在多个文件里各写一遍，改 key 就得靠全文搜索，漏一处只表现为「那句话回落中文」，
// 不报错、不 500、测试全绿。本包把 key 收成**编译期常量**，改名即编译失败。
//
// # 硬判据：常量值逐字等于 sys_i18n.item_key
//
// `pkg/i18n` 的取值链是 `text != "" && text != key` 命中即用，于是**常量值写错不会编译报错**
// —— 只会让词条取不到、页面静默回落中文兜底。改动本文件时，值的判据是「与被替换掉的那个
// 字面量逐字相同」，不是「看起来像」。
//
// # 与「错误消息 enums」的区别（命名为什么不得不例外）
//
// 其它模块的 `enums` 放的是**响应消息**（值形如 `cart.err.outOfStock`，三段），消费方是
// `pkg/response` 的翻译层与 `ErrorAuto`。本包放的是**访客页面文案**，消费方是片段的
// `Request.tr`，**从不**进 `ErrorAuto`。
//
// 但 `pkg/response/error_auto_test.go` 的 TestIsBusinessErrorCoversAllModuleEnums 会 glob
// `internal/module/*/enums/*.go`、逐个校验**所有** `Err*` / `Msg*` 常量的值是否命中它的两层判据
// （key 形态 `模块.类别.语义` / 常量名形态 `ErrXxx`）。实测（2026-09）：
//
//   - `site.fragment.*` 是**四段**，不命中 `businessErrKey`（只匹配三段）→ 以 `Err` / `Msg`
//     开头、值为本形态的常量会让那条测试直接变红（本包 `FragmentMsg*` 的 12 个常量正是因此改名）；
//   - 不以 `Err` / `Msg` 开头的常量（`CartEmpty` / `OrderEmpty` …）不在筛选范围内，不受影响。
//
// 所以：**本包禁止出现 `Err*` / `Msg*` 前缀的常量**（片段错误出口的 9 个 key 常量留在
// fragment_err.go 包内，不进本包）。新增常量时按语义路径取名即可，除非 key 落在 `msg` 段
// —— 那一段统一用 `FragmentMsg` 前缀。
//
// 详见 `docs/rules/i18n.md` §五（Go 侧取词的形态清单与归零验收口径）。
package runtimefragmentenums

// 取词调用点一律写成 `r.tr(rfenums.Xxx, "中文兜底")` —— **第二个实参必须留在原地**：
// 它是词条缺失（未跑迁移 / i18n 未初始化）时的回落，删掉会让页面显示裸 key。
const (
	// —— 通用 · 会话状态（site.fragment.common.*） ——
	CommonGoLogin         = "site.fragment.common.go_login"
	CommonSessionNotReady = "site.fragment.common.session_not_ready"

	// —— 购物车 · 结算 · 库存（site.fragment.cart.* / checkout.* / stock.*） ——
	CartCheckout          = "site.fragment.cart.checkout"
	CartClear             = "site.fragment.cart.clear"
	CartEmpty             = "site.fragment.cart.empty"
	CartItemsUnit         = "site.fragment.cart.items_unit"
	CartQtyAria           = "site.fragment.cart.qty_aria"
	CartTotalPrefix       = "site.fragment.cart.total_prefix"
	CartUnavailable       = "site.fragment.cart.unavailable"
	CartUpdate            = "site.fragment.cart.update"
	CheckoutAccountMailed = "site.fragment.checkout.account_mailed"
	CheckoutOrderNo       = "site.fragment.checkout.order_no"
	CheckoutTitlePaid     = "site.fragment.checkout.title_paid"
	CheckoutTitlePending  = "site.fragment.checkout.title_pending"
	CheckoutTotal         = "site.fragment.checkout.total"
	CheckoutUnavailable   = "site.fragment.checkout.unavailable"
	StockCheckout         = "site.fragment.stock.checkout"
	StockIn               = "site.fragment.stock.in"
	StockLow              = "site.fragment.stock.low"
	StockOut              = "site.fragment.stock.out"

	// —— 搜索（site.fragment.search.*） ——
	SearchDegraded   = "site.fragment.search.degraded"
	SearchEmptyQuery = "site.fragment.search.empty_query"
	SearchNoResults  = "site.fragment.search.no_results"

	// —— 实时价格（site.fragment.live_price.*） ——
	LivePriceDelisted = "site.fragment.live_price.delisted"
	LivePriceUpdated  = "site.fragment.live_price.updated"

	// —— 会员身份（site.fragment.membership.*，BIZ-3 消费侧接入） ——
	//
	// 会员片段的降级形态各有各的说法（未登录要访客去登录、端口未接入要运维去接线、
	// 解析失败可以稍后重试），所以不是一句通用提示 —— 三者页面上长得一样的话，
	// 装配缺陷会被当成「这站要登录」而被忽略很久。
	MembershipTitle          = "site.fragment.membership.title"
	MembershipGuest          = "site.fragment.membership.guest"
	MembershipUnavailable    = "site.fragment.membership.unavailable"
	MembershipProjectMissing = "site.fragment.membership.project_missing"
	MembershipFailed         = "site.fragment.membership.failed"
	MembershipDefaultHint    = "site.fragment.membership.default_hint"
	MembershipDiscount       = "site.fragment.membership.discount"
	MembershipFreeShipping   = "site.fragment.membership.free_shipping"
	MembershipNoBenefit      = "site.fragment.membership.none"

	// —— 评论（site.fragment.comment.*，BIZ-5） ——
	//
	// 降级形态各有各的说法（未登录要访客去登录、端口未接入要运维去接线、没给工程 / 实体是
	// 页面作者的配置问题、类型未注册说明注册表缺了一项、限流要访客等一会儿），
	// 所以不是一句通用提示 —— 三者页面上长得一样的话，装配缺陷会被当成「这站不让评论」
	// 而被忽略很久。词条与迁移 466a 的 seed 逐条对应。
	CommentTitle           = "site.fragment.comment.title"
	CommentEmpty           = "site.fragment.comment.empty"
	CommentFormTitle       = "site.fragment.comment.form_title"
	CommentBodyPlaceholder = "site.fragment.comment.body_placeholder"
	CommentSubmit          = "site.fragment.comment.submit"
	CommentMore            = "site.fragment.comment.more"
	CommentReply           = "site.fragment.comment.reply"
	CommentReplyTo         = "site.fragment.comment.reply_to"
	CommentAuthorGuest     = "site.fragment.comment.author_guest"
	CommentPendingNotice   = "site.fragment.comment.pending_notice"
	CommentGuest           = "site.fragment.comment.guest"
	CommentUnavailable     = "site.fragment.comment.unavailable"
	CommentProjectMissing  = "site.fragment.comment.project_missing"
	CommentEntityMissing   = "site.fragment.comment.entity_missing"
	CommentEntityInvalid   = "site.fragment.comment.entity_invalid"
	CommentListFailed      = "site.fragment.comment.list_failed"
	CommentBodyRequired    = "site.fragment.comment.body_required"
	CommentBodyTooLong     = "site.fragment.comment.body_too_long"
	CommentCSRFExpired     = "site.fragment.comment.csrf_expired"
	CommentRateLimited     = "site.fragment.comment.rate_limited"
	CommentSubmitFailed    = "site.fragment.comment.submit_failed"
)
