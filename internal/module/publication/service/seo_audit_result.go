package pubservice

// seo_audit_result.go — 产物体检的**事实类型**（审计 SEO-019）。
//
// 为什么把结论从「一句话」改成「路径 + 检查项 + 级别 + 参数」：
// 原先每条结论带 Message 成品句，句子里既有中文文案、又混着命中页面与目标链接。
// 那是**展示层**的产物，一旦要在别的语言里显示，就得在 service 里重新拼一遍句子，
// 而 service 没有语言。现在结论只交语言无关的事实：谁的哪个检查项、什么参数；
// 文案由词条（enums 的界面 key）在模板里取，参数由前端按需填充。
//
// 重复标题的 Path **必须为空**：它不是「某个页面的问题」，而是「几个页面之间的问题」。
// 挑第一个命中路径写进 Path，等于给这条结论伪造一个归属页面 —— 运营照着改那一页，
// 另外几页的标题还是没改。全部命中路径放 Params.Paths（已排序），一个不少。

// AuditResult 一次体检的完整结果。
//
// Count 在 service 侧恒等于 len(Issues)（由 RunSEOAudit 归一），不让调用方自己数：
// 两处各算一次，迟早有一处忘了同步，页面上就会出现「发现 3 个问题」但只列出 2 条。
type AuditResult struct {
	Scanned int          `json:"scanned"`
	Count   int          `json:"count"`
	Issues  []AuditIssue `json:"issues"`
}

// AuditIssue 一条体检结论。
//
// Path  该结论的**归属页面**（重复标题为空，见包注释）；
// Item  检查项标识（七个稳定值，见 publication_feed.go 的 Audit* 常量）；
// Level error / warning / info；
// Params 该检查项需要展示的原值（其余四项为空）。
type AuditIssue struct {
	Path   string           `json:"path"`
	Item   string           `json:"item"`
	Level  string           `json:"level"`
	Params AuditIssueParams `json:"params"`
}

// AuditIssueParams 一条结论携带的原值参数。
//
// 只放**从产物里读到的原值**，不放任何拼好的句子：图片带 Src、死链带 Href、
// 重复标题带 Title 与全部命中路径，其余四项三者皆空（omitempty 后不出现在 JSON 里）。
type AuditIssueParams struct {
	Title string   `json:"title,omitempty"`
	Src   string   `json:"src,omitempty"`
	Href  string   `json:"href,omitempty"`
	Paths []string `json:"paths,omitempty"`
}

// newAuditResult 把结论列表归一成结果对象（Count = len(Issues)，空列表编码为 []）。
func newAuditResult(scanned int, issues []AuditIssue) AuditResult {
	if issues == nil {
		issues = []AuditIssue{}
	}
	return AuditResult{Scanned: scanned, Count: len(issues), Issues: issues}
}
