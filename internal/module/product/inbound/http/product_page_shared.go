package producthttp

// product_page_shared.go — 商品后台页需要的页面级小工具。
//
// 搬迁说明（后台页面回迁）：这些符号原来住在已删除的 dashboard 装配层
//（settings_handle.go 的评分视图、page_translations_*.go 的翻译提示文案）。
// 它们是装配层内、被多个页面域共用的**页面级**工具，搬迁时不进壳层（shell）
// 也没有共同承接者，因此按「页面跟着模块走」在这里就地重建 —— 只搬本模块
// 页面真正用到的部分，不引入新的取数逻辑。
//
// 与 dashboard 侧的差异只有一处：翻译提示只保留商品翻译页用到的 4 条兜底。
// 若日后壳包统一提供这些工具，本文件连同其调用点一并收敛即可。

import (
	"strings"

	"github.com/gin-gonic/gin"

	"go_wp/internal/web/shell"
	"go_wp/pkg/response"
)

// scoreView SEO 评分视图（服务端渲染，客户端只处理「点击建议跳转」）。
type scoreView struct {
	OK       bool
	Total    int
	Grade    string
	Sections []scoreSectionView
	// ProfileType / ProfileReason 本次评分所用的页型与调权理由（审计 SEO-016）。
	// 文档要求调权在结果里回显（docs/02-E1 §5）：不显示的话，编辑者看到同一份内容
	// 在商品页比文章页高几分时无从解释。空 = 用默认权重。
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

// seoColorLabels 评分颜色 → 中文（与前端旧面板一致）。
var seoColorLabels = map[string]string{
	"green": "优秀", "lightgreen": "良好", "yellow": "需改进", "red": "差", "red-blocking": "缺失",
}

// requestScoreLang 评分使用的语言（后台 Cookie / Accept-Language，SEO-001）。
func requestScoreLang(c *gin.Context) string {
	return response.RequestLanguage(c)
}

// productTranslationMsgFallback 商品翻译页提示的中文兜底（dashboard enums 常量是
// sys_i18n 的 key，缺词条时用这里的原文；只列本页真正会产生的那几条）。
var productTranslationMsgFallback = map[string]string{
	MsgTranslationSaveFailed:  "译文保存失败，请稍后重试",
	MsgTranslationInvalid:     "提交数据不完整，请刷新页面后重试",
	MsgTranslationStale:       "原文已变更，请刷新页面后重新翻译",
	MsgTranslationLangInvalid: "目标语言未启用，请先在站点设置里启用",
}

// translationMsg 把 enums key 翻成当前语言；非 key（如 service 校验的原始中文）原样返回。
func translationMsg(c *gin.Context, msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ""
	}
	return shell.TranslateFor(c)(msg, productTranslationMsgFallback[msg])
}

// translationMsgs 批量翻译提示文案。
func translationMsgs(c *gin.Context, msgs []string) []string {
	if len(msgs) == 0 {
		return nil
	}
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		if text := translationMsg(c, m); text != "" {
			out = append(out, text)
		}
	}
	return out
}

// containsString 判断切片是否含目标值。
func containsString(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// translationLangOption 工作台语言下拉项。
type translationLangOption struct {
	Code   string
	Label  string
	Active bool
}
