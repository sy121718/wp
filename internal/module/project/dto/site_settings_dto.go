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
	// IndexNowKey IndexNow 协议密钥（SEO-022）；空 = 不 ping。
	IndexNowKey string `json:"indexNowKey,omitempty"`
	// URLPatterns 各实体类型的详情页路径模式（WordPress 固定链接的等价物）。
	//
	// 键 = 实体类型（article / product / product_category / product_brand / product_tag），
	// 值 = 路径模板，用 {slug} / {id} 占位，例如 "/blog/{slug}"。
	//
	// 未配置的类型回落到 siteurl.DefaultPatterns，所以"设置页什么都不填"也能自动派生 ——
	// 配置是**覆盖**，不是前置条件。派生值只是后台表单的预填：发布时显式传入的路径永远优先，
	// 且路径一旦发布就独立于内容（改标题 / 正文 / slug 都不动它）。
	URLPatterns map[string]string `json:"urlPatterns,omitempty"`
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
