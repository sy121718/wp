package seo

// score_grade.go — 评分等级（颜色）→ 展示文案的**唯一映射**（key + 中文兜底）。
//
// 为什么落在 seo 包，而不是某个模块的 enums：
//
//  1. 这张表被**多个模块**共同消费（content 的文章评分侧栏、project 的工作台评分面板；
//     此前 product 也持有一份同义表），放到任一业务模块都会让别的模块跨模块 import 它的
//     enums —— 语义上是“谁拥有”错位：颜色到文案不是消费方的知识；
//  2. `internal/seo` 是**唯一能同时被业务模块、构建器与发布管线引用的零依赖层**
//     （同包 absolute_url.go 已写明这条判据），且它已经是全部评分相关展示词条的入口
//     （各处以 `seoscore` 别名 import：content / project / product / workbench / page /
//     publication / presentation）；
//  3. 评分的**内核**在 internal/seo/scoring（产出 Color 值），词条 key 是它的展示投影 ——
//     投影放它们之间的 seo 包，scoring 保持纯逻辑、不带 i18n 词条。
//
// 收录前的状态：同一张表在本仓库**三处逐字重复** —— content/inbound/http/article_score_view.go、
// project/inbound/http/settings_panel.go 与 product/inbound/http/product_page_shared.go
//（map 内容、函数体全同，仅注释措辞不同）。三处合并后只调本文件的出口，改等级或改词条
// 只有一处要动。
//
// product 那一份另带一套 key（admin.seo.grade.{green,lightgreen,yellow,red,redBlocking}，
// 与本文件用的 admin.seo.score.grade.* 值逐字相同、只有 key 名不同）：收编后它改用本表的 key，
// 旧 key 失去全部引用者，由迁移 458（public/migrations/458_retire_seo_grade_keys.sql）
// 从 sys_i18n 退役 —— 「同一批展示文案只能有一份定义」的收尾动作。
//
// 未知颜色原样回显颜色名（它是数据不是文案），便于排查 —— 与收录前行为一致。

// ScoreGradeLabel 一个评分等级的词条（key + 中文兜底）。
type ScoreGradeLabel struct{ Key, Fallback string }

// ScoreGradeLabels 评分颜色 → 词条（词条本体在 sys_i18n 的 admin.seo.score.grade.*）。
var ScoreGradeLabels = map[string]ScoreGradeLabel{
	"green":        {"admin.seo.score.grade.excellent", "优秀"},
	"lightgreen":   {"admin.seo.score.grade.good", "良好"},
	"yellow":       {"admin.seo.score.grade.needsWork", "需改进"},
	"red":          {"admin.seo.score.grade.poor", "差"},
	"red-blocking": {"admin.seo.score.grade.missing", "缺失"},
}

// ScoreGradeText 评分颜色 → 当前语言文案（tr 取 shell.TranslateFor(c)）。
func ScoreGradeText(tr func(key, fallback string) string, color string) string {
	if l, ok := ScoreGradeLabels[color]; ok {
		return tr(l.Key, l.Fallback)
	}
	return color
}
