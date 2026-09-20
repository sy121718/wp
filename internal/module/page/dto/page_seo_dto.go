package pagedto

// page_seo_dto.go — 构建期 SEO 合规巡检的请求 / 响应形状（审计 SEO-01 前半段）。
//
// 报告验收要求每条结论都带 **URL / 规则 / 证据 / ArtifactHash** 四要素，
// 本文件的字段就是这四要素的载体：少任何一项，结论都无法被复核（说不出「哪一页、
// 哪个规则、看到什么字节、哪一份产物」）。

import "go_wp/pkg/utils"

// SEOPatrolReq 站点 SEO 合规巡检请求（按工程）。
type SEOPatrolReq struct {
	ProjectID string `form:"projectId" json:"projectId" binding:"required"`
}

// SEOFindingItem 一条巡检结论。
type SEOFindingItem struct {
	// URL 命中的页面访问路径（可定位）。
	URL string `json:"url"`
	// Lang 该产物的构建语言（多语言站点用来把结论落到具体语言版本）。
	Lang string `json:"lang,omitempty"`
	// ArtifactHash 产物的内容寻址哈希：拿着它就能回到被检查的那份字节。
	ArtifactHash string `json:"artifactHash,omitempty"`
	// Rule 规则 id（稳定契约，见 internal/seo/compliance 的 RuleXxx 常量）。
	Rule string `json:"rule"`
	// Level error = 产物自相矛盾；warn = 需人工确认。
	Level string `json:"level"`
	// Evidence 实际观测到的字节 / 取值（证据）。
	Evidence string `json:"evidence"`
	// Expect 判据说明（为什么这样算不合规）。
	Expect string `json:"expect"`
}

// SEOArtifactItem 单份产物的巡检结论。
type SEOArtifactItem struct {
	URL          string `json:"url"`
	Lang         string `json:"lang,omitempty"`
	ArtifactHash string `json:"artifactHash,omitempty"`
	// Checks 本次评估的规则条目数（含未命中的）。
	Checks int `json:"checks"`
	// Index / Follow / RobotsExplicit 解析出的索引策略（RobotsExplicit=false
	// 表示未声明 robots，此时策略取搜索引擎默认值 index,follow）。
	Index          bool   `json:"index"`
	Follow         bool   `json:"follow"`
	RobotsExplicit bool   `json:"robotsExplicit"`
	Robots         string `json:"robots,omitempty"`
	// Findings 命中该产物的结论（无结论 = 该产物合规）。
	Findings []SEOFindingItem `json:"findings"`
}

// SEOUncheckedRoute 没能参与巡检的激活路由。
//
// 显式列出而不是静默跳过：巡检报告的可用性取决于「它漏了什么」也是可见的
// （自动发布实例的产物不在 page 模块的产物表里，本轮未纳入）。
type SEOUncheckedRoute struct {
	URL    string `json:"url"`
	Reason string `json:"reason"`
}

// SEOPatrolResp 站点 SEO 合规巡检报告。
type SEOPatrolResp struct {
	ProjectID string `json:"projectId"`
	// SampledAt 本次巡检的采样时间（观测信封）。
	//
	// 结论本身是确定性的（同一 ArtifactHash 必得同一份结论），采样时间只回答
	//「这批证据是什么时候采的」—— 它不是结论的一部分，也不能拿它当作新鲜度证明。
	SampledAt utils.JSONTime `json:"sampledAt"`
	// Checked 参与巡检的产物份数；Healthy 其中没有 error 级结论的份数。
	Checked int `json:"checked"`
	Healthy int `json:"healthy"`
	// Errors / Warnings 结论条数（含跨产物结论）。
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
	// ChecksPerArtifact 单份产物上评估的规则条目数（跨产物规则另计两条）。
	ChecksPerArtifact int `json:"checksPerArtifact"`
	// Artifacts 逐产物的结论，按 URL 升序。
	Artifacts []SEOArtifactItem `json:"artifacts"`
	// CrossFindings 跨产物结论（互指不成立 / 互指目标语言与事实不符）。
	// 它们不只属于某一份产物，而是两份产物之间的事实，故单独成列，URL 指向声明方。
	CrossFindings []SEOFindingItem `json:"crossFindings"`
	// Unchecked 未纳入巡检的激活路由（如实列出原因）。
	Unchecked []SEOUncheckedRoute `json:"unchecked"`
}
