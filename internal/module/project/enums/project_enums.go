// Package projectenums 统一管理 project 模块响应消息。
package projectenums

const (
	ErrProjectNotFound = "ErrProjectNotFound" // 站点工程不存在
	ErrInvalidSettings = "ErrInvalidSettings" // 站点设置必须是合法的 JSON 对象
	ErrInvalidName     = "ErrInvalidName"     // 站点工程名称不能为空且不能超过 200 个字符
	// ErrInvalidParam 请求本身不合法（nil 请求等），与名称/设置/工程存在性无关。
	ErrInvalidParam = "ErrInvalidParam" // 请求参数无效
	// ErrProjectInternal 工程服务内部错误（基础设施故障兜底，不向客户端泄漏内部细节）。
	ErrProjectInternal = "ErrProjectInternal" // 工程服务内部错误
	// ErrProjectRequired 未指定工程且解析不出工程作用域（0 个工程，或入口本身没有工程参数）。
	//
	// DB-009 第三批：主题入口（GetTheme/UpdateTheme/ActivateTheme/DeleteTheme）的签名里
	// 没有工程参数（后台页面直接依赖该契约），此前靠「工程表恰好一个工程」猜作用域，
	// 多工程部署下退化为「不限工程」—— 换非超级角色后那是静默「主题不存在」。
	// 现在改为逐工程扇出定位；只有连工程表都读不出内容时才用这个错误显式失败。
	ErrProjectRequired = "ErrProjectRequired" // 需要显式工程作用域
)

const (
	// ---- 站点主题（Theme）业务错误文案，project 模块下 theme 能力 ----
	ErrThemeNameRequired    = "ErrThemeNameRequired"    // 主题名称不能为空
	ErrThemeNotFound        = "ErrThemeNotFound"        // 主题不存在
	ErrThemeIsActive        = "ErrThemeIsActive"        // 激活主题不可删除，请先切换到其他主题
	ErrThemeDuplicateName   = "ErrThemeDuplicateName"   // 同名主题已存在
	ErrThemeProjectIDEmpty  = "ErrThemeProjectIDEmpty"  // 工程 ID 不能为空
	ErrInvalidThemeSettings = "ErrInvalidThemeSettings" // 无效的主题设置
	// ErrThemeInternal 主题服务内部错误（基础设施故障兜底，不向客户端泄漏内部细节）。
	ErrThemeInternal = "ErrThemeInternal" // 主题服务内部错误
)

const (
// ---- 主题包资产端口错误（主题包导入导出已移除，端口保留给装配层注入的适配器） ----
// 值一律等于常量名：文案真源在 sys_i18n（迁移 058 的既有口径），此处只做键。
)

const (
	MsgProjectCreated = "MsgProjectCreated" // 站点工程创建成功
	MsgProjectUpdated = "MsgProjectUpdated" // 站点工程更新成功
)

// 后台页面（主题管理 / 主题设置 / 站点设置）的**表单校验与结果**文案。
//
// 为什么不复用上面的 service 哨兵文案：这些判断发生在 service **之前**（表单缺参）
// 或 service **之后**（保存成功但整站刷新失败），两侧都没有一个 error 可以拿来判定，
// 所以按显式 key 走 —— 判据与出口见 inbound/http/project_err.go。
//
// 值一律等于常量名：文案真源在 sys_i18n（迁移 058 的既有口径），此处只做键。
const (
	ErrThemeIDRequired = "ErrThemeIDRequired" // 缺少主题 id

	// MsgThemeSettingsInvalid 主题设置未通过校验（IsSafeCSSValue 白名单）。
	//
	// 原先散在 theme_settings_admin_pages.go 的本地常量里，本轮随页面出口改造收进 enums：
	// 它是**响应文案**（会进 ?err= 与模板错误槽），而本地常量让读侧白名单够不着它。
	MsgThemeSettingsInvalid = "MsgThemeSettingsInvalid" // 主题设置不合法

	// MsgThemeSettingsRefreshFailed 「保存已成功、整站刷新失败」的部分成功提示。
	//
	// 归 Msg 而不是 Err：落库已经提交，用户要做的是「稍后重建」而不是「重填表单」——
	// 文案里必须把这个区别说出来，否则用户会以为刚才那一屏白填了，然后再填一遍。
	MsgThemeSettingsRefreshFailed = "MsgThemeSettingsRefreshFailed" // 主题设置已保存，但整站页面刷新失败

	ErrSiteSettingsNameRequired = "ErrSiteSettingsNameRequired" // 工程与站点名称不能为空
	ErrGA4IDInvalid             = "ErrGA4IDInvalid"             // GA4 测量 ID 格式不合法（形如 G-XXXXXXXXXX，只允许字母与数字）
	ErrGSCVerificationInvalid   = "ErrGSCVerificationInvalid"   // Search Console 验证 token 格式不合法（base64url：字母、数字、- 与 _，8~128 位）
	ErrNotFoundHTMLTooLong      = "ErrNotFoundHTMLTooLong"      // 自定义 404 页内容过长（上限 32 KiB）
	ErrLangURLModeInvalid       = "ErrLangURLModeInvalid"       // 语言 URL 方案取值非法（可选 off / default_plain / all_prefix）
	// 站点运费规则（站点级基础运费 / 满额免运费门槛，单位分）。
	//
	// 两个字段各一个 key 而不是合成一句「运费配置不合法」：这一页有十几个输入框，
	// 不指明是哪一个的提示等于让用户自己猜（而他要猜的那两个框长得几乎一样）。
	ErrShippingBaseFeeInvalid       = "ErrShippingBaseFeeInvalid"       // 基础运费金额不合法（负数 / 非数字 / 超上限）
	ErrShippingFreeThresholdInvalid = "ErrShippingFreeThresholdInvalid" // 满额免运费门槛金额不合法（同上）
)

// —— 点分 key 常量（新式）——
//
// 值是 sys_i18n 的 item_key（文案真源在迁移 451），命名按「去掉模块子域前缀
// （`admin.theme_settings.` / `admin.seo.`）后的语义路径」。
//
// 与上面那批 `ErrXxx = "ErrXxx"` 分开成组：老式形态的值就是常量名本身
// （`sys_i18n` 里存同名 key），两者混在同一前缀下会让人以为值也是 `ErrXxx`。
// 中文兜底留在调用点（词条缺失时的回落），不在这里。
const (
	// 主题设置页的结构与分块文案。
	ThemeSettingsStructureCurrent = "admin.theme_settings.structure.current" // （当前生效）
	ThemeSettingsBlockUnset       = "admin.theme_settings.block.unset"       // （未设置）

	// SEO 评分卡的条目格式与 SERP 占位。
	//
	// SEOScoreIssueFormat 是**跨模块共用**词条：站点设置面板的页面评分卡
	//（settings_panel.go）与文章编辑页的文章评分卡（content 模块 article_view.go）
	// 渲染的是同一批检查项。词条 key 以 admin.seo.score 打头、页面级评分卡是本模块
	// 的主要消费方，故常量登记在本包，content 侧引用来复用（不另抄一份自研 key）。
	SEOScoreIssueFormat    = "admin.seo.score.issueFormat"    // {label}：{actual}（基准 {benchmark}）→ {hint}
	SEOScoreSerpTitleEmpty = "admin.seo.score.serpTitleEmpty" // （未填写 SEO 标题）
	SEOScoreSerpDescEmpty  = "admin.seo.score.serpDescEmpty"  // （未填写 SEO 描述）
)
