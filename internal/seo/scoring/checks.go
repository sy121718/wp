package scoring

// checks.go — 检查项定义（docs/02-E1 §4 的 24 个检查项）。
// 每个 Check.Score 是纯函数：只读 Input，返回得分与实测值描述。

type checkFunc func(*Input) (int, string)

// Check 单个检查项。
type Check struct {
	Key   string
	Score checkFunc
	Max   int
}

// Section 评分维度。
type Section struct {
	Key    string
	Weight float64
	Checks []Check
}

// allSections 全部维度与检查项（顺序即侧栏展示顺序）。
func allSections() []Section {
	return []Section{
		{Key: "title", Weight: 0.15, Checks: []Check{
			{Key: "title_present", Max: 5, Score: chkTitlePresent},
			{Key: "title_length", Max: 5, Score: chkTitleLength},
			{Key: "title_keyword_first_half", Max: 5, Score: chkTitleKeywordFirstHalf},
		}},
		{Key: "meta", Weight: 0.05, Checks: []Check{
			{Key: "meta_length", Max: 3, Score: chkMetaLength},
			{Key: "meta_has_cta", Max: 2, Score: chkMetaCTA},
		}},
		{Key: "headings", Weight: 0.10, Checks: []Check{
			{Key: "single_h1", Max: 4, Score: chkSingleH1},
			{Key: "h1_has_keyword", Max: 3, Score: chkH1Keyword},
			{Key: "heading_hierarchy", Max: 3, Score: chkHierarchy},
		}},
		{Key: "content", Weight: 0.25, Checks: []Check{
			// 维度内 25 分的分配自 2026-09-29 起是 12/4/4/5（原来是 15/5/5）：
			// 对比表是新增的硬要求，分数从长度与可读性两项里让出来 —— 维度总分不变，
			// 但「写得很长」不再自动等于「内容质量高」。
			{Key: "content_length", Max: 12, Score: chkContentLength},
			{Key: "paragraph_length", Max: 4, Score: chkParagraphLength},
			{Key: "sentence_length", Max: 4, Score: chkSentenceLength},
			{Key: "comparison_table", Max: 5, Score: chkComparisonTable},
		}},
		{Key: "keywords", Weight: 0.15, Checks: []Check{
			{Key: "keyword_density", Max: 8, Score: chkKeywordDensity},
			{Key: "keyword_positions", Max: 5, Score: chkKeywordPositions},
			{Key: "secondary_keywords", Max: 2, Score: chkSecondaryKeywords},
		}},
		{Key: "links", Weight: 0.10, Checks: []Check{
			{Key: "internal_link_count", Max: 6, Score: chkInternalLinks},
			{Key: "external_links", Max: 2, Score: chkExternalLinks},
			{Key: "anchor_descriptive", Max: 2, Score: chkAnchors},
		}},
		{Key: "images", Weight: 0.10, Checks: []Check{
			{Key: "alt_coverage", Max: 5, Score: chkAltCoverage},
			{Key: "image_weight", Max: 3, Score: chkImageWeight},
			{Key: "image_format", Max: 2, Score: chkImageFormat},
		}},
		{Key: "tech", Weight: 0.10, Checks: []Check{
			{Key: "url_clean", Max: 3, Score: chkURLClean},
			{Key: "canonical", Max: 3, Score: chkCanonical},
			{Key: "schema_present", Max: 2, Score: chkSchema},
			{Key: "https", Max: 2, Score: chkHTTPS},
		}},
	}
}
