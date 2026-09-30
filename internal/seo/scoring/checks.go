package scoring

// checks.go — 检查项定义（docs/02-E1 §4 的 24 个检查项）。
// 每个 Check.Score 是纯函数：只读 Input，返回得分与实测值描述。

type checkFunc func(*Input) (int, string)

// Check 单个检查项。
type Check struct {
	Key       string
	Label     string
	Score     checkFunc
	Max       int
	Benchmark string
	Hint      string
}

// Section 评分维度。
type Section struct {
	Key    string
	Label  string
	Weight float64
	Checks []Check
}

// allSections 全部维度与检查项（顺序即侧栏展示顺序）。
func allSections() []Section {
	return []Section{
		{Key: "title", Label: "标题标签", Weight: 0.15, Checks: []Check{
			{Key: "title_present", Label: "标题存在", Max: 5, Score: chkTitlePresent,
				Benchmark: "非空且长度 ≥ 20 字符", Hint: "为页面填写 SEO 标题"},
			{Key: "title_length", Label: "标题长度", Max: 5, Score: chkTitleLength,
				Benchmark: "40-60 宽度单位", Hint: "删掉冗余词，品牌名放末尾，关键词前置（中文按约 30 字）"},
			{Key: "title_keyword_first_half", Label: "关键词位置", Max: 5, Score: chkTitleKeywordFirstHalf,
				Benchmark: "主关键词出现在标题前半段", Hint: "把主关键词挪到标题前 30 个字符内"},
		}},
		{Key: "meta", Label: "元描述", Weight: 0.05, Checks: []Check{
			{Key: "meta_length", Label: "描述长度", Max: 3, Score: chkMetaLength,
				Benchmark: "150-160 字符", Hint: "补齐到 150-160 字符，避免 SERP 截断"},
			{Key: "meta_has_cta", Label: "含行动号召", Max: 2, Score: chkMetaCTA,
				Benchmark: "含动词型 CTA", Hint: "结尾加「立即选购 / 了解更多」这类动作词"},
		}},
		{Key: "headings", Label: "标题结构", Weight: 0.10, Checks: []Check{
			{Key: "single_h1", Label: "唯一 H1", Max: 4, Score: chkSingleH1,
				Benchmark: "恰好 1 个 H1", Hint: "保留一个 H1，其余降为 H2"},
			{Key: "h1_has_keyword", Label: "H1 含关键词", Max: 3, Score: chkH1Keyword,
				Benchmark: "H1 含主关键词", Hint: "在 H1 中自然带上主关键词"},
			{Key: "heading_hierarchy", Label: "层级递进", Max: 3, Score: chkHierarchy,
				Benchmark: "H1→H2→H3 无跳级", Hint: "补回缺失的中间层级，避免 H2 直接跳到 H4"},
		}},
		{Key: "content", Label: "内容质量", Weight: 0.25, Checks: []Check{
			// 维度内 25 分的分配自 2026-09-29 起是 12/4/4/5（原来是 15/5/5）：
			// 对比表是新增的硬要求，分数从长度与可读性两项里让出来 —— 维度总分不变，
			// 但「写得很长」不再自动等于「内容质量高」。
			{Key: "content_length", Label: "内容长度", Max: 12, Score: chkContentLength,
				Benchmark: "按查询意图分档（信息型 1500+ / 交易型 500+）", Hint: "补充子主题、FAQ、案例与证据"},
			{Key: "paragraph_length", Label: "段落长度", Max: 4, Score: chkParagraphLength,
				Benchmark: "单段 ≤150 词 / ≤200 字", Hint: "拆成多段，每段一个要点"},
			{Key: "sentence_length", Label: "句子长度", Max: 4, Score: chkSentenceLength,
				Benchmark: "长句占比 ≤20%", Hint: "长句拆短，降低阅读负担"},
			{Key: "comparison_table", Label: "对比表", Max: 5, Score: chkComparisonTable,
				Benchmark: "比较型 / 购买型意图需有结构化对比表（信息型不要求）",
				Hint:      "补一张对比表或矩阵（逐项对照、可据以决策），而不是把规格再列一遍"},
		}},
		{Key: "keywords", Label: "关键词优化", Weight: 0.15, Checks: []Check{
			{Key: "keyword_density", Label: "关键词密度", Max: 8, Score: chkKeywordDensity,
				Benchmark: "0.5-2.0%", Hint: ">3% 属堆砌，用同义词/实体词替换"},
			{Key: "keyword_positions", Label: "关键词点位", Max: 5, Score: chkKeywordPositions,
				Benchmark: "title / H1 / 首 100 词 / URL / alt / meta 六点位", Hint: "补齐缺失点位"},
			{Key: "secondary_keywords", Label: "次级关键词", Max: 2, Score: chkSecondaryKeywords,
				Benchmark: "2-3 个次级词出现", Hint: "补 2-3 个语义词/长尾词"},
		}},
		{Key: "links", Label: "内外链", Weight: 0.10, Checks: []Check{
			{Key: "internal_link_count", Label: "内链数量", Max: 6, Score: chkInternalLinks,
				Benchmark: "按篇幅 2-15 条", Hint: "每千字加 3-5 条上下文内链"},
			{Key: "external_links", Label: "外部链接", Max: 2, Score: chkExternalLinks,
				Benchmark: "≥1 条权威外链", Hint: "引用权威来源（官方文档/研究）"},
			{Key: "anchor_descriptive", Label: "锚文本描述性", Max: 2, Score: chkAnchors,
				Benchmark: "无「点击这里」类锚文本", Hint: "锚文本写清目标页主题"},
		}},
		{Key: "images", Label: "图片优化", Weight: 0.10, Checks: []Check{
			{Key: "alt_coverage", Label: "alt 覆盖率", Max: 5, Score: chkAltCoverage,
				Benchmark: "内容图 100% 有功能性 alt", Hint: "为每张内容图补描述性 alt"},
			{Key: "image_weight", Label: "图片体积", Max: 3, Score: chkImageWeight,
				Benchmark: "Hero<200KB / 内容<150KB / 图标<30KB", Hint: "压缩或用 WebP 变体"},
			{Key: "image_format", Label: "图片格式", Max: 2, Score: chkImageFormat,
				Benchmark: "WebP/AVIF/SVG 占比 ≥80%", Hint: "改用 WebP/AVIF（图标用 SVG）"},
		}},
		{Key: "tech", Label: "页面技术", Weight: 0.10, Checks: []Check{
			{Key: "url_clean", Label: "URL 简洁", Max: 3, Score: chkURLClean,
				Benchmark: "≤75 字符、无参数、小写短横线", Hint: "精简 URL，去掉查询参数与大小写混用"},
			{Key: "canonical", Label: "canonical", Max: 3, Score: chkCanonical,
				Benchmark: "显式配置或可推导", Hint: "在页面 SEO 设置里显式填写 canonical"},
			{Key: "schema_present", Label: "结构化数据", Max: 2, Score: chkSchema,
				Benchmark: "按页型输出 JSON-LD", Hint: "为文章/商品补 Article/Product 结构化数据"},
			{Key: "https", Label: "HTTPS", Max: 2, Score: chkHTTPS,
				Benchmark: "强制 HTTPS", Hint: "全站强制 HTTPS 并做 301 跳转"},
		}},
	}
}
