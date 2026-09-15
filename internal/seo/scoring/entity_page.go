package scoring

// entity_page.go — 商品 / 分类 / 品牌页的评分接线（审计 SEO-016）。
//
// 背景：ProductProfile / LandingProfile / GuideProfile 三套页型权重此前只被单测
// 调用过，生产入口一律传 nil（internal/seo/extract.go、article.go 都是 Score(in, nil)）。
// 结果是商品页拿着文章页的尺子打分：内容深度占 25%，而商品页本来就不该靠长文取胜；
// 分类页也不该按信息型长文去要求 1500 字。
//
// 三层职责刻意分开，本文件只做中间那层：
//
//  1. 调用方（dashboard handler）负责取数 —— 商品 / 分类 / 品牌的字段来自各自契约；
//  2. 本文件负责**字段映射与页型选择**（实体字段 → 评分引擎 Input）；
//  3. scoring.Score 负责计算。
//
// 取数留在调用方不是偷懒：本包是纯计算包（无 IO、不依赖任何业务模块），
// 一旦它认识 productdto，「再加一个实体类型」就变成改核心包 + 核心包依赖业务模块。

import "strings"

// PageKind 页型标识（决定用哪套 Profile 权重）。
type PageKind string

const (
	// KindProduct 商品详情页。
	KindProduct PageKind = "product"
	// KindCategory 商品分类页。
	KindCategory PageKind = "category"
	// KindBrand 品牌页。
	KindBrand PageKind = "brand"
)

// ProfileFor 页型 → 权重档案；没有对应档案返回 nil（nil = 默认权重）。
//
// 只映射**文档里已经定义过的**页型（docs/02-E1 §5 的调权表）。为没有依据的页型
// 现造一套权重，等于把猜出来的调权写进评分结果，而运营会把它当真 —— 页面草稿
// 与文章至今没有专门档案，它们的 Type 就保持空。
func ProfileFor(kind PageKind) *Profile {
	switch kind {
	case KindProduct:
		return ProductProfile()
	case KindCategory, KindBrand:
		return LandingProfile()
	default:
		return nil
	}
}

// EntityPageInput 商品 / 分类 / 品牌页的评分输入（中性结构体）。
//
// 字段刻意都取自**已有实体列**：商品名 / 副标题 / 描述 / 图集 alt / SEO 字段 /
// 规格维度名，分类与品牌的名称 / 描述 / 图 / SEO 字段。不新增数据库列 ——
// 评分器的输入一旦需要新列，接线就变成迁移工程，而迁移要等一个发布窗口。
type EntityPageInput struct {
	Kind PageKind // 决定 Profile 与内容长度基准

	Name        string // 实体名（SEO 标题 / 描述的回落值，也是 H1）
	Subtitle    string // 商品副标题（可空，进描述与正文）
	Description string // 纯文本描述（调用方负责去 HTML 标签）
	// SEOTitle / SEODescription 专门的 SEO 字段，填了优先用（与文章侧同一口径）。
	SEOTitle       string
	SEODescription string
	// FocusKeyword 主关键词。商品域目前**没有这一列**：接线时不传，评分器会如实
	// 报「未设置主关键词」而不是编一个。要让它有值，得先给实体加列（本次未做）。
	FocusKeyword      string
	SecondaryKeywords []string
	URL               string   // 详情页线上路径；编辑期拿不到就传空（URL 项按未设置判）
	Slug              string   // URL 段（商品 / 分类 / 品牌的 slug）
	Images            []Image  // 图片与 alt（商品图集 / 分类图 / 品牌 logo）
	SpecNames         []string // 规格维度名（属性组名）→ H2
	ChildNames        []string // 子分类名 / 挂载分类名 → H2
	// HasCanonical / HasSchema 详情页产物是否带 canonical 与 JSON-LD。
	// 发布实例渲染时由系统注入（presentation 的 applyEntitySEO），不由编辑者填，
	// 所以调用方按「该实体已经发布」判真。
	HasCanonical bool
	HasSchema    bool
	Locale       string

	// Intent 可覆盖查询意图（空则按页型取默认值）。
	Intent QueryIntent
}

// EntityTitle 实体页实际用于评分的标题（SEO 字段优先、实体名回落）。
//
// 导出是为了让 title 唯一性检查与评分**比对同一个值**：两处各写一份回落规则的话，
// 会出现「评分说标题是 X、唯一性却在比对空串」这种分叉 —— 检查项与结论打架时，
// 运营只会认为这个功能不可信。
func EntityTitle(p *EntityPageInput) string {
	if p == nil {
		return ""
	}
	return PreferredTitle(p.SEOTitle, p.Name)
}

// PreferredTitle 标题回落规则（专门的 SEO 字段优先，缺了才用实体名）。
//
// 与文章侧（internal/seo/article.go 的 firstNonEmpty(seoTitle, title)）同一口径：
// 运营的心智是「填了 SEO 标题就该用它」，而没填时 SERP 上出现的本来就是实体名。
func PreferredTitle(seoTitle, fallback string) string {
	return firstNonEmpty(seoTitle, fallback)
}

// ScoreEntityPage 计算商品 / 分类 / 品牌页的 SEO 评分（自动接上对应页型档案）。
func ScoreEntityPage(in *EntityPageInput) *Result {
	if in == nil {
		in = &EntityPageInput{}
	}
	return Score(entityInputOf(in), ProfileFor(in.Kind))
}

// entityInputOf 实体页输入 → 评分引擎输入（映射规则集中在这一处）。
func entityInputOf(p *EntityPageInput) *Input {
	in := &Input{
		URL:    strings.TrimSpace(p.URL),
		Locale: p.Locale,
		// SEO 字段优先、实体名回落：与文章侧（internal/seo/article.go）同一口径 ——
		// 「专门的 SEO 字段填了就该用它」，没填时 SERP 上出现的就是实体名本身。
		Title:             PreferredTitle(p.SEOTitle, p.Name),
		MetaDescription:   PreferredTitle(p.SEODescription, p.Description),
		FocusKeyword:      strings.TrimSpace(p.FocusKeyword),
		SecondaryKeywords: p.SecondaryKeywords,
		Images:            p.Images,
		HasCanonical:      p.HasCanonical,
		HasSchema:         p.HasSchema,
		// 站点发布走 HTTPS（与页面草稿、文章的评分入口同假设）：这不是「假设」，
		// 而是构建/发布链路的既有事实，评分器不该把它记成一条待办。
		IsHTTPS: true,
		Intent:  intentOf(p),
	}
	in.Headings = entityHeadings(p)
	in.BodyText = entityBody(p)
	in.WordCount = textUnitLen(in.BodyText, in.Locale)
	return in
}

// intentOf 页型的查询意图（决定内容长度基准）。
//
// 商品页是交易页 → transactional（满分 500 字）；分类页与品牌页是「浏览后决定
// 要不要点进去」的页 → commercial（满分 1200）。两者都远低于信息型长文的 1500 ——
// 这正是 SEO-016 verification 要的「商品页的内容长度要求低于指南页」。
func intentOf(p *EntityPageInput) QueryIntent {
	if p.Intent != "" {
		return p.Intent
	}
	switch p.Kind {
	case KindProduct:
		return IntentTransactional
	case KindCategory, KindBrand:
		return IntentCommercial
	default:
		return IntentInformational
	}
}

// entityHeadings 实体页的标题结构：实体名是唯一的 H1，规格维度与关联分类各占一个 H2。
//
// 这不是「凭想象造结构」：详情页模板渲染商品时，商品名就是页面的 H1，规格维度名
// 作为分组小标题出现。评分按渲染出来会长成的样子打分，而不是按字段里存了什么 ——
// 与文章侧把封面按 hero 计入图片口径是同一条理由。
func entityHeadings(p *EntityPageInput) []Heading {
	out := make([]Heading, 0, 1+len(p.SpecNames)+len(p.ChildNames))
	if name := strings.TrimSpace(p.Name); name != "" {
		out = append(out, Heading{Level: 1, Text: name})
	}
	for _, n := range p.SpecNames {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, Heading{Level: 2, Text: n})
		}
	}
	for _, n := range p.ChildNames {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, Heading{Level: 2, Text: n})
		}
	}
	return out
}

// entityBody 实体页的可见正文（名称 / 副标题 / 描述 / 规格 / 分类，每项一段）。
//
// 段落以单换行分隔：scoring 的 splitParagraphs 按单换行切段（富文本提取器产出的
// 也是单换行），这样「商品描述写成一大坨」会被段落长度项如实扣分，而不是被
// 拼成一段后所有页都扣同样的分。
func entityBody(p *EntityPageInput) string {
	parts := make([]string, 0, 5+len(p.SpecNames)+len(p.ChildNames))
	for _, s := range []string{p.Name, p.Subtitle, p.Description} {
		if s = strings.TrimSpace(s); s != "" {
			parts = append(parts, s)
		}
	}
	for _, n := range p.SpecNames {
		if n = strings.TrimSpace(n); n != "" {
			parts = append(parts, n)
		}
	}
	for _, n := range p.ChildNames {
		if n = strings.TrimSpace(n); n != "" {
			parts = append(parts, n)
		}
	}
	return strings.Join(parts, "\n")
}

// firstNonEmpty 取第一个非空值（SEO 字段回落用；与 seo 包的同名助手同语义）。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
