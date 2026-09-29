package contenthttp

// article_score_view.go — 文章页评分侧栏的视图结构（fragments/seo_score 的入参）。
//
// 这些结构原先定义在 dashboard 的 settings_handle.go 里（页面设置面板、文章编辑页、
// 商品/分类/品牌页三处共用）。文章页搬回 content 模块后不能跨包引用 dashboard 的私有
// 类型，这里按原定义**逐字段复制**一份：模板 fragments/seo_score 是按字段名取值的，
// 少一个键 Jet 会报错并截断整页，所以字段集必须与模板要求一致（含文章页用不到的
// ProfileType / Duplicates / Empty —— 它们由同一份模板消费）。
//
// 与 dashboard 那份的同步点是模板契约而不是 Go 类型：只要 fragments/seo_score 的键不变，
// 两份定义各自演进互不影响。

import (
	"github.com/gin-gonic/gin"

	"go_wp/pkg/response"
)

// requestScoreLang 评分使用的语言（后台 Cookie / Accept-Language，SEO-001）。
func requestScoreLang(c *gin.Context) string {
	return response.RequestLanguage(c)
}

// scoreView SEO 评分视图（服务端渲染，客户端只处理「点击建议跳转」）。
type scoreView struct {
	OK       bool
	Total    int
	Grade    string
	Sections []scoreSectionView
	// ProfileType / ProfileReason 本次评分所用的页型与调权理由（审计 SEO-016）。
	// 文档要求调权在结果里回显（docs/02-E1 §5）：不显示的话，编辑者看到同一份内容
	// 在商品页比文章页高几分时无从解释。空 = 用默认权重（页面草稿与文章）。
	ProfileType   string
	ProfileReason string
	// Duplicates 与本页标题重复的其它页面（审计 SEO-018 编辑期轻量版）。
	// **列出冲突页面本身**而不是只报数量：只报「有重复」运营不知道该去改哪一页。
	Duplicates    []string
	DuplicateNote string
	// Empty 空态文案（读不到实体时给一句可读的话，而不是让面板整体不可用）。
	Empty     string
	SerpTitle string
	SerpURL   string
	SerpDesc  string
}

// scoreSectionView 单个评分维度。
type scoreSectionView struct {
	Label      string
	Color      string
	ColorLabel string
	Score      int
	Max        int
	Issues     []scoreIssueView
}

// scoreIssueView 单条未达标检查（Target 非空表示可点击定位）。
type scoreIssueView struct {
	Text   string
	Target string
}

// 评分颜色 → 等级文案的映射**只有一份**，在 seoscore.ScoreGradeText（internal/seo/score_grade.go）。
// 此前本文件与 project/inbound/http/settings_panel.go 各持一份逐字相同的副本，
// 且两处注释都写着“正确的归宿是 seoscore 包” —— 已按此收编，取词改调该出口。
