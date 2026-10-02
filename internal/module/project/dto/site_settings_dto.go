package projectdto

import "encoding/json"

// SiteSettings 站点级设置（projects.settings 这一列 JSON 对象的结构化视图）。
//
// 真源始终是 projects.settings：本类型只负责「同一份 JSON 的命名键」，
// 不引入第二份存储。字段清单在这里单点定义，后台表单（dashboard 站点设置页）
// 与构建期注入（page / presentation 读取 GA4 测量 ID）共用同一份解析 ——
// 两边各写一份结构体迟早会分叉，而分叉的表现是「后台存了、产物里没有」这种静默失效。
type SiteSettings struct {
	// SiteName 站点显示名（前台站名，可与工程名不同）。
	SiteName string `json:"siteName,omitempty"`
	// SiteDesc 站点简介。
	SiteDesc string `json:"siteDesc,omitempty"`
	// ContactEmail 联系邮箱。
	ContactEmail string `json:"contactEmail,omitempty"`
	// GA4MeasurementID 站点 Google Analytics 4 测量 ID（形如 G-XXXXXXXXXX，BIZ-8）。
	//
	// 构建期由 builder 注入产物 <head> 的 gtag 片段；空值 = 一个字节都不注入。
	// 形状校验唯一出口是 builder.NormalizeGA4MeasurementID（保存与注入同一判据）。
	GA4MeasurementID string `json:"ga4MeasurementId,omitempty"`
	// SearchConsoleVerification Google Search Console 站点验证 token（SEO-009）。
	//
	// 构建期由 builder 注入产物 <head> 的 <meta name="google-site-verification">；
	// 空值 = 一个字节都不注入。形状校验唯一出口是 builder.NormalizeSearchConsoleVerification
	// （保存与注入同一判据）。token 是 base64url，**大小写敏感**：这里不做大小写归一化。
	SearchConsoleVerification string `json:"searchConsoleVerification,omitempty"`
	// HeadScripts 站点自定义 Head 代码（PIPE-8：第三方统计 / 营销追踪片段）。
	//
	// 构建期由 builder 注入产物 </head> 之前（独占一行）；空值 = 一个字节都不注入。
	// 与 GA4 / GSC 的实质差别：它**不是白名单字符集能覆盖的东西** —— GA4、Facebook Pixel、
	// Clarity、Hotjar 各有各的写法且随时新增，白名单必漏、转义则让脚本失效。
	// 因此形状判据只做「非空 + 不超长（16 KiB）+ 不含结构性标签（<!DOCTYPE>/<html>/
	// <head>/<body>）」，安全责任由信任边界承担：只有后台站点设置页（Session + CSRF +
	// Casbin /api/project/update）能写，公开面没有写入路径。
	// 形状校验唯一出口是 builder.NormalizeHeadScripts（保存与注入同一判据）；
	// 完整安全论证见 internal/builder/site_scripts.go 的文件头注释。
	HeadScripts string `json:"headScripts,omitempty"`
	// BodyScripts 站点自定义 Body 代码（PIPE-8：在线客服浮窗 / 转化追踪片段）。
	// 构建期注入产物 </body> 之前（独占一行、增强脚本之后），口径与 HeadScripts 完全一致。
	BodyScripts string `json:"bodyScripts,omitempty"`
	// IndexNowKey IndexNow 协议密钥（SEO-022）；空 = 不 ping。
	IndexNowKey string `json:"indexNowKey,omitempty"`
	// NotFoundHTML 站点自定义 404 页内容（SEO-013）。
	//
	// 发布时随站点级产物（sitemap / robots / feed）一起刷到激活目录根的 404.html，
	// 访问面在请求未命中任何激活路径时以 **404 状态码**返回它（位置约定单源在
	// pipeline.NotFoundFileName）；空 = 未配置，此时删除既有的 404.html。
	//
	// 存的是一份完整 HTML 文档（含 <html> / 样式），发布链原样落盘 ——
	// 它不参与页面编译，因此不套用 Page Document 的构建管线。
	// 长度上限的校验出口在后台保存入口（dashboard 站点设置页）。
	NotFoundHTML string `json:"notFoundHtml,omitempty"`
	// URLPatterns 各实体类型的详情页路径模式（WordPress 固定链接的等价物）。
	//
	// 键 = 实体类型（article / product / product_category / product_brand / product_tag），
	// 值 = 路径模板，用 {slug} / {id} 占位，例如 "/blog/{slug}"。
	//
	// 未配置的类型回落到 siteurl.DefaultPatterns，所以"设置页什么都不填"也能自动派生 ——
	// 配置是**覆盖**，不是前置条件。派生值只是后台表单的预填：发布时显式传入的路径永远优先，
	// 且路径一旦发布就独立于内容（改标题 / 正文 / slug 都不动它）。
	URLPatterns map[string]string `json:"urlPatterns,omitempty"`
	// LangURLMode 多语言访问路径方案（多语言开关）：
	//
	//	off = 各语言共用逻辑路径（单语言兼容）；default_plain = 默认语言无前缀、
	//	非默认语言短码前缀；all_prefix = 所有语言一律加短码前缀。
	//
	//	空 = 未在设置页配置，跟随**全局默认**（sys_config 的 i18n 组 site_lang_url_mode：
	//	「所有工程都没配时用什么」）。
	//
	//	它是**工程级**的值：构建期按工程读（唯一解析入口 pipeline.SiteLangURLModeOf ——
	//	工程值非空且合法用它，否则回退全局默认）。保存后立即生效，因为读的就是这份 settings。
	//
	//	（历史）这里一度在保存时热更新 pkg/i18n 的包级变量、启动时再按「第一个配置了该键的
	//	工程」恢复：多工程下 A 工程的保存会改变 B 工程的判定（数据污染），且全局默认值的
	//	定时刷新会把它周期打回。该 setter 与恢复逻辑已删除。
	LangURLMode string `json:"langURLMode,omitempty"`
	// ShippingBaseFee 站点级基础运费（**分**；0 = 不收运费）。
	//
	// 结算（cart）在建单之前经 project 的 ShippingPolicyReader 读出它，作为本单运费的
	// 起点；满额免运费门槛与会员免运费都作用在这一笔上，**不动商品小计**。
	// 单位是分（与 orders 同口径）：元只出现在后台表单边界，换算单源见
	// ParseYuanToCents / FormatCentsAsYuan（整数拆分，不用浮点乘）。
	// 形状校验唯一出口是 NormalizeShippingPolicy（保存与读取同一判据）。
	ShippingBaseFee int64 `json:"shippingBaseFee,omitempty"`
	// ShippingFreeThreshold 满额免运费门槛（**分**；0 = 不启用）。
	//
	// > 0 时：订单商品小计达到该额即免基础运费。与 ShippingBaseFee 的高低不做比较
	//（门槛是「买多少免运费」的独立商业参数，见 NormalizeShippingPolicy）。
	ShippingFreeThreshold int64 `json:"shippingFreeThreshold,omitempty"`
}

// ParseSiteSettings 解析站点设置：非对象、空值、字段缺失一律按零值处理，
// 绝不返回错误 —— 设置缺失是合法状态（新工程只有 {} ），不该阻断后台页面或构建。
func ParseSiteSettings(raw json.RawMessage) SiteSettings {
	var s SiteSettings
	if len(raw) == 0 {
		return s
	}
	_ = json.Unmarshal(raw, &s)
	return s
}
