// product_score_view.go — SEO 评分片段的**视图装配**（分数 → 片段行）。
//
// 为什么在 service：这些函数把算分结果翻成页面要读的字段（分项行、未达标整句、
// SERP 回显、title 冲突清单）。控制器只负责「绑定 → 算 → 返回业务层给的东西」，
// 装配留在那里就是它在算东西，而且三个 POST 端点与抽屉各拼一遍措辞就会漂。
//
// 取词函数由请求期传入（语言是请求级的，service 不持有 gin.Context）。
package productservice

import (
	"strings"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	seoscore "go_wp/internal/seo"
	"go_wp/internal/seo/scoring"
	"go_wp/pkg/i18n"
)

// SeoEmptyView 空态片段视图：文案是「key + 中文兜底」的事实（SeoScoreInputOf 给出），取值在这里。
func SeoEmptyView(tr TranslateFunc, empty SeoScoreEmpty) productdto.ScoreResp {
	return productdto.ScoreResp{Empty: tr(empty.Key, empty.Fallback)}
}

// SeoEntityScoreView 评分结果 → 片段视图（含页型回显与 title 冲突清单）。
func SeoEntityScoreView(tr TranslateFunc, in *scoring.EntityPageInput, res *scoring.Result,
	dups []scoring.TitleEntry) productdto.ScoreResp {
	sv := productdto.ScoreResp{OK: true}
	if res != nil {
		sv.Total = res.Total
		sv.Grade = res.Grade
		// 页型与调权理由回显（docs/02-E1 §5）：编辑者要能看出「这份分数是按商品页的
		// 尺子量的」，否则同一段描述在商品页比文章页高几分会被当成评分器不稳。
		if res.Profile != nil {
			sv.ProfileType = res.Profile.Type
			sv.ProfileReason = res.Profile.Reason
		}
		sv.Sections = seoScoreSections(tr, res.Sections)
	}
	title := scoring.EntityTitle(in)
	fillSeoSerp(&sv, tr, in, title)
	fillSeoDuplicateTitle(&sv, title, dups)
	return sv
}

// seoScoreSections 评分分项 → 片段行（含未达标项的整句说明）。
//
// 未达标行的整句是**模板**：标签 / 实测 / 基准 / 建议四段都是数据，句子的语序与标点
// 由词条决定（英文的冒号与括号与中文不同），命名占位符 + FillTranslate 负责填充
// （词条被写坏时自动回落下面那句中文兜底）。
func seoScoreSections(tr TranslateFunc, sections []scoring.SectionResult) []productdto.ScoreSectionResp {
	out := make([]productdto.ScoreSectionResp, 0, len(sections))
	for _, sec := range sections {
		item := productdto.ScoreSectionResp{
			Label: sec.Label, Color: sec.Color, Score: sec.Score, Max: sec.Max,
			ColorLabel: seoscore.ScoreGradeText(tr, sec.Color),
		}
		for _, ck := range sec.Checks {
			if ck.Score >= ck.Max {
				continue
			}
			item.Issues = append(item.Issues, productdto.ScoreIssueResp{
				Text: i18n.FillTranslate(tr, productenums.SEOIssueLine,
					"{label}：{actual}（基准 {benchmark}）→ {hint}",
					map[string]string{
						"label": ck.Label, "actual": ck.Actual,
						"benchmark": ck.Benchmark, "hint": ck.Hint,
					}),
				Target: ck.Target,
			})
		}
		out = append(out, item)
	}
	return out
}

// fillSeoSerp 搜索结果预览（标题 / 路径 / 描述）：空值给「未填写」那句文案事实。
func fillSeoSerp(sv *productdto.ScoreResp, tr TranslateFunc, in *scoring.EntityPageInput, title string) {
	sv.SerpTitle = title
	if sv.SerpTitle == "" {
		sv.SerpTitle = tr(productenums.SEOSerpTitleEmpty, "（未填写标题）")
	} else {
		sv.SerpTitle = seoscore.TruncateDisplayWidth(sv.SerpTitle, 60)
	}
	sv.SerpURL = in.URL
	if sv.SerpURL == "" {
		sv.SerpURL = tr(productenums.SEOSerpURLEmpty, "（线上路径未知）")
	}
	sv.SerpDesc = strings.TrimSpace(in.SEODescription)
	if sv.SerpDesc == "" {
		sv.SerpDesc = strings.TrimSpace(in.Description)
	}
	if sv.SerpDesc == "" {
		sv.SerpDesc = tr(productenums.SEOSerpDescEmpty, "（未填写描述）")
	}
}

// fillSeoDuplicateTitle title 唯一性（SEO-018 编辑期轻量版）：结论形状与发布侧体检一致 ——
// 列出全部冲突页面。
func fillSeoDuplicateTitle(sv *productdto.ScoreResp, title string, dups []scoring.TitleEntry) {
	if len(dups) == 0 {
		return
	}
	sv.DuplicateNote = scoring.DuplicateTitleMessage(title, dups)
	for _, d := range dups {
		sv.Duplicates = append(sv.Duplicates, d.Page)
	}
}
