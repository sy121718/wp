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
//   - 不以 `Err` / `Msg` 开头的常量（`CartEmpty` / `UserLoginIdentity` …）不在筛选范围内，不受影响。
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

	// —— 捆绑配置器（site.fragment.bundle.*） ——
	BundleCalcOk             = "site.fragment.bundle.calc_ok"
	BundleCannotOrder        = "site.fragment.bundle.cannot_order"
	BundleMaxPieces          = "site.fragment.bundle.max_pieces"
	BundleNotConfigured      = "site.fragment.bundle.not_configured"
	BundleOptional           = "site.fragment.bundle.optional"
	BundleOptionMeta         = "site.fragment.bundle.option_meta"
	BundleOptionRequired     = "site.fragment.bundle.option_required"
	BundlePricePrefix        = "site.fragment.bundle.price_prefix"
	BundleQtyAboveMax        = "site.fragment.bundle.qty_above_max"
	BundleQtyAboveStock      = "site.fragment.bundle.qty_above_stock"
	BundleQtyAria            = "site.fragment.bundle.qty_aria"
	BundleQtyBelowMin        = "site.fragment.bundle.qty_below_min"
	BundleQtyInvalid         = "site.fragment.bundle.qty_invalid"
	BundleRequired           = "site.fragment.bundle.required"
	BundleResultItem         = "site.fragment.bundle.result_item"
	BundleResultSummary      = "site.fragment.bundle.result_summary"
	BundleStockUnavailable   = "site.fragment.bundle.stock_unavailable"
	BundleSubmit             = "site.fragment.bundle.submit"
	BundleTotalAboveMax      = "site.fragment.bundle.total_above_max"
	BundleTotalBelowMin      = "site.fragment.bundle.total_below_min"
	BundleTotalMax           = "site.fragment.bundle.total_max"
	BundleTotalMin           = "site.fragment.bundle.total_min"
	BundleTotalRange         = "site.fragment.bundle.total_range"
	BundleTotalUnlimited     = "site.fragment.bundle.total_unlimited"
	BundleUnlimited          = "site.fragment.bundle.unlimited"
	BundleVariantDup         = "site.fragment.bundle.variant_dup"
	BundleVariantNotInConfig = "site.fragment.bundle.variant_not_in_config"

	// —— 搜索（site.fragment.search.*） ——
	SearchDegraded   = "site.fragment.search.degraded"
	SearchEmptyQuery = "site.fragment.search.empty_query"
	SearchNoResults  = "site.fragment.search.no_results"

	// —— 实时价格（site.fragment.live_price.*） ——
	LivePriceDelisted = "site.fragment.live_price.delisted"
	LivePriceUpdated  = "site.fragment.live_price.updated"

	// —— 登录面板（site.fragment.login_panel.*） ——
	LoginPanelContinueShopping = "site.fragment.login_panel.continue_shopping"
	LoginPanelLoginRegister    = "site.fragment.login_panel.login_register"

	// —— 订单（site.fragment.order.*） ——
	OrderCurrency                = "site.fragment.order.currency"
	OrderDiscount                = "site.fragment.order.discount"
	OrderEmpty                   = "site.fragment.order.empty"
	OrderGoShop                  = "site.fragment.order.go_shop"
	OrderNeedLoginDetail         = "site.fragment.order.need_login_detail"
	OrderNeedLoginList           = "site.fragment.order.need_login_list"
	OrderNextPage                = "site.fragment.order.next_page"
	OrderNoReturnable            = "site.fragment.order.no_returnable"
	OrderPagerAria               = "site.fragment.order.pager_aria"
	OrderPayMethod               = "site.fragment.order.pay_method"
	OrderPrevPage                = "site.fragment.order.prev_page"
	OrderReaderUnavailable       = "site.fragment.order.reader_unavailable"
	OrderRemark                  = "site.fragment.order.remark"
	OrderReturnApply             = "site.fragment.order.return_apply"
	OrderReturnHint              = "site.fragment.order.return_hint"
	OrderReturnQtyAria           = "site.fragment.order.return_qty_aria"
	OrderReturnQtyHint           = "site.fragment.order.return_qty_hint"
	OrderReturnQtyPlaceholder    = "site.fragment.order.return_qty_placeholder"
	OrderReturnReason            = "site.fragment.order.return_reason"
	OrderReturnReasonPlaceholder = "site.fragment.order.return_reason_placeholder"
	OrderReturnsTitle            = "site.fragment.order.returns_title"
	OrderReturnSubmit            = "site.fragment.order.return_submit"
	OrderShipping                = "site.fragment.order.shipping"
	OrderShippingAddr            = "site.fragment.order.shipping_addr"
	OrderSubtotalGoods           = "site.fragment.order.subtotal_goods"
	OrderTabAll                  = "site.fragment.order.tab_all"
	OrderTabsAria                = "site.fragment.order.tabs_aria"
	OrderTotalCount              = "site.fragment.order.total_count"
	OrderUnavailable             = "site.fragment.order.unavailable"
	OrderViewDetail              = "site.fragment.order.view_detail"

	// —— 退货（site.fragment.return.*） ——
	ReturnNeedLogin           = "site.fragment.return.need_login"
	ReturnProviderUnavailable = "site.fragment.return.provider_unavailable"
	ReturnReasonPrefix        = "site.fragment.return.reason_prefix"
	ReturnRefundNote          = "site.fragment.return.refund_note"
	ReturnStatusLine          = "site.fragment.return.status_line"
	ReturnTitleError          = "site.fragment.return.title_error"
	ReturnTitleSuccess        = "site.fragment.return.title_success"

	// —— 访客账号 · 登录注册（site.fragment.user.*） ——
	UserAccountAddress               = "site.fragment.user.account.address"
	UserAccountBio                   = "site.fragment.user.account.bio"
	UserAccountBirthday              = "site.fragment.user.account.birthday"
	UserAccountChangePassword        = "site.fragment.user.account.change_password"
	UserAccountCity                  = "site.fragment.user.account.city"
	UserAccountCompany               = "site.fragment.user.account.company"
	UserAccountCountry               = "site.fragment.user.account.country"
	UserAccountCurrentDevice         = "site.fragment.user.account.current_device"
	UserAccountCurrentPassword       = "site.fragment.user.account.current_password"
	UserAccountEmail                 = "site.fragment.user.account.email"
	UserAccountEmailNotify           = "site.fragment.user.account.email_notify"
	UserAccountFirstName             = "site.fragment.user.account.first_name"
	UserAccountGender                = "site.fragment.user.account.gender"
	UserAccountGenderFemale          = "site.fragment.user.account.gender_female"
	UserAccountGenderMale            = "site.fragment.user.account.gender_male"
	UserAccountGenderUnset           = "site.fragment.user.account.gender_unset"
	UserAccountIPUnknown             = "site.fragment.user.account.ip_unknown"
	UserAccountLastActive            = "site.fragment.user.account.last_active"
	UserAccountLastLogin             = "site.fragment.user.account.last_login"
	UserAccountLastName              = "site.fragment.user.account.last_name"
	UserAccountLoggedInAt            = "site.fragment.user.account.logged_in_at"
	UserAccountNeedLoginPassword     = "site.fragment.user.account.need_login_password"
	UserAccountNeedLoginPreference   = "site.fragment.user.account.need_login_preference"
	UserAccountNeedLoginProfile      = "site.fragment.user.account.need_login_profile"
	UserAccountNeedLoginSessions     = "site.fragment.user.account.need_login_sessions"
	UserAccountNewPassword           = "site.fragment.user.account.new_password"
	UserAccountNickname              = "site.fragment.user.account.nickname"
	UserAccountPageSize              = "site.fragment.user.account.page_size"
	UserAccountPasswordHint          = "site.fragment.user.account.password_hint"
	UserAccountPhone                 = "site.fragment.user.account.phone"
	UserAccountProvince              = "site.fragment.user.account.province"
	UserAccountRegisteredAt          = "site.fragment.user.account.registered_at"
	UserAccountRevoke                = "site.fragment.user.account.revoke"
	UserAccountRevokeOthers          = "site.fragment.user.account.revoke_others"
	UserAccountSavePreference        = "site.fragment.user.account.save_preference"
	UserAccountSaveProfile           = "site.fragment.user.account.save_profile"
	UserAccountSessionsEmpty         = "site.fragment.user.account.sessions_empty"
	UserAccountSessionsUnavailable   = "site.fragment.user.account.sessions_unavailable"
	UserAccountShowOnline            = "site.fragment.user.account.show_online"
	UserAccountSmsNotify             = "site.fragment.user.account.sms_notify"
	UserAccountTimezone              = "site.fragment.user.account.timezone"
	UserAccountUnavailable           = "site.fragment.user.account.unavailable"
	UserAccountUnavailablePreference = "site.fragment.user.account.unavailable_preference"
	UserAccountUnknownBrowser        = "site.fragment.user.account.unknown_browser"
	UserAccountUnverified            = "site.fragment.user.account.unverified"
	UserAccountUsername              = "site.fragment.user.account.username"
	UserAccountVerified              = "site.fragment.user.account.verified"
	UserAccountVisibility            = "site.fragment.user.account.visibility"
	UserAccountVisibilityMembers     = "site.fragment.user.account.visibility_members"
	UserAccountVisibilityPrivate     = "site.fragment.user.account.visibility_private"
	UserAccountVisibilityPublic      = "site.fragment.user.account.visibility_public"
	UserAccountWebsite               = "site.fragment.user.account.website"
	UserAccountZip                   = "site.fragment.user.account.zip"
	UserForgotBackLogin              = "site.fragment.user.forgot.back_login"
	UserForgotEmail                  = "site.fragment.user.forgot.email"
	UserForgotPrivacy                = "site.fragment.user.forgot.privacy"
	UserForgotSubmit                 = "site.fragment.user.forgot.submit"
	UserLoginForgot                  = "site.fragment.user.login.forgot"
	UserLoginIdentity                = "site.fragment.user.login.identity"
	UserLoginNoAccount               = "site.fragment.user.login.no_account"
	UserLoginPassword                = "site.fragment.user.login.password"
	UserLoginRegister                = "site.fragment.user.login.register"
	UserLoginRemember                = "site.fragment.user.login.remember"
	UserLoginSubmit                  = "site.fragment.user.login.submit"
	UserPanelAccountCenter           = "site.fragment.user.panel.account_center"
	UserPanelAccountHint             = "site.fragment.user.panel.account_hint"
	UserPanelGuest                   = "site.fragment.user.panel.guest"
	UserPanelLoggedIn                = "site.fragment.user.panel.logged_in"
	UserPanelLogout                  = "site.fragment.user.panel.logout"
	UserPanelNoAccount               = "site.fragment.user.panel.no_account"
	UserPanelRegister                = "site.fragment.user.panel.register"
	UserRegisterAfterNote            = "site.fragment.user.register.after_note"
	UserRegisterEmail                = "site.fragment.user.register.email"
	UserRegisterHasAccount           = "site.fragment.user.register.has_account"
	UserRegisterLogin                = "site.fragment.user.register.login"
	UserRegisterNickname             = "site.fragment.user.register.nickname"
	UserRegisterPassword             = "site.fragment.user.register.password"
	UserRegisterSubmit               = "site.fragment.user.register.submit"
	UserRegisterUsername             = "site.fragment.user.register.username"
	UserResetEmail                   = "site.fragment.user.reset.email"
	UserResetPassword                = "site.fragment.user.reset.password"
	UserResetRevokeNote              = "site.fragment.user.reset.revoke_note"
	UserResetSubmit                  = "site.fragment.user.reset.submit"

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

	// —— 模块文案过渡表（site.fragment.msg.*，fragmentMessageKeys 用） ——
	FragmentMsgCartEmpty           = "site.fragment.msg.cart_empty"
	FragmentMsgInternal            = "site.fragment.msg.internal"
	FragmentMsgInvalidParam        = "site.fragment.msg.invalid_param"
	FragmentMsgLoginRequired       = "site.fragment.msg.login_required"
	FragmentMsgLoginRequiredOrders = "site.fragment.msg.login_required_orders"
	FragmentMsgOrderNotFound       = "site.fragment.msg.order_not_found"
	FragmentMsgOrdersUnavailable   = "site.fragment.msg.orders_unavailable"
	FragmentMsgProductUnavailable  = "site.fragment.msg.product_unavailable"
	FragmentMsgQtyInvalid          = "site.fragment.msg.qty_invalid"
	FragmentMsgReturnItemsRequired = "site.fragment.msg.return_items_required"
	FragmentMsgReturnsUnavailable  = "site.fragment.msg.returns_unavailable"
	FragmentMsgStockInsufficient   = "site.fragment.msg.stock_insufficient"
)
